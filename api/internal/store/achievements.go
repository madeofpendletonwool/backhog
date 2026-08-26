package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// unlockStub is a newly inserted unlock, before hydration. The insert's
// RETURNING tells "newly unlocked" (for the UI toast) from "already had it",
// which is what makes evaluation idempotent.
type unlockStub struct {
	achievementID string
	entryID       string
	unlockedAt    time.Time
}

// evaluateAchievementsTx runs the catalogue against one triggering event,
// inside the caller's transaction so the unlock lands atomically with the
// status change or session that earned it. The entry snapshot is read in-tx
// and therefore includes the mutation that triggered evaluation.
func evaluateAchievementsTx(ctx context.Context, tx *sql.Tx, userID, entryID, kind string) ([]unlockStub, error) {
	var e achievements.Entry
	var finishedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT e.status, e.created_at, e.finished_at,
		       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0),
		       g.time_to_beat_main, g.time_to_beat_complete
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.id = ?`, userID, entryID).
		Scan(&e.Status, &e.CreatedAt, &finishedAt, &e.LoggedMinutes,
			&e.TimeToBeatMain, &e.TimeToBeatComplete)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ID = entryID
	e.At = time.Now()
	if finishedAt.Valid {
		e.At = finishedAt.Time
	}

	if err := snapshotAggregatesTx(ctx, tx, userID, &e); err != nil {
		return nil, err
	}

	var newly []unlockStub
	for _, def := range achievements.Catalogue {
		if !def.Predicate(achievements.Event{Kind: kind, Entry: e}) {
			continue
		}
		at, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, entryID)
		if err != nil {
			return nil, err
		}
		if at != nil {
			newly = append(newly, unlockStub{
				achievementID: def.Achievement.ID, entryID: entryID, unlockedAt: *at,
			})
		}
	}
	return newly, nil
}

// snapshotAggregatesTx fills the user-level aggregates a predicate needs: how
// many games are finished, and whether this entry is the oldest game the user
// owns and still means to finish.
func snapshotAggregatesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) error {
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND status = 'played'`,
		userID).Scan(&e.PlayedCount); err != nil {
		return err
	}
	oldest, err := oldestOwnedTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	e.IsOldestOwned = oldest != nil && !e.CreatedAt.After(*oldest)
	return nil
}

// oldestOwnedTx returns the earliest created_at among the games the user owns
// and still means to finish. MIN() over SQLite strings keeps lexicographic
// order, which matches timestamp order, so the raw string is parsed here.
func oldestOwnedTx(ctx context.Context, tx *sql.Tx, userID string) (*time.Time, error) {
	var raw sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT MIN(created_at) FROM library_entries
		WHERE user_id = ? AND status NOT IN ('wishlist','ignored')`, userID).Scan(&raw)
	if err != nil || !raw.Valid {
		return nil, err
	}
	t, ok := parseDBTime(raw.String)
	if !ok {
		return nil, nil
	}
	return &t, nil
}

// parseDBTime reads a SQLite timestamp or date string. Aggregates like MIN()
// and expressions like COALESCE() lose the column's declared type, so their
// results arrive as plain strings.
func parseDBTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// insertUnlockTx records an unlock idempotently and returns its timestamp, or
// nil when the user already had it.
func insertUnlockTx(ctx context.Context, tx *sql.Tx, userID, achievementID, entryID string) (*time.Time, error) {
	var at time.Time
	err := tx.QueryRowContext(ctx, `
		INSERT OR IGNORE INTO achievement_unlocks (id, user_id, achievement_id, entry_id)
		VALUES (?, ?, ?, ?) RETURNING unlocked_at`,
		newID(), userID, achievementID, entryID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &at, nil
}

// BackfillAchievements evaluates the whole catalogue against a user's existing
// library, so a long-time user opens the gallery with their history already
// counted instead of starting at zero. Idempotent — re-running (the gallery
// does it on every load) only fills gaps, such as achievements added to the
// catalogue in a later release.
//
// Historical data has limits the live hooks don't: games finished before the
// feature may have no finished_at (counted at their created_at), and logged
// minutes are today's totals, not the at-completion figure.
func (s *Store) BackfillAchievements(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.backfillAchievementsTx(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) backfillAchievementsTx(ctx context.Context, tx *sql.Tx, userID string) error {
	// Finished games in completion order, so a running count attaches
	// count-based achievements to the game that crossed the line. The
	// finished timestamp is read as a string because COALESCE() strips the
	// column's declared type.
	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, e.created_at, COALESCE(e.finished_at, e.created_at),
		       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0),
		       g.time_to_beat_main, g.time_to_beat_complete
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.status = 'played'
		ORDER BY COALESCE(e.finished_at, e.created_at) ASC, e.id`, userID)
	if err != nil {
		return err
	}
	played := []achievements.Entry{}
	for rows.Next() {
		var e achievements.Entry
		var finishedAt string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &finishedAt, &e.LoggedMinutes,
			&e.TimeToBeatMain, &e.TimeToBeatComplete); err != nil {
			rows.Close()
			return err
		}
		e.Status = models.StatusPlayed
		if at, ok := parseDBTime(finishedAt); ok {
			e.At = at
		} else {
			e.At = e.CreatedAt
		}
		played = append(played, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	oldest, err := oldestOwnedTx(ctx, tx, userID)
	if err != nil {
		return err
	}

	for i := range played {
		played[i].PlayedCount = i + 1
		played[i].IsOldestOwned = oldest != nil && !played[i].CreatedAt.After(*oldest)
		ev := achievements.Event{Kind: achievements.EventFinished, Entry: played[i]}
		for _, def := range achievements.Catalogue {
			if !def.Predicate(ev) {
				continue
			}
			if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, played[i].ID); err != nil {
				return err
			}
		}
	}

	// Dropped games only feed Abandonment Issues.
	rows, err = tx.QueryContext(ctx, `
		SELECT e.id, e.created_at, COALESCE(e.finished_at, e.created_at)
		FROM library_entries e
		WHERE e.user_id = ? AND e.status = 'dropped'`, userID)
	if err != nil {
		return err
	}
	dropped := []achievements.Entry{}
	for rows.Next() {
		var e achievements.Entry
		var finishedAt string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &finishedAt); err != nil {
			rows.Close()
			return err
		}
		e.Status = models.StatusDropped
		if at, ok := parseDBTime(finishedAt); ok {
			e.At = at
		} else {
			e.At = e.CreatedAt
		}
		dropped = append(dropped, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range dropped {
		ev := achievements.Event{Kind: achievements.EventDropped, Entry: e}
		for _, def := range achievements.Catalogue {
			if !def.Predicate(ev) {
				continue
			}
			if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Achievements returns the full catalogue merged with the user's unlock state,
// running the historical backfill first so the gallery is complete on first
// open. Locked entries carry no date and no game.
func (s *Store) Achievements(ctx context.Context, userID string) ([]models.AchievementStatus, error) {
	if err := s.BackfillAchievements(ctx, userID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT achievement_id, unlocked_at, COALESCE(entry_id, '')
		FROM achievement_unlocks WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type unlock struct {
		at      time.Time
		entryID string
	}
	byAchievement := map[string]unlock{}
	for rows.Next() {
		var id, entryID string
		var u unlock
		if err := rows.Scan(&id, &u.at, &entryID); err != nil {
			return nil, err
		}
		u.entryID = entryID
		byAchievement[id] = u
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.AchievementStatus, 0, len(achievements.Catalogue))
	for _, def := range achievements.Catalogue {
		status := models.AchievementStatus{Achievement: def.Achievement}
		if u, ok := byAchievement[def.Achievement.ID]; ok {
			at := u.at
			status.UnlockedAt = &at
			if u.entryID != "" {
				if entry, err := s.GetEntry(ctx, userID, u.entryID); err == nil {
					status.Entry = &entry
				}
			}
		}
		out = append(out, status)
	}
	return out, nil
}

// hydrateUnlocks turns freshly inserted unlock stubs into toast-ready
// statuses. Hydration failures (a deleted entry, say) drop the game reference
// rather than the unlock.
func (s *Store) hydrateUnlocks(ctx context.Context, userID string, stubs []unlockStub) []models.AchievementStatus {
	out := make([]models.AchievementStatus, 0, len(stubs))
	for _, stub := range stubs {
		def := achievements.ByID(stub.achievementID)
		if def == nil {
			continue
		}
		at := stub.unlockedAt
		status := models.AchievementStatus{Achievement: *def, UnlockedAt: &at}
		if stub.entryID != "" {
			if entry, err := s.GetEntry(ctx, userID, stub.entryID); err == nil {
				status.Entry = &entry
			}
		}
		out = append(out, status)
	}
	return out
}

// Season derives the per-calendar-year "Backlog Challenge" rollup on demand:
// games finished in the year, hours logged in the year, franchises fully
// cleared in the year, and backlog rescues (finished after a year or more of
// ownership). Series data arriving late (the series backfill is gradual) just
// means the franchise count grows into place — no failure mode.
func (s *Store) Season(ctx context.Context, userID string, year int) (models.Season, error) {
	var season models.Season
	season.Year = year
	start := fmt.Sprintf("%04d-01-01", year)
	end := fmt.Sprintf("%04d-01-01", year+1)

	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN e.created_at <= date(e.finished_at, '-1 year') THEN 1 ELSE 0 END), 0)
		FROM library_entries e
		WHERE e.user_id = ? AND e.status = 'played'
		  AND e.finished_at IS NOT NULL
		  AND e.finished_at >= ? AND e.finished_at < ?`,
		userID, start, end).Scan(&season.GamesCompleted, &season.Rescues)
	if err != nil {
		return season, err
	}

	var minutes float64
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(minutes), 0) FROM play_sessions
		WHERE user_id = ? AND played_on >= ? AND played_on < ?`,
		userID, start, end).Scan(&minutes)
	if err != nil {
		return season, err
	}
	season.HoursPlayed = round1(minutes / 60)

	// A franchise is cleared the year its last owned member was finished.
	// Owned means at least two non-wishlist, non-ignored members — the same
	// bar as the series index — all of them played.
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT s.id
			FROM series s
			JOIN series_games sg ON sg.series_id = s.id
			JOIN library_entries e ON e.game_id = sg.game_id AND e.user_id = ?
			WHERE e.status NOT IN ('wishlist','ignored')
			GROUP BY s.id
			HAVING COUNT(*) >= 2
			   AND SUM(e.status = 'played') = COUNT(*)
			   AND MAX(e.finished_at) >= ? AND MAX(e.finished_at) < ?
		)`, userID, start, end).Scan(&season.FranchisesCleared)
	if err != nil {
		return season, err
	}

	return season, nil
}
