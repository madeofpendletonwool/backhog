package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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
// droppedAtFallback carries the pre-update finished_at for resumed entries
// whose drop predates the status history table. wasQueueTop carries the
// entry's top-of-queue state from before the finishing update cleared its
// position — the snapshot itself can no longer see it.
func evaluateAchievementsTx(ctx context.Context, tx *sql.Tx, userID, entryID, kind string, droppedAtFallback *time.Time, wasQueueTop bool) ([]unlockStub, error) {
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
	e.WasQueueTop = wasQueueTop

	e.DroppedAt, err = lastDroppedAtTx(ctx, tx, entryID, droppedAtFallback)
	if err != nil {
		return nil, err
	}
	e.DropHistory, err = loadDropHistoryTx(ctx, tx, entryID, droppedAtFallback)
	if err != nil {
		return nil, err
	}

	if err := snapshotAggregatesTx(ctx, tx, userID, &e); err != nil {
		return nil, err
	}

	var newly []unlockStub
	for _, def := range achievements.Catalogue {
		if def.Predicate == nil || !def.Predicate(achievements.Event{Kind: kind, Entry: e}) {
			continue
		}
		at, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, &entryID)
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

// snapshotAggregatesTx fills the user- and game-level aggregates a predicate
// needs: finish and drop counts, the unplayed backlog size, the entry's rank
// by ownership age, and the series the game belongs to. Straightforward
// queries are fine — libraries are hundreds of rows, not millions.
func snapshotAggregatesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) error {
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND status = 'played'`,
		userID).Scan(&e.PlayedCount); err != nil {
		return err
	}

	// DroppedCount counts drop events, not currently-dropped entries: a
	// drop-and-resume still happened. Entries dropped before the history
	// table existed only count while they remain dropped.
	if err := tx.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(DISTINCT h.entry_id) FROM entry_status_history h
			 JOIN library_entries d ON d.id = h.entry_id
			 WHERE d.user_id = ? AND h.to_status = 'dropped')
			+
			(SELECT COUNT(*) FROM library_entries d
			 WHERE d.user_id = ? AND d.status = 'dropped'
			   AND NOT EXISTS (SELECT 1 FROM entry_status_history h
			                   WHERE h.entry_id = d.id AND h.to_status = 'dropped'))`,
		userID, userID).Scan(&e.DroppedCount); err != nil {
		return err
	}

	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND status IN ('backlog','playing')`,
		userID).Scan(&e.UnplayedCount); err != nil {
		return err
	}

	// The peak-unplayed sweep: one pass over every owned entry's
	// contribution interval, evaluated in Go. Libraries are hundreds of
	// rows, not millions.
	timeline, err := loadUnplayedTimelineTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	_, e.PeakUnplayedCount = timeline.stateAt(e.At)

	// Backlog Negative compares this calendar year's finishes and
	// acquisitions at the event's moment.
	year := fmt.Sprintf("%04d", e.At.Year())
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries
		WHERE user_id = ? AND status = 'played'
		  AND finished_at IS NOT NULL AND strftime('%Y', finished_at) = ?`,
		userID, year).Scan(&e.YearFinishes); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries
		WHERE user_id = ? AND status <> 'wishlist'
		  AND strftime('%Y', created_at) = ?`,
		userID, year).Scan(&e.YearAdditions); err != nil {
		return err
	}

	// The calendar aggregates: finishes bucketed into At's month for the
	// hat-trick ladder, the distinct months of At's year for a perfect
	// season, and the June–August window — all against the same played
	// population the finish count reads.
	if err := tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN strftime('%Y-%m', finished_at) = ? THEN 1 ELSE 0 END), 0),
			COUNT(DISTINCT CASE WHEN strftime('%Y', finished_at) = ? THEN strftime('%m', finished_at) END),
			COALESCE(SUM(CASE WHEN strftime('%Y', finished_at) = ?
				AND CAST(strftime('%m', finished_at) AS INTEGER) BETWEEN 6 AND 8 THEN 1 ELSE 0 END), 0)
		FROM library_entries
		WHERE user_id = ? AND status = 'played' AND finished_at IS NOT NULL`,
		monthKeyOf(e.At), year, year, userID).
		Scan(&e.MonthFinishes, &e.YearMonthsFinished, &e.SummerFinishes); err != nil {
		return err
	}

	// The consecutive-month streak walks back from At's month over the
	// months that saw a finish.
	finishMonths := map[string]int{}
	mrows, err := tx.QueryContext(ctx, `
		SELECT strftime('%Y-%m', finished_at), COUNT(*) FROM library_entries
		WHERE user_id = ? AND status = 'played' AND finished_at IS NOT NULL
		GROUP BY 1`, userID)
	if err != nil {
		return err
	}
	for mrows.Next() {
		var key string
		var n int
		if err := mrows.Scan(&key, &n); err != nil {
			mrows.Close()
			return err
		}
		finishMonths[key] = n
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return err
	}
	e.FinishStreak = finishMonthStreak(finishMonths, e.At)

	// The 50h+ ladder: how many long-haul games the user has finished.
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.status = 'played' AND g.time_to_beat_main >= ?`,
		userID, achievements.LongHaulSeconds).Scan(&e.LongHaulFinishes); err != nil {
		return err
	}

	// Rank among owned, finishable entries — same population as IsOldestOwned.
	// Timestamps are TEXT, so the comparison is lexicographic = chronological.
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) + 1 FROM library_entries
		WHERE user_id = ? AND status NOT IN ('wishlist','ignored')
		  AND created_at < (SELECT created_at FROM library_entries WHERE id = ?)`,
		userID, e.ID).Scan(&e.CreatedAtRank); err != nil {
		return err
	}
	e.IsOldestOwned = e.CreatedAtRank == 1

	rows, err := tx.QueryContext(ctx, `
		SELECT sg.series_id FROM series_games sg
		WHERE sg.game_id = (SELECT game_id FROM library_entries WHERE id = ?)
		ORDER BY sg.series_id`, e.ID)
	if err != nil {
		return err
	}
	e.SeriesIDs = []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		e.SeriesIDs = append(e.SeriesIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// The platform the user chose and the game's original release date ride
	// the same snapshot so predicates read one struct.
	if err := tx.QueryRowContext(ctx, `
		SELECT e.platform_id, g.first_release_date
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.id = ?`, e.ID).Scan(&e.PlatformID, &e.FirstReleaseDate); err != nil {
		return err
	}

	e.FinishYear, e.FinishMonth = e.At.Year(), int(e.At.Month())
	return nil
}

// timelineEvent is one endpoint of an entry's unplayed-contribution
// interval: +1 at acquisition, -1 when it stops being unplayed.
type timelineEvent struct {
	at    time.Time
	delta int
}

// unplayedTimeline answers peak/current unplayed-count queries by sweeping
// the contribution intervals of every owned entry. An entry contributes
// from created_at until finished_at — which is the finish stamp on a played
// game and the drop stamp on a dropped one — or until now when it is still
// backlog or playing. Wishlist and ignored entries never contribute. Known
// approximation, accepted by design: a wishlist→backlog promotion keeps its
// original created_at, so it counts as unplayed from acquisition.
type unplayedTimeline struct {
	events []timelineEvent // sorted by (at, delta): interval ends before starts
}

// loadUnplayedTimelineTx reads every owned entry's interval endpoints.
// Pre-feature played/dropped rows without finished_at collapse to an empty
// interval at created_at, mirroring the backfill's timestamp fallback.
func loadUnplayedTimelineTx(ctx context.Context, tx *sql.Tx, userID string) (*unplayedTimeline, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT status, created_at, finished_at FROM library_entries
		WHERE user_id = ? AND status NOT IN ('wishlist','ignored')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tl := &unplayedTimeline{}
	for rows.Next() {
		var status, createdRaw string
		var finishedRaw sql.NullString
		if err := rows.Scan(&status, &createdRaw, &finishedRaw); err != nil {
			return nil, err
		}
		created, ok := parseDBTime(createdRaw)
		if !ok {
			continue
		}
		tl.events = append(tl.events, timelineEvent{at: created, delta: 1})
		switch status {
		case models.StatusBacklog, models.StatusPlaying:
			// Still unplayed: the interval runs to now, no end event.
		default:
			endRaw := finishedRaw.String
			if !finishedRaw.Valid {
				endRaw = createdRaw
			}
			if end, ok := parseDBTime(endRaw); ok {
				tl.events = append(tl.events, timelineEvent{at: end, delta: -1})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(tl.events, func(i, j int) bool {
		if !tl.events[i].at.Equal(tl.events[j].at) {
			return tl.events[i].at.Before(tl.events[j].at)
		}
		return tl.events[i].delta < tl.events[j].delta
	})
	return tl, nil
}

// stateAt returns the unplayed count at t and the peak count up to t, in
// one pass over the sorted events.
func (tl *unplayedTimeline) stateAt(t time.Time) (current, peak int) {
	count := 0
	for _, ev := range tl.events {
		if ev.at.After(t) {
			break
		}
		count += ev.delta
		if count > peak {
			peak = count
		}
	}
	return count, peak
}

// monthKeyOf buckets a moment into its UTC calendar month, "YYYY-MM" — the
// same bucket strftime('%Y-%m') reads from stored timestamps.
func monthKeyOf(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
}

// finishMonthStreak walks back from at's calendar month, counting the
// consecutive months present in months. at's own month counts when it holds
// a finish — the caller just landed one there.
func finishMonthStreak(months map[string]int, at time.Time) int {
	m := at.UTC()
	m = time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
	streak := 0
	for months[monthKeyOf(m)] > 0 {
		streak++
		m = m.AddDate(0, -1, 0)
	}
	return streak
}

// additions returns every non-wishlist acquisition timestamp in order —
// ignored entries included, an acquisition is an acquisition — for the
// backfill's running same-year addition counts.
func additionsTx(ctx context.Context, tx *sql.Tx, userID string) ([]time.Time, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT created_at FROM library_entries
		WHERE user_id = ? AND status <> 'wishlist'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if t, ok := parseDBTime(raw); ok {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

// lastDroppedAtTx returns when the entry was last dropped: the newest
// 'dropped' row in its status history, falling back to finished_at for
// pre-feature drops (finished_at is stamped on drop and wiped on resume, so
// the fallback only carries information the history table lacks).
func lastDroppedAtTx(ctx context.Context, tx *sql.Tx, entryID string, fallback *time.Time) (*time.Time, error) {
	var raw sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT changed_at FROM entry_status_history
		WHERE entry_id = ? AND to_status = 'dropped'
		ORDER BY changed_at DESC, id DESC LIMIT 1`, entryID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil || !raw.Valid {
		return nil, err
	}
	t, ok := parseDBTime(raw.String)
	if !ok {
		return fallback, nil
	}
	return &t, nil
}

// loadDropHistoryTx assembles the entry's drop arcs from its status history,
// oldest first: each to_status='dropped' row opens an arc, each
// dropped → playing|backlog row closes the newest open one. Going to
// wishlist or ignored is leaving, not returning, so those rows close nothing.
// A resume row with no arc to close is a pre-feature drop — the fallback
// (the pre-update finished_at) stands in for its drop time.
func loadDropHistoryTx(ctx context.Context, tx *sql.Tx, entryID string, fallback *time.Time) ([]achievements.DropCycle, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT from_status, to_status, changed_at FROM entry_status_history
		WHERE entry_id = ?
		ORDER BY changed_at, rowid`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cycles := []achievements.DropCycle{}
	for rows.Next() {
		var from, to, raw string
		if err := rows.Scan(&from, &to, &raw); err != nil {
			return nil, err
		}
		at, ok := parseDBTime(raw)
		if !ok {
			continue
		}
		switch {
		case to == models.StatusDropped:
			cycles = append(cycles, achievements.DropCycle{DroppedAt: at})
		case from == models.StatusDropped &&
			(to == models.StatusPlaying || to == models.StatusBacklog):
			closed := false
			for i := len(cycles) - 1; i >= 0; i-- {
				if cycles[i].ResumedAt == nil {
					resumed := at
					cycles[i].ResumedAt = &resumed
					closed = true
					break
				}
			}
			if !closed && fallback != nil {
				cycles = append(cycles, achievements.DropCycle{
					DroppedAt: *fallback, ResumedAt: &at,
				})
			}
		}
	}
	return cycles, rows.Err()
}

// ownedCreatedAtTx returns the created_at stamps of every owned,
// finishable entry — the population ownership-age ranks are measured
// against — sorted ascending. Timestamps parse in UTC so ranks compare
// against the same instants the live snapshot's SQL ranks.
func ownedCreatedAtTx(ctx context.Context, tx *sql.Tx, userID string) ([]time.Time, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT created_at FROM library_entries
		WHERE user_id = ? AND status NOT IN ('wishlist','ignored')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if t, ok := parseDBTime(raw); ok {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

// rankAmong returns at's position in the sorted stamps (oldest = 1),
// counting only strictly-earlier stamps so ties share a rank — the same
// semantics as the live snapshot's COUNT(created_at < ...) + 1.
func rankAmong(stamps []time.Time, at time.Time) int {
	return sort.Search(len(stamps), func(i int) bool {
		return !stamps[i].Before(at)
	}) + 1
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
// nil when the user already had it. entryID is nil for unlocks with no
// triggering game (time-window achievements).
func insertUnlockTx(ctx context.Context, tx *sql.Tx, userID, achievementID string, entryID *string) (*time.Time, error) {
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
		       g.time_to_beat_main, g.time_to_beat_complete, g.first_release_date
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
			&e.TimeToBeatMain, &e.TimeToBeatComplete, &e.FirstReleaseDate); err != nil {
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

	// Ownership-age ranks are measured against today's owned population,
	// the same one the live snapshot ranks against — the generalization
	// IsOldestOwned has always used.
	ownedStamps, err := ownedCreatedAtTx(ctx, tx, userID)
	if err != nil {
		return err
	}

	// The sweep backs the peak-reduction predicates, and the additions
	// walk keeps same-year acquisition counts honest at each historical
	// finish — a game added in March must not count against a February
	// finish. Finishes replay in ascending At order, so one pointer
	// serves the whole pass.
	timeline, err := loadUnplayedTimelineTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	additions, err := additionsTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	// The comeback arcs: drop cycles for the finish predicates and the
	// resume events to replay. Pre-feature data has no history rows, so
	// comebacks backfill only where history exists — the gap fills in as
	// users actually cycle games.
	arcs, resumes, err := loadUserDropArcsTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	yearFinishes, yearAdditions := map[int]int{}, map[int]int{}
	ai := 0
	// The calendar aggregates grow as finishes replay in order, exactly as
	// they did live: month buckets, the distinct months per year behind a
	// perfect season, the summer window, and the 50h+ count.
	monthCounts, yearMonths, summerCounts := map[string]int{}, map[int]int{}, map[int]int{}
	longHauls := 0

	for i := range played {
		played[i].PlayedCount = i + 1
		played[i].CreatedAtRank = rankAmong(ownedStamps, played[i].CreatedAt)
		played[i].IsOldestOwned = played[i].CreatedAtRank == 1
		played[i].DropHistory = arcsUntil(arcs[played[i].ID], played[i].At)
		for ai < len(additions) && !additions[ai].After(played[i].At) {
			yearAdditions[additions[ai].Year()]++
			ai++
		}
		year := played[i].At.Year()
		yearFinishes[year]++
		played[i].YearFinishes = yearFinishes[year]
		played[i].YearAdditions = yearAdditions[year]
		played[i].UnplayedCount, played[i].PeakUnplayedCount = timeline.stateAt(played[i].At)

		month := monthKeyOf(played[i].At)
		if monthCounts[month] == 0 {
			yearMonths[year]++
		}
		monthCounts[month]++
		if achievements.IsSummer(int(played[i].At.Month())) {
			summerCounts[year]++
		}
		if played[i].TimeToBeatMain != nil && *played[i].TimeToBeatMain >= achievements.LongHaulSeconds {
			longHauls++
		}
		played[i].MonthFinishes = monthCounts[month]
		played[i].FinishStreak = finishMonthStreak(monthCounts, played[i].At)
		played[i].YearMonthsFinished = yearMonths[year]
		played[i].SummerFinishes = summerCounts[year]
		played[i].LongHaulFinishes = longHauls
		played[i].FinishYear, played[i].FinishMonth = year, int(played[i].At.Month())

		ev := achievements.Event{Kind: achievements.EventFinished, Entry: played[i]}
		for _, def := range achievements.Catalogue {
			if def.Predicate == nil || !def.Predicate(ev) {
				continue
			}
			if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, &played[i].ID); err != nil {
				return err
			}
		}
	}

	// Resume events replay at their historical moments, each carrying the
	// arc it closed. Only the resume-gated predicates can fire on them.
	for _, r := range resumes {
		ev := achievements.Event{Kind: achievements.EventResumed, Entry: achievements.Entry{
			ID: r.entryID, Status: r.status, At: r.at,
			DropHistory: []achievements.DropCycle{{DroppedAt: r.droppedAt, ResumedAt: &r.at}},
		}}
		for _, def := range achievements.Catalogue {
			if def.Predicate == nil || !def.Predicate(ev) {
				continue
			}
			if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, &r.entryID); err != nil {
				return err
			}
		}
	}

	// Dropped games feed the drop predicates, replayed in drop order so
	// the count ladder attaches to the game that crossed the line.
	rows, err = tx.QueryContext(ctx, `
		SELECT e.id, e.created_at, COALESCE(e.finished_at, e.created_at),
		       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0),
		       g.time_to_beat_main
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.status = 'dropped'
		ORDER BY COALESCE(e.finished_at, e.created_at) ASC, e.id`, userID)
	if err != nil {
		return err
	}
	dropped := []achievements.Entry{}
	for rows.Next() {
		var e achievements.Entry
		var finishedAt string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &finishedAt, &e.LoggedMinutes,
			&e.TimeToBeatMain); err != nil {
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

	for i := range dropped {
		dropped[i].DroppedCount = i + 1
		dropped[i].UnplayedCount, dropped[i].PeakUnplayedCount = timeline.stateAt(dropped[i].At)
		ev := achievements.Event{Kind: achievements.EventDropped, Entry: dropped[i]}
		for _, def := range achievements.Catalogue {
			if def.Predicate == nil || !def.Predicate(ev) {
				continue
			}
			if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, &dropped[i].ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// resumeReplay is one historical dropped → playing|backlog transition to
// re-fire the resume predicates at.
type resumeReplay struct {
	entryID   string
	status    string
	at        time.Time
	droppedAt time.Time
}

// loadUserDropArcsTx reads every entry's status history for the user and
// returns the drop arcs per entry (for the finish predicates) plus the
// resume transitions to replay (for the resume predicates). Real history
// rows only — a pre-feature drop that is still dropped has no arc, and one
// that was resumed before the feature left no resume row.
func loadUserDropArcsTx(ctx context.Context, tx *sql.Tx, userID string) (map[string][]achievements.DropCycle, []resumeReplay, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT h.entry_id, h.from_status, h.to_status, h.changed_at
		FROM entry_status_history h
		WHERE h.user_id = ?
		ORDER BY h.entry_id, h.changed_at, h.rowid`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	arcs := map[string][]achievements.DropCycle{}
	resumes := []resumeReplay{}
	for rows.Next() {
		var entryID, from, to, raw string
		if err := rows.Scan(&entryID, &from, &to, &raw); err != nil {
			return nil, nil, err
		}
		at, ok := parseDBTime(raw)
		if !ok {
			continue
		}
		switch {
		case to == models.StatusDropped:
			arcs[entryID] = append(arcs[entryID], achievements.DropCycle{DroppedAt: at})
		case from == models.StatusDropped &&
			(to == models.StatusPlaying || to == models.StatusBacklog):
			cycles := arcs[entryID]
			for i := len(cycles) - 1; i >= 0; i-- {
				if cycles[i].ResumedAt == nil {
					resumed := at
					cycles[i].ResumedAt = &resumed
					resumes = append(resumes, resumeReplay{
						entryID: entryID, status: to, at: at, droppedAt: cycles[i].DroppedAt,
					})
					break
				}
			}
		}
	}
	return arcs, resumes, rows.Err()
}

// arcsUntil filters the arcs down to drops that had already happened at at —
// a later drop-and-return must not count against an earlier finish.
func arcsUntil(cycles []achievements.DropCycle, at time.Time) []achievements.DropCycle {
	if len(cycles) == 0 {
		return nil
	}
	out := make([]achievements.DropCycle, 0, len(cycles))
	for _, cycle := range cycles {
		if !cycle.DroppedAt.After(at) {
			out = append(out, cycle)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// evaluateTimeWindowAchievementsTx runs the catalogue's lazy time predicates
// — the achievements with no mutation event to hook ("30 days without adding
// a game") — against the user's wall-clock aggregates. Called on gallery
// loads, where the historical backfill already runs; idempotent like
// everything else. defs is a parameter so the mechanism is testable without
// touching the real catalogue.
func (s *Store) evaluateTimeWindowAchievementsTx(ctx context.Context, tx *sql.Tx, userID string, defs []achievements.Definition) error {
	lazy := defs[:0:0]
	for _, def := range defs {
		if def.TimePredicate != nil {
			lazy = append(lazy, def)
		}
	}
	if len(lazy) == 0 {
		return nil
	}

	var raw sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM library_entries WHERE user_id = ? AND status <> 'wishlist'`,
		userID).Scan(&raw); err != nil {
		return err
	}
	if !raw.Valid {
		// No non-wishlist entries: there is no "last added" to measure a
		// window from.
		return nil
	}
	lastAcquired, ok := parseDBTime(raw.String)
	if !ok {
		return nil
	}
	snap := achievements.TimeSnapshot{Now: time.Now(), LastAcquiredAt: lastAcquired}
	for _, def := range lazy {
		if !def.TimePredicate(snap) {
			continue
		}
		if _, err := insertUnlockTx(ctx, tx, userID, def.Achievement.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

// Achievements returns the full catalogue merged with the user's unlock state,
// running the historical backfill first so the gallery is complete on first
// open, then the lazy time-window check for the achievements no mutation can
// unlock. Locked entries carry no date and no game; locked hidden entries
// carry no identity either.
func (s *Store) Achievements(ctx context.Context, userID string) ([]models.AchievementStatus, error) {
	if err := s.BackfillAchievements(ctx, userID); err != nil {
		return nil, err
	}
	if achievements.HasTimePredicates() {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		if err := s.evaluateTimeWindowAchievementsTx(ctx, tx, userID, achievements.Catalogue); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
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
		u, unlocked := byAchievement[def.Achievement.ID]
		locked := !unlocked
		status := models.AchievementStatus{Achievement: achievements.Present(def.Achievement, locked)}
		if unlocked {
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
