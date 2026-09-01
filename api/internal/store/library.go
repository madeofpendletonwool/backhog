package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// LibraryFilter describes a library query. Zero values mean "no filter".
type LibraryFilter struct {
	Status     string
	MediaType  string
	Query      string
	PlatformID *int64
	GenreID    *int64
	ListID     string
	// Author / Subject / Language are book facets; they never match a game.
	Author   string
	Subject  string
	Language string
	Sort     string
	Limit    int
	Offset   int
}

// entrySelect is the shared projection for entry queries. Both subjects are
// LEFT JOINed so a book row (game_id NULL) survives the query and vice versa;
// queryEntries hydrates whichever side media_type points at. The games side of
// the join keeps the sort keys and text filters below working unchanged — a
// LEFT JOIN on games.id costs the same index probe the old inner join paid.
// Genres and platforms are attached separately by hydrate.
const entrySelect = `
	SELECT e.id, e.media_type, e.status, e.platform_id, e.user_rating, e.notes, e.queue_position,
	       e.started_at, e.finished_at, e.created_at, e.updated_at, e.game_id, e.book_id,
	       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0)
	FROM library_entries e
	LEFT JOIN games g ON g.id = e.game_id
	LEFT JOIN books b ON b.id = e.book_id`

// sortClauses whitelists user-supplied sort keys. Never interpolate raw input.
// Shared keys (added / updated / queue) order any media mix. "name" and
// "title" coalesce across subjects so a mixed-media page stays one sorted
// list; the book-only keys (author, published, pages) push games to the end
// via NULLS LAST, exactly as missing values already behave on the game keys.
// The "pages" key reads page counts through the ed alias that ListEntries
// joins in, falling back to the work's earliest edition when the entry has
// not picked a printing yet.
var sortClauses = map[string]string{
	"added":     "e.created_at DESC",
	"name":      "COALESCE(g.name, b.title) COLLATE NOCASE ASC",
	"title":     "COALESCE(b.title, g.name) COLLATE NOCASE ASC",
	"author":    "json_extract(b.authors_json, '$[0]') COLLATE NOCASE ASC",
	"released":  "g.first_release_date DESC NULLS LAST",
	"published": "b.first_publish_year DESC NULLS LAST",
	"rating":    "g.igdb_rating DESC NULLS LAST",
	"shortest":  "g.time_to_beat_main ASC NULLS LAST",
	"longest":   "g.time_to_beat_main DESC NULLS LAST",
	"pages": `COALESCE(ed.page_count,
		(SELECT ed2.page_count FROM book_editions ed2
		 WHERE ed2.book_id = e.book_id AND ed2.page_count IS NOT NULL
		 ORDER BY ed2.published_year, ed2.id LIMIT 1)) ASC NULLS LAST`,
	"updated": "e.updated_at DESC",
	"queue":   "e.queue_position ASC NULLS LAST, e.created_at ASC",
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
		INSERT INTO library_entries (id, user_id, media_type, game_id, status, platform_id, queue_position, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, models.MediaGame, gameID, status, platformID, queuePos, started)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Entry{}, ErrConflict
		}
		return models.Entry{}, err
	}
	return s.GetEntry(ctx, userID, id)
}

// ErrEditionMismatch is returned by AddBookEntry when the supplied edition
// belongs to a different work than the book being added.
var ErrEditionMismatch = errors.New("edition does not belong to that book")

// AddBookEntry puts a book into a user's library. The work must already exist
// in the shared cache; editionID is optional (a reader may not know which
// printing they own) but must belong to the work when set — page maps key off
// the edition, so anchoring the wrong one silently would corrupt stage 10.
// Returns ErrConflict if the user already has the work.
func (s *Store) AddBookEntry(ctx context.Context, userID, bookID string, editionID *string, status string) (models.Entry, error) {
	if !models.ValidStatus(status) {
		status = models.StatusBacklog
	}
	if editionID != nil && *editionID != "" {
		var owner string
		err := s.db.QueryRowContext(ctx,
			`SELECT book_id FROM book_editions WHERE id = ?`, *editionID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != bookID) {
			return models.Entry{}, ErrEditionMismatch
		}
		if err != nil {
			return models.Entry{}, err
		}
	} else {
		editionID = nil
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
		INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status, queue_position, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, models.MediaBook, bookID, editionID, status, queuePos, started)
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
	if models.ValidMediaType(f.MediaType) {
		where = append(where, "e.media_type = ?")
		args = append(args, f.MediaType)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `(COALESCE(g.name, b.title) LIKE ? COLLATE NOCASE
			OR EXISTS (SELECT 1 FROM json_each(b.authors_json) je WHERE je.value LIKE ? COLLATE NOCASE))`)
		pattern := "%" + escapeLike(q) + "%"
		args = append(args, pattern, pattern)
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
	if v := strings.TrimSpace(f.Author); v != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM json_each(b.authors_json) je WHERE je.value = ? COLLATE NOCASE)`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(f.Subject); v != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM json_each(b.subjects_json) je WHERE je.value = ? COLLATE NOCASE)`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(f.Language); v != "" {
		// The language of the printing: the entry's own edition when it has
		// one, otherwise the work's earliest edition.
		where = append(where, `COALESCE(
			(SELECT ed.language FROM book_editions ed WHERE ed.id = e.edition_id),
			(SELECT ed2.language FROM book_editions ed2
			 WHERE ed2.book_id = e.book_id AND ed2.language <> ''
			 ORDER BY ed2.published_year, ed2.id LIMIT 1)) = ?`)
		args = append(args, v)
	}

	orderBy, ok := sortClauses[f.Sort]
	if !ok {
		orderBy = sortClauses["added"]
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}

	// The editions join feeds the "pages" sort; it is a primary-key probe per
	// row, so it rides along on every sorted page rather than branching on
	// which sort key arrived.
	query := entrySelect + `
		LEFT JOIN book_editions ed ON ed.id = e.edition_id` +
		" WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + orderBy + " LIMIT ? OFFSET ?"
	args = append(args, limit, max(f.Offset, 0))

	return s.queryEntries(ctx, query, args...)
}

// CountEntries returns the total matching a filter, for pagination.
func (s *Store) CountEntries(ctx context.Context, userID string, f LibraryFilter) (int, error) {
	var count int
	args := []any{userID}
	query := `SELECT COUNT(*) FROM library_entries e
		LEFT JOIN games g ON g.id = e.game_id
		LEFT JOIN books b ON b.id = e.book_id
		WHERE e.user_id = ?`
	if models.ValidStatus(f.Status) {
		query += " AND e.status = ?"
		args = append(args, f.Status)
	}
	if models.ValidMediaType(f.MediaType) {
		query += " AND e.media_type = ?"
		args = append(args, f.MediaType)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		query += ` AND (COALESCE(g.name, b.title) LIKE ? COLLATE NOCASE
			OR EXISTS (SELECT 1 FROM json_each(b.authors_json) je WHERE je.value LIKE ? COLLATE NOCASE))`
		pattern := "%" + escapeLike(q) + "%"
		args = append(args, pattern, pattern)
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// EntryUpdate carries the mutable fields of an entry. Nil means "leave alone".
type EntryUpdate struct {
	Status        *string
	PlatformID    *int64
	ClearPlatform bool
	UserRating    *int
	ClearRating   bool
	Notes         *string
}

// UpdateEntry applies a partial update. Status transitions stamp started_at and
// finished_at, append a row to the status history, and move the entry in or out
// of the play queue as it leaves or re-enters 'backlog', so the queue always
// reflects exactly what is still unplayed. Finishing, dropping, or resuming a
// dropped game evaluates achievements inside the same transaction; any newly
// unlocked ones are returned for the UI toast.
func (s *Store) UpdateEntry(ctx context.Context, userID, entryID string, u EntryUpdate) (models.Entry, []models.AchievementStatus, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Entry{}, nil, err
	}
	defer tx.Rollback()

	var currentStatus string
	var startedAt, finishedAt sql.NullTime
	var queuePos sql.NullFloat64
	err = tx.QueryRowContext(ctx,
		`SELECT status, started_at, finished_at, queue_position FROM library_entries WHERE user_id = ? AND id = ?`,
		userID, entryID).Scan(&currentStatus, &startedAt, &finishedAt, &queuePos)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Entry{}, nil, ErrNotFound
	}
	if err != nil {
		return models.Entry{}, nil, err
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	var args []any
	evalKind := ""
	// For a resumed entry whose drop predates the status history table, the
	// pre-update finished_at is the only record of when it was dropped.
	var droppedAtFallback *time.Time

	if u.Status != nil && *u.Status != currentStatus {
		if !models.ValidStatus(*u.Status) {
			return models.Entry{}, nil, fmt.Errorf("invalid status %q", *u.Status)
		}
		newStatus := *u.Status
		sets = append(sets, "status = ?")
		args = append(args, newStatus)

		if err := recordStatusChangeTx(ctx, tx, userID, entryID, currentStatus, newStatus); err != nil {
			return models.Entry{}, nil, err
		}

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
			evalKind = achievements.EventFinished
		case models.StatusDropped:
			sets = append(sets, "finished_at = CURRENT_TIMESTAMP")
			evalKind = achievements.EventDropped
		case models.StatusBacklog, models.StatusWishlist:
			sets = append(sets, "started_at = NULL", "finished_at = NULL")
		}

		// A dropped game coming back is a resume — the comeback
		// achievements key off it. Going to wishlist is leaving, not
		// returning; finishing a dropped game is just a finish (but the
		// comeback finish predicates still want the drop time).
		if currentStatus == models.StatusDropped &&
			(newStatus == models.StatusPlaying || newStatus == models.StatusBacklog ||
				newStatus == models.StatusPlayed) {
			if newStatus != models.StatusPlayed {
				evalKind = achievements.EventResumed
			}
			if finishedAt.Valid {
				t := finishedAt.Time
				droppedAtFallback = &t
			}
		}

		if newStatus == models.StatusBacklog {
			pos, err := nextQueuePositionTx(ctx, tx, userID)
			if err != nil {
				return models.Entry{}, nil, err
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

	// Next! needs the queue rank captured before the UPDATE clears the
	// finishing entry's position: the top of the queue is whoever holds
	// the smallest position among the backlog.
	wasQueueTop := false
	if evalKind == achievements.EventFinished && queuePos.Valid {
		var minPos sql.NullFloat64
		if err := tx.QueryRowContext(ctx,
			`SELECT MIN(queue_position) FROM library_entries
			 WHERE user_id = ? AND status = 'backlog' AND queue_position IS NOT NULL`,
			userID).Scan(&minPos); err != nil {
			return models.Entry{}, nil, err
		}
		wasQueueTop = minPos.Valid && queuePos.Float64 == minPos.Float64
	}

	switch {
	case u.ClearRating:
		sets = append(sets, "user_rating = NULL")
	case u.UserRating != nil:
		if *u.UserRating < 1 || *u.UserRating > 10 {
			return models.Entry{}, nil, fmt.Errorf("rating must be between 1 and 10")
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
		return models.Entry{}, nil, err
	}

	var newly []unlockStub
	if evalKind != "" {
		newly, err = evaluateAchievementsTx(ctx, tx, userID, entryID, evalKind, droppedAtFallback, wasQueueTop)
		if err != nil {
			return models.Entry{}, nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Entry{}, nil, err
	}
	entry, err := s.GetEntry(ctx, userID, entryID)
	if err != nil {
		return models.Entry{}, nil, err
	}
	return entry, s.hydrateUnlocks(ctx, userID, newly), nil
}

// recordStatusChangeTx appends one row to the entry's status history, inside
// the caller's transaction. Every transition writes a row — it is the only
// record of when a game was dropped or resumed once finished_at is wiped.
func recordStatusChangeTx(ctx context.Context, tx *sql.Tx, userID, entryID, fromStatus, toStatus string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status)
		VALUES (?, ?, ?, ?, ?)`,
		newID(), entryID, userID, fromStatus, toStatus)
	return err
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

// Stats summarises the library for the dashboard. Explicitly game-scoped: the
// dashboard is the games dashboard, and book entries must not inflate it once
// they exist.
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
			COALESCE(SUM(status = 'ignored'), 0),
			COALESCE(SUM(status = 'wishlist'), 0),
			COALESCE(SUM(CASE WHEN e.status IN ('backlog','playing')
			                  THEN g.time_to_beat_main ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'played'
			                  THEN g.time_to_beat_main ELSE 0 END), 0),
			COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.user_id = ?), 0)
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = 'game'`, userID, userID).
		Scan(&st.Total, &st.Backlog, &st.Playing, &st.Played, &st.Dropped, &st.Ignored, &st.Wishlist,
			&st.BacklogHours, &st.PlayedHours, &loggedMinutes)
	if err != nil {
		return st, err
	}
	// time_to_beat is stored in seconds; logged sessions in minutes.
	st.BacklogHours = round1(st.BacklogHours / 3600)
	st.PlayedHours = round1(st.PlayedHours / 3600)
	st.LoggedHours = round1(loggedMinutes / 60)

	// Completion measures games you mean to finish, so both wishlist (don't own)
	// and ignored (own, but never-ending — you'll never "beat" them) are excluded
	// from the denominator.
	owned := st.Total - st.Wishlist - st.Ignored
	if owned > 0 {
		st.Completion = round1(float64(st.Played) / float64(owned) * 100)
	}
	return st, nil
}

// Facets returns the platforms and genres present in a user's library, for the
// filter rail. Only values that would actually match anything are returned.
// Platforms carry their curated classification, degrading to family "other"
// for unclassified rows. Game-scoped by hand: books carry no platforms or
// genres, so they must never reach the rail even once they exist.
func (s *Store) Facets(ctx context.Context, userID string) (platforms []models.Platform, genres []models.NamedRef, err error) {
	prows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT p.id, p.name,
		       COALESCE(NULLIF(p.manufacturer, ''), 'Other'),
		       COALESCE(NULLIF(p.family, ''), 'other'),
		       p.generation, p.handheld
		FROM library_entries e
		JOIN game_platforms gp ON gp.game_id = e.game_id
		JOIN platforms p ON p.id = gp.platform_id
		WHERE e.user_id = ? AND e.media_type = 'game' ORDER BY p.name`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer prows.Close()

	platforms = []models.Platform{}
	for prows.Next() {
		var p models.Platform
		var generation sql.NullInt64
		if err := prows.Scan(&p.ID, &p.Name, &p.Manufacturer, &p.Family, &generation, &p.Handheld); err != nil {
			return nil, nil, err
		}
		if generation.Valid {
			g := int(generation.Int64)
			p.Generation = &g
		}
		platforms = append(platforms, p)
	}
	if err := prows.Err(); err != nil {
		return nil, nil, err
	}

	genres, err = s.facet(ctx, userID, `
		SELECT DISTINCT gn.id, gn.name
		FROM library_entries e
		JOIN game_genres gg ON gg.game_id = e.game_id
		JOIN genres gn ON gn.id = gg.genre_id
		WHERE e.user_id = ? AND e.media_type = 'game' ORDER BY gn.name`)
	if err != nil {
		return nil, nil, err
	}
	return platforms, genres, nil
}

// BookStats summarises a user's book library. The books counterpart of Stats,
// kept separate rather than widening the games query: the dashboard owns the
// reading numbers, this only feeds the library page's strip.
func (s *Store) BookStats(ctx context.Context, userID string) (models.BookStats, error) {
	var st models.BookStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(status = 'backlog'), 0),
			COALESCE(SUM(status = 'playing'), 0),
			COALESCE(SUM(status = 'played'), 0),
			COALESCE(SUM(status = 'dropped'), 0),
			COALESCE(SUM(status = 'ignored'), 0),
			COALESCE(SUM(status = 'wishlist'), 0)
		FROM library_entries
		WHERE user_id = ? AND media_type = 'book'`, userID).
		Scan(&st.Total, &st.Backlog, &st.Reading, &st.Read, &st.Dropped, &st.Ignored, &st.Wishlist)
	if err != nil {
		return st, err
	}
	// Same completion definition as games: wishlist and ignored leave the
	// denominator, since neither is a book you owe yourself a finish.
	owned := st.Total - st.Wishlist - st.Ignored
	if owned > 0 {
		st.Completion = round1(float64(st.Read) / float64(owned) * 100)
	}
	return st, nil
}

// BookFacets returns the authors, subjects, languages and statuses present in
// a user's book library, for the books filter rail. Language comes from the
// printing: the entry's own edition when set, else the work's earliest
// edition. Only values that would actually match anything are returned.
func (s *Store) BookFacets(ctx context.Context, userID string) (models.BookFacets, error) {
	out := models.BookFacets{
		Authors:   []string{},
		Subjects:  []string{},
		Languages: []string{},
		Statuses:  []string{},
	}

	authors, err := s.stringFacet(ctx, userID, `
		SELECT DISTINCT je.value
		FROM library_entries e
		JOIN books b ON b.id = e.book_id, json_each(b.authors_json) je
		WHERE e.user_id = ? AND e.media_type = 'book' AND je.value <> ''
		ORDER BY je.value COLLATE NOCASE`)
	if err != nil {
		return out, err
	}
	out.Authors = authors

	subjects, err := s.stringFacet(ctx, userID, `
		SELECT DISTINCT je.value
		FROM library_entries e
		JOIN books b ON b.id = e.book_id, json_each(b.subjects_json) je
		WHERE e.user_id = ? AND e.media_type = 'book' AND je.value <> ''
		ORDER BY je.value COLLATE NOCASE`)
	if err != nil {
		return out, err
	}
	out.Subjects = subjects

	languages, err := s.stringFacet(ctx, userID, `
		SELECT DISTINCT COALESCE(
			(SELECT ed.language FROM book_editions ed WHERE ed.id = e.edition_id),
			(SELECT ed2.language FROM book_editions ed2
			 WHERE ed2.book_id = e.book_id AND ed2.language <> ''
			 ORDER BY ed2.published_year, ed2.id LIMIT 1))
		FROM library_entries e
		WHERE e.user_id = ? AND e.media_type = 'book'`)
	if err != nil {
		return out, err
	}
	out.Languages = languages

	statuses, err := s.stringFacet(ctx, userID, `
		SELECT DISTINCT status FROM library_entries
		WHERE user_id = ? AND media_type = 'book' ORDER BY status`)
	if err != nil {
		return out, err
	}
	out.Statuses = statuses
	return out, nil
}

// stringFacet runs a single-column facet query in library-entry scope.
func (s *Store) stringFacet(ctx context.Context, userID, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if v.Valid && v.String != "" {
			out = append(out, v.String)
		}
	}
	return out, rows.Err()
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

// queryEntries runs an entry query and hydrates the embedded subject record:
// the game (with genres and platforms) for game rows, the work for book rows.
func (s *Store) queryEntries(ctx context.Context, query string, args ...any) ([]models.Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []models.Entry{}
	gameIDs := []int64{}
	bookIDs := []string{}
	for rows.Next() {
		var e models.Entry
		var gameID sql.NullInt64
		var bookID sql.NullString
		if err := rows.Scan(&e.ID, &e.MediaType, &e.Status, &e.PlatformID, &e.UserRating, &e.Notes,
			&e.QueuePosition, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.UpdatedAt, &gameID, &bookID,
			&e.LoggedMinutes); err != nil {
			return nil, err
		}
		if gameID.Valid {
			e.Game = &models.Game{ID: gameID.Int64}
			gameIDs = append(gameIDs, gameID.Int64)
		}
		if bookID.Valid && bookID.String != "" {
			e.Book = &models.Book{ID: bookID.String}
			bookIDs = append(bookIDs, bookID.String)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	games, err := s.gamesByID(ctx, gameIDs)
	if err != nil {
		return nil, err
	}
	books, err := s.BooksByIDs(ctx, bookIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Game != nil {
			if g, ok := games[entries[i].Game.ID]; ok {
				entries[i].Game = &g
			}
		}
		if entries[i].Book != nil {
			if b, ok := books[entries[i].Book.ID]; ok {
				entries[i].Book = &b
			}
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
