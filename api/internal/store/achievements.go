package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/metadata"
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

// ErrNotAnEgg is returned when UnlockEgg is asked to unlock an id that is
// not on the egg whitelist.
var ErrNotAnEgg = errors.New("achievement is not an easter egg")

// UnlockEgg records an easter-egg unlock for the user: the only path that
// can tip an Egg achievement, since predicates never fire for them. The
// insert is the same idempotent INSERT OR IGNORE the event evaluation uses,
// so a second call reports the existing unlock instead of duplicating it.
// The returned status carries the revealed identity — the reveal is the
// toast payload — and newly reports whether this call is the one that
// unlocked it.
func (s *Store) UnlockEgg(ctx context.Context, userID, achievementID string) (models.AchievementStatus, bool, error) {
	if !achievements.IsEgg(achievementID) {
		return models.AchievementStatus{}, false, ErrNotAnEgg
	}
	def := achievements.ByID(achievementID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AchievementStatus{}, false, err
	}
	defer tx.Rollback()

	at, err := insertUnlockTx(ctx, tx, userID, achievementID, nil)
	if err != nil {
		return models.AchievementStatus{}, false, err
	}
	newly := at != nil
	if !newly {
		// Already had it: report the original unlock moment.
		at = new(time.Time)
		if err := tx.QueryRowContext(ctx,
			`SELECT unlocked_at FROM achievement_unlocks WHERE user_id = ? AND achievement_id = ?`,
			userID, achievementID).Scan(at); err != nil {
			return models.AchievementStatus{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return models.AchievementStatus{}, false, err
	}
	return models.AchievementStatus{Achievement: *def, UnlockedAt: at}, newly, nil
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
		// Eggs never unlock from events — only the egg endpoint can tip
		// them — and entries without a predicate can never fire.
		if def.Achievement.Egg || def.Predicate == nil ||
			!def.Predicate(achievements.Event{Kind: kind, Entry: e}) {
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
// queries are fine — libraries are hundreds of rows, not millions. Every
// population is game-scoped by hand: the catalogue speaks games (IGDB,
// platforms, series), and book entries must not move these counts once they
// exist.
func snapshotAggregatesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) error {
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND media_type = 'game' AND status = 'played'`,
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
			 WHERE d.user_id = ? AND d.media_type = 'game' AND h.to_status = 'dropped')
			+
			(SELECT COUNT(*) FROM library_entries d
			 WHERE d.user_id = ? AND d.media_type = 'game' AND d.status = 'dropped'
			   AND NOT EXISTS (SELECT 1 FROM entry_status_history h
			                   WHERE h.entry_id = d.id AND h.to_status = 'dropped'))`,
		userID, userID).Scan(&e.DroppedCount); err != nil {
		return err
	}

	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_entries WHERE user_id = ? AND media_type = 'game' AND status IN ('backlog','playing')`,
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
		WHERE user_id = ? AND media_type = 'game' AND status = 'played'
		  AND finished_at IS NOT NULL AND strftime('%Y', finished_at) = ?`,
		userID, year).Scan(&e.YearFinishes); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM library_entries
		WHERE user_id = ? AND media_type = 'game' AND status <> 'wishlist'
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
		WHERE user_id = ? AND media_type = 'game' AND status = 'played' AND finished_at IS NOT NULL`,
		monthKeyOf(e.At), year, year, userID).
		Scan(&e.MonthFinishes, &e.YearMonthsFinished, &e.SummerFinishes); err != nil {
		return err
	}

	// The consecutive-month streak walks back from At's month over the
	// months that saw a finish.
	finishMonths := map[string]int{}
	mrows, err := tx.QueryContext(ctx, `
		SELECT strftime('%Y-%m', finished_at), COUNT(*) FROM library_entries
		WHERE user_id = ? AND media_type = 'game' AND status = 'played' AND finished_at IS NOT NULL
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
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'played' AND g.time_to_beat_main >= ?`,
		userID, achievements.LongHaulSeconds).Scan(&e.LongHaulFinishes); err != nil {
		return err
	}

	// Rank among owned, finishable entries — same population as IsOldestOwned.
	// Timestamps are TEXT, so the comparison is lexicographic = chronological.
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) + 1 FROM library_entries
		WHERE user_id = ? AND media_type = 'game' AND status NOT IN ('wishlist','ignored')
		  AND created_at < (SELECT created_at FROM library_entries WHERE id = ?)`,
		userID, e.ID).Scan(&e.CreatedAtRank); err != nil {
		return err
	}
	e.IsOldestOwned = e.CreatedAtRank == 1

	rows, err := tx.QueryContext(ctx, `
		SELECT sg.series_id FROM series_games sg
		WHERE sg.game_id = (SELECT game_id FROM library_entries WHERE id = ?)
		  AND sg.kind = 'game'
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

	// The series standings, against the same owned bar the series index
	// uses: non-wishlist, non-ignored, kind 'game'. The finish being
	// evaluated is already stamped in-tx, so it counts as played.
	if len(e.SeriesIDs) > 0 {
		placeholders := strings.Repeat(",?", len(e.SeriesIDs))[1:]
		args := make([]any, 0, len(e.SeriesIDs)+2)
		args = append(args, year, userID)
		for _, id := range e.SeriesIDs {
			args = append(args, id)
		}
		srows, err := tx.QueryContext(ctx, `
			SELECT sg.series_id, COUNT(*),
			       COALESCE(SUM(e.status = 'played'), 0),
			       COALESCE(SUM(e.status IN ('backlog','playing')), 0),
			       COALESCE(SUM(e.status = 'played' AND strftime('%Y', e.finished_at) = ?), 0)
			FROM series_games sg
			JOIN library_entries e ON e.game_id = sg.game_id
			WHERE e.user_id = ? AND sg.kind = 'game'
			  AND sg.series_id IN (`+placeholders+`)
			  AND e.status NOT IN ('wishlist','ignored')
			GROUP BY sg.series_id`, args...)
		if err != nil {
			return err
		}
		e.SeriesStandings = map[string]achievements.SeriesStanding{}
		for srows.Next() {
			var id string
			var s achievements.SeriesStanding
			if err := srows.Scan(&id, &s.Owned, &s.Played, &s.Unplayed, &s.YearPlayed); err != nil {
				srows.Close()
				return err
			}
			e.SeriesStandings[id] = s
		}
		srows.Close()
		if err := srows.Err(); err != nil {
			return err
		}

		// Back to Back: the previous finish — by (stamp, id), the same
		// order the backfill replays in — sharing a series with this one.
		prevShares, err := prevFinishSharesSeriesTx(ctx, tx, userID, e)
		if err != nil {
			return err
		}
		e.PrevFinishSharesSeries = prevShares
	}

	// The platform the user chose and the game's original release date ride
	// the same snapshot so predicates read one struct.
	if err := tx.QueryRowContext(ctx, `
		SELECT e.platform_id, g.first_release_date
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.id = ?`, e.ID).Scan(&e.PlatformID, &e.FirstReleaseDate); err != nil {
		return err
	}

	// The diversity aggregate: how many distinct genres this calendar
	// year's finishes cover, this finish included.
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT gg.genre_id)
		FROM library_entries e JOIN game_genres gg ON gg.game_id = e.game_id
		WHERE e.user_id = ? AND e.status = 'played'
		  AND e.finished_at IS NOT NULL AND strftime('%Y', e.finished_at) = ?`,
		userID, year).Scan(&e.YearGenres); err != nil {
		return err
	}

	// The platform-mastery aggregates over the user's finished platforms:
	// the distinct-platform count for World Tour, distinct generations for
	// Generation Gap, the handheld and Xbox generation runs, and the
	// Nintendo-console and Game Boy family counts. Platforms the catalog
	// does not classify keep a NULL generation, so they never count
	// toward a generation.
	if err := tx.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT e.platform_id),
			COUNT(DISTINCT CASE WHEN p.generation > 0 THEN p.generation END),
			COUNT(DISTINCT CASE WHEN p.handheld AND p.generation > 0 THEN p.generation END),
			COUNT(DISTINCT CASE WHEN p.family = ? AND p.generation > 0 THEN p.generation END),
			COUNT(DISTINCT CASE WHEN p.family = ? THEN e.platform_id END),
			COUNT(DISTINCT CASE WHEN p.family = ? THEN e.platform_id END)
		FROM library_entries e JOIN platforms p ON p.id = e.platform_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'played'`,
		metadata.FamilyXbox, metadata.FamilyNintendoConsole, metadata.FamilyGameBoy,
		userID).Scan(&e.DistinctPlatforms, &e.DistinctGenerations, &e.HandheldGenerations,
		&e.XboxGenerations, &e.NintendoConsoles, &e.GameBoySystems); err != nil {
		return err
	}

	// The curated hardware sets: how many of The Big N's consoles and the
	// pilgrim's stations carry a finish.
	bigN, err := countFinishedPlatforms(ctx, tx, userID, metadata.BigNPlatformIDs)
	if err != nil {
		return err
	}
	e.BigNConsoles = bigN
	pilgrim, err := countFinishedPlatforms(ctx, tx, userID, metadata.PilgrimPlatformIDs)
	if err != nil {
		return err
	}
	e.PilgrimConsoles = pilgrim

	// Retroactive's lookback: whether the entry's platform has seen no
	// logged session in the RetroactiveYears up to and including the
	// finish date — played_on is a date, so the whole finish day blocks.
	// Sessions only exist from Backhog usage, so a platform never touched
	// here reads as dormant on the first finish: honest, per the design.
	if e.PlatformID != nil {
		stamp := e.At.UTC().Format("2006-01-02 15:04:05")
		dormant := false
		if err := tx.QueryRowContext(ctx, `
			SELECT NOT EXISTS (
				SELECT 1 FROM play_sessions ps
				JOIN library_entries le ON le.id = ps.entry_id
				WHERE ps.user_id = ? AND le.platform_id = ?
				  AND ps.played_on BETWEEN date(?, ?) AND date(?))`,
			userID, *e.PlatformID, stamp,
			fmt.Sprintf("-%d years", achievements.RetroactiveYears), stamp).Scan(&dormant); err != nil {
			return err
		}
		e.PlatformDormant = &dormant
	}

	e.FinishYear, e.FinishMonth = e.At.Year(), int(e.At.Month())
	return nil
}

// rowQuerier is the slice of the database handle the shared helpers need:
// satisfied by both *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// countFinishedPlatforms counts how many of the curated platforms carry
// at least one of the user's finishes.
func countFinishedPlatforms(ctx context.Context, q rowQuerier, userID string, ids []int64) (int, error) {
	placeholders := strings.Repeat(",?", len(ids))[1:]
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	var n int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT platform_id) FROM library_entries
		WHERE user_id = ? AND media_type = 'game' AND status = 'played'
		  AND platform_id IN (`+placeholders+`)`, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// countFinishedFamilyPlatforms counts how many distinct platforms of one
// classification family carry a finish — the family-wide sets, whose
// membership is the catalog itself.
func countFinishedFamilyPlatforms(ctx context.Context, q rowQuerier, userID, family string) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT e.platform_id)
		FROM library_entries e JOIN platforms p ON p.id = e.platform_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'played' AND p.family = ?`,
		userID, family).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// prevFinishSharesSeriesTx reports whether the user's previous finish —
// the closest one strictly before this entry by (finish stamp, id), the
// same order the backfill replays in — was a game from one of the same
// kind-'game' series. Timestamps are compared as datetime() values so the
// stored TEXT format never matters.
func prevFinishSharesSeriesTx(ctx context.Context, tx *sql.Tx, userID string, e *achievements.Entry) (bool, error) {
	stamp := e.At.UTC().Format("2006-01-02 15:04:05")
	var prevGame sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT game_id FROM library_entries
		WHERE user_id = ? AND media_type = 'game' AND status = 'played' AND id <> ?
		  AND (datetime(COALESCE(finished_at, created_at)) < datetime(?)
		       OR (datetime(COALESCE(finished_at, created_at)) = datetime(?) AND id < ?))
		ORDER BY datetime(COALESCE(finished_at, created_at)) DESC, id DESC
		LIMIT 1`, userID, e.ID, stamp, stamp, e.ID).Scan(&prevGame)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !prevGame.Valid {
		return false, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT series_id FROM series_games
		WHERE game_id = ? AND kind = 'game'`, prevGame.Int64)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	mine := map[string]bool{}
	for _, id := range e.SeriesIDs {
		mine[id] = true
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, err
		}
		if mine[id] {
			return true, nil
		}
	}
	return false, rows.Err()
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
		WHERE user_id = ? AND media_type = 'game' AND status NOT IN ('wishlist','ignored')`, userID)
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
		WHERE user_id = ? AND media_type = 'game' AND status <> 'wishlist'`, userID)
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
		WHERE user_id = ? AND media_type = 'game' AND status NOT IN ('wishlist','ignored')`, userID)
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

// loadSeriesGamesTx maps each cached game to its kind-'game' series
// memberships, sorted by series id — DLC and expansion rows never feed
// the series predicates.
func loadSeriesGamesTx(ctx context.Context, tx *sql.Tx) (map[int64][]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT game_id, series_id FROM series_games
		WHERE kind = 'game' ORDER BY game_id, series_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]string{}
	for rows.Next() {
		var gameID int64
		var seriesID string
		if err := rows.Scan(&gameID, &seriesID); err != nil {
			return nil, err
		}
		out[gameID] = append(out[gameID], seriesID)
	}
	return out, rows.Err()
}

// seriesMemberState is one owned member of a series, reduced to what the
// historical standings replay needs: when it was acquired, whether it is
// finished or dropped today, and when that closing status was stamped.
type seriesMemberState struct {
	createdAt time.Time
	status    string
	closedAt  *time.Time
}

// loadSeriesMembersTx reads every owned (non-wishlist, non-ignored)
// member of every series the user holds games in, keyed by series id.
// The closed stamp is finished_at — a finish or a drop, whichever closed
// the book — with the backfill's created_at fallback for pre-feature
// rows; backlog and playing members never close.
func loadSeriesMembersTx(ctx context.Context, tx *sql.Tx, userID string) (map[string][]seriesMemberState, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT sg.series_id, e.status, e.created_at, COALESCE(e.finished_at, e.created_at)
		FROM series_games sg
		JOIN library_entries e ON e.game_id = sg.game_id
		WHERE e.user_id = ? AND sg.kind = 'game'
		  AND e.status NOT IN ('wishlist','ignored')
		ORDER BY sg.series_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]seriesMemberState{}
	for rows.Next() {
		var seriesID, status, createdRaw, closedRaw string
		if err := rows.Scan(&seriesID, &status, &createdRaw, &closedRaw); err != nil {
			return nil, err
		}
		created, ok := parseDBTime(createdRaw)
		if !ok {
			continue
		}
		m := seriesMemberState{createdAt: created, status: status}
		switch status {
		case models.StatusPlayed, models.StatusDropped:
			if closed, ok := parseDBTime(closedRaw); ok {
				m.closedAt = &closed
			}
		}
		out[seriesID] = append(out[seriesID], m)
	}
	return out, rows.Err()
}

// standingsAt replays one series' members to the historical moment at,
// judged from what the library knows today — the same approximation the
// unplayed timeline makes. A member owned by then counts as played once
// its finish stamp passes, dropped once its drop stamp does, and
// unplayed until either.
func standingsAt(members []seriesMemberState, at time.Time) achievements.SeriesStanding {
	var s achievements.SeriesStanding
	for _, m := range members {
		if m.createdAt.After(at) {
			continue
		}
		s.Owned++
		if m.closedAt == nil || m.closedAt.After(at) {
			s.Unplayed++
			continue
		}
		if m.status == models.StatusPlayed {
			s.Played++
			if m.closedAt.Year() == at.Year() {
				s.YearPlayed++
			}
		}
	}
	return s
}

// seriesSet turns a series id list into the set Back to Back carries
// between replayed finishes.
func seriesSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// platformClass is the slice of a platforms row the backfill replay needs.
type platformClass struct {
	family     string
	generation sql.NullInt64
	handheld   bool
}

// loadPlatformMetaTx reads every platform row's classification. Rows the
// catalog does not know ride along unclassified and degrade exactly the
// way the live snapshot's SQL degrades them.
func loadPlatformMetaTx(ctx context.Context, tx *sql.Tx) (map[int64]platformClass, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, family, generation, handheld FROM platforms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]platformClass{}
	for rows.Next() {
		var id int64
		var cls platformClass
		if err := rows.Scan(&id, &cls.family, &cls.generation, &cls.handheld); err != nil {
			return nil, err
		}
		out[id] = cls
	}
	return out, rows.Err()
}

// loadGameGenresTx maps each cached game to its genre ids, sorted, for the
// running per-year genre set behind Sampler.
func loadGameGenresTx(ctx context.Context, tx *sql.Tx) (map[int64][]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT game_id, genre_id FROM game_genres ORDER BY game_id, genre_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]int64{}
	for rows.Next() {
		var gameID, genreID int64
		if err := rows.Scan(&gameID, &genreID); err != nil {
			return nil, err
		}
		out[gameID] = append(out[gameID], genreID)
	}
	return out, rows.Err()
}

// platformSession is one logged play session reduced to what Retroactive's
// replay needs: the platform of the entry it was logged against, and the
// day it happened.
type platformSession struct {
	platformID int64
	playedOn   time.Time
}

// loadUserPlatformSessionsTx reads the user's sessions joined to their
// entry's platform. Sessions against entries with no platform set cannot
// speak to any platform's dormancy and are skipped.
func loadUserPlatformSessionsTx(ctx context.Context, tx *sql.Tx, userID string) ([]platformSession, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT le.platform_id, ps.played_on
		FROM play_sessions ps JOIN library_entries le ON le.id = ps.entry_id
		WHERE ps.user_id = ? AND le.platform_id IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []platformSession{}
	for rows.Next() {
		var sess platformSession
		var raw string
		if err := rows.Scan(&sess.platformID, &raw); err != nil {
			return nil, err
		}
		if at, ok := parseDBTime(raw); ok {
			sess.playedOn = at
			out = append(out, sess)
		}
	}
	return out, rows.Err()
}

// platformIDSet turns a curated platform id list into the set lookups the
// backfill replay does per finish.
func platformIDSet(ids []int64) map[int64]bool {
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// sharesAnySeries reports whether any id is in prev.
func sharesAnySeries(prev map[string]bool, ids []string) bool {
	for _, id := range ids {
		if prev[id] {
			return true
		}
	}
	return false
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
		SELECT e.id, e.game_id, e.created_at, COALESCE(e.finished_at, e.created_at),
		       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0),
		       g.time_to_beat_main, g.time_to_beat_complete, g.first_release_date,
		       e.platform_id
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'played'
		ORDER BY COALESCE(e.finished_at, e.created_at) ASC, e.id`, userID)
	if err != nil {
		return err
	}
	played := []achievements.Entry{}
	gameIDs := []int64{}
	platformIDs := []sql.NullInt64{}
	for rows.Next() {
		var e achievements.Entry
		var gameID int64
		var platformID sql.NullInt64
		var finishedAt string
		if err := rows.Scan(&e.ID, &gameID, &e.CreatedAt, &finishedAt, &e.LoggedMinutes,
			&e.TimeToBeatMain, &e.TimeToBeatComplete, &e.FirstReleaseDate, &platformID); err != nil {
			rows.Close()
			return err
		}
		e.Status = models.StatusPlayed
		if platformID.Valid {
			pid := platformID.Int64
			e.PlatformID = &pid
		}
		if at, ok := parseDBTime(finishedAt); ok {
			e.At = at
		} else {
			e.At = e.CreatedAt
		}
		played = append(played, e)
		gameIDs = append(gameIDs, gameID)
		platformIDs = append(platformIDs, platformID)
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
	// The series replay: which series each cached game belongs to as a
	// full game, and every series' owned members reduced to the stamps
	// the standings need — so each historical finish judges the series
	// at its own moment, not against today's state.
	seriesOf, err := loadSeriesGamesTx(ctx, tx)
	if err != nil {
		return err
	}
	seriesMembers, err := loadSeriesMembersTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	// The platform replay: every platform row's classification, the
	// genres each cached game carries, and the user's sessions joined to
	// the platform they were logged against — so the diversity and
	// platform-mastery aggregates grow exactly as they did live, and
	// Retroactive's lookback replays against the historical session dates.
	platformMeta, err := loadPlatformMetaTx(ctx, tx)
	if err != nil {
		return err
	}
	genresOf, err := loadGameGenresTx(ctx, tx)
	if err != nil {
		return err
	}
	userSessions, err := loadUserPlatformSessionsTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	bigNSet := platformIDSet(metadata.BigNPlatformIDs)
	pilgrimSet := platformIDSet(metadata.PilgrimPlatformIDs)
	prevSeries := map[string]bool{}
	yearFinishes, yearAdditions := map[int]int{}, map[int]int{}
	ai := 0
	// The calendar aggregates grow as finishes replay in order, exactly as
	// they did live: month buckets, the distinct months per year behind a
	// perfect season, the summer window, and the 50h+ count.
	monthCounts, yearMonths, summerCounts := map[string]int{}, map[int]int{}, map[int]int{}
	longHauls := 0
	// The diversity and platform-mastery aggregates grow the same way:
	// the year's genre set, and the distinct platforms, generations, and
	// family members the finishes have covered so far.
	yearGenres := map[int]map[int64]bool{}
	platformsSeen, nintendoSeen := map[int64]bool{}, map[int64]bool{}
	gameBoySeen, bigNSeen, pilgrimSeen := map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
	generationsSeen, handheldGens, xboxGens := map[int]bool{}, map[int]bool{}, map[int]bool{}

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

		// Series standings at the historical moment, and the previous
		// finish's series for Back to Back.
		seriesIDs := seriesOf[gameIDs[i]]
		played[i].SeriesIDs = seriesIDs
		if len(seriesIDs) > 0 {
			standings := make(map[string]achievements.SeriesStanding, len(seriesIDs))
			for _, sid := range seriesIDs {
				standings[sid] = standingsAt(seriesMembers[sid], played[i].At)
			}
			played[i].SeriesStandings = standings
		}
		played[i].PrevFinishSharesSeries = sharesAnySeries(prevSeries, seriesIDs)
		prevSeries = seriesSet(seriesIDs)

		// The diversity aggregate: this year's genre set, this finish's
		// genres folded in.
		genreSet := yearGenres[year]
		if genreSet == nil {
			genreSet = map[int64]bool{}
		}
		for _, gid := range genresOf[gameIDs[i]] {
			genreSet[gid] = true
		}
		yearGenres[year] = genreSet
		played[i].YearGenres = len(genreSet)

		// The platform-mastery aggregates, and Retroactive's lookback
		// against the historical session dates. Platform assignments are
		// judged from what the library knows today — the same
		// approximation the series standings make.
		if platformIDs[i].Valid {
			pid := platformIDs[i].Int64
			platformsSeen[pid] = true
			cls := platformMeta[pid]
			gen := 0
			if cls.generation.Valid && cls.generation.Int64 > 0 {
				gen = int(cls.generation.Int64)
				generationsSeen[gen] = true
				if cls.handheld {
					handheldGens[gen] = true
				}
			}
			switch cls.family {
			case metadata.FamilyNintendoConsole:
				nintendoSeen[pid] = true
			case metadata.FamilyGameBoy:
				gameBoySeen[pid] = true
			case metadata.FamilyXbox:
				if gen > 0 {
					xboxGens[gen] = true
				}
			}
			if bigNSet[pid] {
				bigNSeen[pid] = true
			}
			if pilgrimSet[pid] {
				pilgrimSeen[pid] = true
			}
			played[i].DistinctPlatforms = len(platformsSeen)
			played[i].DistinctGenerations = len(generationsSeen)
			played[i].HandheldGenerations = len(handheldGens)
			played[i].XboxGenerations = len(xboxGens)
			played[i].NintendoConsoles = len(nintendoSeen)
			played[i].GameBoySystems = len(gameBoySeen)
			played[i].BigNConsoles = len(bigNSeen)
			played[i].PilgrimConsoles = len(pilgrimSeen)

			finishDay := played[i].At.UTC()
			finishDay = time.Date(finishDay.Year(), finishDay.Month(), finishDay.Day(), 0, 0, 0, 0, time.UTC)
			start := finishDay.AddDate(-achievements.RetroactiveYears, 0, 0)
			dormant := true
			for _, sess := range userSessions {
				if sess.platformID == pid && !sess.playedOn.Before(start) && !sess.playedOn.After(finishDay) {
					dormant = false
					break
				}
			}
			played[i].PlatformDormant = &dormant
		}

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
			if def.Achievement.Egg || def.Predicate == nil || !def.Predicate(ev) {
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
			if def.Achievement.Egg || def.Predicate == nil || !def.Predicate(ev) {
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
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'dropped'
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
			if def.Achievement.Egg || def.Predicate == nil || !def.Predicate(ev) {
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
		`SELECT MAX(created_at) FROM library_entries WHERE user_id = ? AND media_type = 'game' AND status <> 'wishlist'`,
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

	// The curated platform sets carry their progress onto the locked
	// gallery cards ("3/5 consoles so far") — served server-side, the
	// same pattern the tonight picks' reasons use.
	progress, err := s.curatedSetProgress(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := make([]models.AchievementStatus, 0, len(achievements.Catalogue))
	for _, def := range achievements.Catalogue {
		u, unlocked := byAchievement[def.Achievement.ID]
		locked := !unlocked
		status := models.AchievementStatus{Achievement: achievements.Present(def.Achievement, locked)}
		if locked && !def.Achievement.Hidden {
			// Progress strings speak to the visible ladder achievements
			// only — on a hidden card the count would give the game away.
			if p, ok := progress[def.Achievement.ID]; ok && p.have > 0 && p.have < p.want {
				status.Description = fmt.Sprintf("%s %d/%d %s so far.",
					status.Description, p.have, p.want, p.unit)
			}
		}
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

// curatedProgress is the user's standing in one curated platform set:
// how many members carry a finish, out of how many, and the noun the
// locked gallery card reads ("4/7 consoles so far").
type curatedProgress struct {
	have, want int
	unit       string
}

// curatedSetProgress measures the user's standing in each curated
// platform set, for the locked gallery cards' progress strings.
func (s *Store) curatedSetProgress(ctx context.Context, userID string) (map[string]curatedProgress, error) {
	bigN, err := countFinishedPlatforms(ctx, s.db, userID, metadata.BigNPlatformIDs)
	if err != nil {
		return nil, err
	}
	pilgrim, err := countFinishedPlatforms(ctx, s.db, userID, metadata.PilgrimPlatformIDs)
	if err != nil {
		return nil, err
	}
	gameBoy, err := countFinishedFamilyPlatforms(ctx, s.db, userID, metadata.FamilyGameBoy)
	if err != nil {
		return nil, err
	}
	return map[string]curatedProgress{
		"the_big_n":           {have: bigN, want: len(metadata.BigNPlatformIDs), unit: "consoles"},
		"playstation_pilgrim": {have: pilgrim, want: len(metadata.PilgrimPlatformIDs), unit: "consoles"},
		"game_boy_generation": {have: gameBoy, want: metadata.FamilySize(metadata.FamilyGameBoy), unit: "systems"},
	}, nil
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
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status = 'played'
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
