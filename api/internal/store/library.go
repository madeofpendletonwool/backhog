package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// LibraryFilter describes a library query. Zero values mean "no filter".
type LibraryFilter struct {
	Status     string
	Query      string
	PlatformID *int64
	GenreID    *int64
	ListID     string
	Sort       string
	Limit      int
	Offset     int
}

// entrySelect is the shared projection for entry queries. Genres and platforms
// are attached separately by hydrate.
const entrySelect = `
	SELECT e.id, e.status, e.platform_id, e.user_rating, e.notes, e.queue_position,
	       e.started_at, e.finished_at, e.created_at, e.updated_at, e.game_id,
	       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0)
	FROM library_entries e JOIN games g ON g.id = e.game_id`

// sortClauses whitelists user-supplied sort keys. Never interpolate raw input.
var sortClauses = map[string]string{
	"added":    "e.created_at DESC",
	"name":     "g.name COLLATE NOCASE ASC",
	"released": "g.first_release_date DESC NULLS LAST",
	"rating":   "g.igdb_rating DESC NULLS LAST",
	"shortest": "g.time_to_beat_main ASC NULLS LAST",
	"longest":  "g.time_to_beat_main DESC NULLS LAST",
	"updated":  "e.updated_at DESC",
	"queue":    "e.queue_position ASC NULLS LAST, e.created_at ASC",
}

// AddEntry puts a game into a user's library. The game must already exist in
// the shared cache. Returns ErrConflict if the user already has it.
func (s *Store) AddEntry(ctx context.Context, userID string, gameID int64, status string, platformID *int64) (models.Entry, error) {
	if !models.ValidStatus(status) {
		status = models.StatusBacklog
	}

	id := newID()
	var queuePos *float64
	if status == models.StatusBacklog {
		// New backlog items land at the end of the queue.
		pos, err := s.nextQueuePosition(ctx, userID)
		if err != nil {
			return models.Entry{}, err
		}
		queuePos = &pos
	}

	var started *time.Time
	if status == models.StatusPlaying {
		now := time.Now()
		started = &now
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO library_entries (id, user_id, game_id, status, platform_id, queue_position, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, gameID, status, platformID, queuePos, started)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Entry{}, ErrConflict
		}
		return models.Entry{}, err
	}
	return s.GetEntry(ctx, userID, id)
}

// GetEntry returns one entry, scoped to its owner.
func (s *Store) GetEntry(ctx context.Context, userID, entryID string) (models.Entry, error) {
	entries, err := s.queryEntries(ctx, entrySelect+` WHERE e.user_id = ? AND e.id = ?`, userID, entryID)
	if err != nil {
		return models.Entry{}, err
	}
	if len(entries) == 0 {
		return models.Entry{}, ErrNotFound
	}
	return entries[0], nil
}

// ListEntries returns a filtered page of a user's library.
func (s *Store) ListEntries(ctx context.Context, userID string, f LibraryFilter) ([]models.Entry, error) {
	var where []string
	var args []any

	where = append(where, "e.user_id = ?")
	args = append(args, userID)

	if models.ValidStatus(f.Status) {
		where = append(where, "e.status = ?")
		args = append(args, f.Status)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "g.name LIKE ? COLLATE NOCASE")
		args = append(args, "%"+escapeLike(q)+"%")
	}
	if f.PlatformID != nil {
		// Match either the platform the user picked or one the game ships on.
		where = append(where, `(e.platform_id = ? OR EXISTS (
			SELECT 1 FROM game_platforms gp WHERE gp.game_id = e.game_id AND gp.platform_id = ?))`)
		args = append(args, *f.PlatformID, *f.PlatformID)
	}
	if f.GenreID != nil {
		where = append(where, `EXISTS (
			SELECT 1 FROM game_genres gg WHERE gg.game_id = e.game_id AND gg.genre_id = ?)`)
		args = append(args, *f.GenreID)
	}
	if f.ListID != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM list_items li WHERE li.entry_id = e.id AND li.list_id = ?)`)
		args = append(args, f.ListID)
	}

	orderBy, ok := sortClauses[f.Sort]
	if !ok {
		orderBy = sortClauses["added"]
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}

	query := entrySelect + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + orderBy + " LIMIT ? OFFSET ?"
	args = append(args, limit, max(f.Offset, 0))

	return s.queryEntries(ctx, query, args...)
}

// CountEntries returns the total matching a filter, for pagination.
func (s *Store) CountEntries(ctx context.Context, userID string, f LibraryFilter) (int, error) {
	var count int
	args := []any{userID}
	query := `SELECT COUNT(*) FROM library_entries e JOIN games g ON g.id = e.game_id WHERE e.user_id = ?`
	if models.ValidStatus(f.Status) {
		query += " AND e.status = ?"
		args = append(args, f.Status)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		query += " AND g.name LIKE ? COLLATE NOCASE"
		args = append(args, "%"+escapeLike(q)+"%")
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// EntryUpdate carries the mutable fields of an entry. Nil means "leave alone".
type EntryUpdate struct {
	Status     *string
	PlatformID *int64
	ClearPlatform bool
	UserRating *int
	ClearRating bool
	Notes      *string
}

// UpdateEntry applies a partial update. Status transitions stamp started_at and
// finished_at, and moving in or out of 'backlog' adds or removes the entry from
// the play queue, so the queue always reflects exactly what is still unplayed.
func (s *Store) UpdateEntry(ctx context.Context, userID, entryID string, u EntryUpdate) (models.Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Entry{}, err
	}
	defer tx.Rollback()

	var currentStatus string
	var startedAt sql.NullTime
	err = tx.QueryRowContext(ctx,
		`SELECT status, started_at FROM library_entries WHERE user_id = ? AND id = ?`,
		userID, entryID).Scan(&currentStatus, &startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Entry{}, ErrNotFound
	}
	if err != nil {
		return models.Entry{}, err
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	var args []any

	if u.Status != nil && *u.Status != currentStatus {
		if !models.ValidStatus(*u.Status) {
			return models.Entry{}, fmt.Errorf("invalid status %q", *u.Status)
		}
		newStatus := *u.Status
		sets = append(sets, "status = ?")
		args = append(args, newStatus)

		switch newStatus {
		case models.StatusPlaying:
			if !startedAt.Valid {
				sets = append(sets, "started_at = CURRENT_TIMESTAMP")
			}
			sets = append(sets, "finished_at = NULL")
		case models.StatusPlayed:
			if !startedAt.Valid {
				sets = append(sets, "started_at = CURRENT_TIMESTAMP")
			}
			sets = append(sets, "finished_at = CURRENT_TIMESTAMP")
		case models.StatusDropped:
			sets = append(sets, "finished_at = CURRENT_TIMESTAMP")
		case models.StatusBacklog, models.StatusWishlist:
			sets = append(sets, "started_at = NULL", "finished_at = NULL")
		}

		if newStatus == models.StatusBacklog {
			pos, err := nextQueuePositionTx(ctx, tx, userID)
			if err != nil {
				return models.Entry{}, err
			}
			sets = append(sets, "queue_position = ?")
			args = append(args, pos)
		} else {
			sets = append(sets, "queue_position = NULL")
		}
	}

	switch {
	case u.ClearPlatform:
		sets = append(sets, "platform_id = NULL")
	case u.PlatformID != nil:
		sets = append(sets, "platform_id = ?")
		args = append(args, *u.PlatformID)
	}

	switch {
	case u.ClearRating:
		sets = append(sets, "user_rating = NULL")
	case u.UserRating != nil:
		if *u.UserRating < 1 || *u.UserRating > 10 {
			return models.Entry{}, fmt.Errorf("rating must be between 1 and 10")
		}
		sets = append(sets, "user_rating = ?")
		args = append(args, *u.UserRating)
	}

	if u.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *u.Notes)
	}

	args = append(args, userID, entryID)
	_, err = tx.ExecContext(ctx,
		`UPDATE library_entries SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id = ?`, args...)
	if err != nil {
		return models.Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Entry{}, err
	}
	return s.GetEntry(ctx, userID, entryID)
}

// DeleteEntry removes a game from a user's library.
func (s *Store) DeleteEntry(ctx context.Context, userID, entryID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM library_entries WHERE user_id = ? AND id = ?`, userID, entryID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats summarises the library for the dashboard.
func (s *Store) Stats(ctx context.Context, userID string) (models.Stats, error) {
	var st models.Stats
	var loggedMinutes float64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(status = 'backlog'), 0),
			COALESCE(SUM(status = 'playing'), 0),
			COALESCE(SUM(status = 'played'), 0),
			COALESCE(SUM(status = 'dropped'), 0),
			COALESCE(SUM(status = 'wishlist'), 0),
			COALESCE(SUM(CASE WHEN e.status IN ('backlog','playing')
			                  THEN g.time_to_beat_main ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'played'
			                  THEN g.time_to_beat_main ELSE 0 END), 0),
			COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.user_id = ?), 0)
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ?`, userID, userID).
		Scan(&st.Total, &st.Backlog, &st.Playing, &st.Played, &st.Dropped, &st.Wishlist,
			&st.BacklogHours, &st.PlayedHours, &loggedMinutes)
	if err != nil {
		return st, err
	}
	// time_to_beat is stored in seconds; logged sessions in minutes.
	st.BacklogHours = round1(st.BacklogHours / 3600)
	st.PlayedHours = round1(st.PlayedHours / 3600)
	st.LoggedHours = round1(loggedMinutes / 60)

	// Completion measures games you own, so wishlist entries are excluded from
	// the denominator — wanting more games shouldn't lower your progress.
	owned := st.Total - st.Wishlist
	if owned > 0 {
		st.Completion = round1(float64(st.Played) / float64(owned) * 100)
	}
	return st, nil
}

// Facets returns the platforms and genres present in a user's library, for the
// filter rail. Only values that would actually match anything are returned.
func (s *Store) Facets(ctx context.Context, userID string) (platforms, genres []models.NamedRef, err error) {
	platforms, err = s.facet(ctx, userID, `
		SELECT DISTINCT p.id, p.name
		FROM library_entries e
		JOIN game_platforms gp ON gp.game_id = e.game_id
		JOIN platforms p ON p.id = gp.platform_id
		WHERE e.user_id = ? ORDER BY p.name`)
	if err != nil {
		return nil, nil, err
	}
	genres, err = s.facet(ctx, userID, `
		SELECT DISTINCT gn.id, gn.name
		FROM library_entries e
		JOIN game_genres gg ON gg.game_id = e.game_id
		JOIN genres gn ON gn.id = gg.genre_id
		WHERE e.user_id = ? ORDER BY gn.name`)
	if err != nil {
		return nil, nil, err
	}
	return platforms, genres, nil
}

func (s *Store) facet(ctx context.Context, userID, query string) ([]models.NamedRef, error) {
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.NamedRef{}
	for rows.Next() {
		var ref models.NamedRef
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// queryEntries runs an entry query and hydrates the embedded game records.
func (s *Store) queryEntries(ctx context.Context, query string, args ...any) ([]models.Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []models.Entry{}
	gameIDs := []int64{}
	for rows.Next() {
		var e models.Entry
		var gameID int64
		if err := rows.Scan(&e.ID, &e.Status, &e.PlatformID, &e.UserRating, &e.Notes,
			&e.QueuePosition, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.UpdatedAt, &gameID,
			&e.LoggedMinutes); err != nil {
			return nil, err
		}
		e.Game.ID = gameID
		entries = append(entries, e)
		gameIDs = append(gameIDs, gameID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	games, err := s.gamesByID(ctx, gameIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if g, ok := games[entries[i].Game.ID]; ok {
			entries[i].Game = g
		}
	}
	return entries, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer("%", "", "_", "")
	return r.Replace(s)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
