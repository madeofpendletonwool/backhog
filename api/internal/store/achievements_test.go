package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newAchievementsStore opens a migrated SQLite database in a temp dir seeded
// with u1's mixed history — four finished games across the ownership ages, one
// drop, one backlog straggler — and u2 with nothing, for scoping checks.
// Dates are anchored to calendar years (relative to whenever the suite runs)
// because the year-scoped predicates compare calendar-year buckets.
func newAchievementsStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "achievements.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(database)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	exec(`INSERT INTO users (id, email, username, password_hash) VALUES
		('u1', 'u1@example.com', 'u1', 'x'),
		('u2', 'u2@example.com', 'u2', 'x')`)

	year := time.Now().UTC().Year()

	// games: (id, hours to beat main, hours to beat complete, original
	// release unix seconds; 0 = unknown). Game 1 is a pre-2000 release, so
	// a1's finish earns the era achievements; game 3 is recent; game 4
	// lands exactly on the time-capsule window for a4's finish.
	for _, g := range []struct {
		id             int64
		main, complete int64
		release        int64
	}{
		{1, 10, 20, time.Date(1998, 6, 15, 0, 0, 0, 0, time.UTC).Unix()},
		{2, 4, 8, 0},
		{3, 60, 120, time.Date(year-2, 1, 1, 0, 0, 0, 0, time.UTC).Unix()},
		{4, 8, 16, time.Date(year-1, 7, 5, 12, 0, 0, 0, time.UTC).Add(-10 * 365 * 24 * time.Hour).Unix()},
		{5, 12, 24, 0},
		{6, 10, 20, 0},
	} {
		var release any
		if g.release != 0 {
			release = g.release
		}
		exec(`INSERT INTO games (id, name, time_to_beat_main, time_to_beat_complete, first_release_date)
			VALUES (?, ?, ?, ?, ?)`,
			g.id, "Game "+string(rune('A'+g.id-1)), g.main*3600, g.complete*3600, release)
	}

	// ymd builds an explicit UTC calendar stamp: the year comparisons
	// (Backlog Negative) need dates that sit in known buckets no matter
	// when the suite runs.
	ymd := func(offsetYears int, monthDay string) string {
		return time.Date(year+offsetYears, 1, 1, 12, 0, 0, 0, time.UTC).
			Format("2006") + "-" + monthDay + " 12:00:00"
	}

	addEntry := func(id string, userID string, gameID int64, status string, created string, finished string, sessionMinutes []int) {
		t.Helper()
		var fin any
		if finished != "" {
			fin = finished
		}
		exec(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, userID, gameID, status, created, fin)
		for i, minutes := range sessionMinutes {
			exec(`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
				VALUES (?, ?, ?, ?, ?)`,
				"ps-"+id+string(rune('a'+i)), userID, id, created[:10], minutes)
		}
	}

	// u1's history: an 8-year-old dig finished first, three more finishes
	// spaced through last year, a drop after ~21 months, and an ancient
	// backlog straggler. Two of the entries were added last year, so the
	// third finish of the year is where finishes overtake additions.
	addEntry("a1", "u1", 1, models.StatusPlayed, ymd(-8, "03-01"), ymd(-1, "06-10"), []int{120, 60})
	addEntry("a2", "u1", 2, models.StatusPlayed, ymd(-1, "01-05"), ymd(-1, "06-20"), []int{300})
	addEntry("a3", "u1", 3, models.StatusPlayed, ymd(-2, "09-15"), ymd(-1, "06-25"), []int{1440, 1440, 1440, 1440, 60})
	addEntry("a4", "u1", 4, models.StatusPlayed, ymd(-1, "02-01"), ymd(-1, "07-05"), []int{960})
	addEntry("a5", "u1", 5, models.StatusDropped, ymd(-3, "05-01"), ymd(-1, "02-20"), nil)
	addEntry("a6", "u1", 6, models.StatusBacklog, ymd(-6, "01-10"), "", nil)

	return s
}

// unlockedIDs maps achievement id → triggering entry id from a status list.
func unlockedIDs(statuses []models.AchievementStatus) map[string]string {
	ids := map[string]string{}
	for _, st := range statuses {
		if st.UnlockedAt == nil {
			continue
		}
		if st.Entry != nil {
			ids[st.ID] = st.Entry.ID
		} else {
			ids[st.ID] = ""
		}
	}
	return ids
}

func TestAchievementsBackfill(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	achievements, err := s.Achievements(ctx, "u1")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}

	want := map[string]string{
		// a1 is the first finish, the oldest-owned game, an 8-year dig
		// with 3h logged, and a pre-2000 release — nine achievements off
		// one game.
		"first_blood":       "a1",
		"archaeologist":     "a1",
		"dusty_relic":       "a1",
		"lost_civilization": "a1",
		"fossil_record":     "a1",
		"time_capsule":      "a1",
		"old_hardware":      "a1",
		"speedrun":          "a1",
		"the_ancient_one":   "a1",
		// a3 is the 60h main-estimate game.
		"long_haul": "a3",
		// a4 logged exactly its 16h completion estimate.
		"completionist": "a4",
		// a5 was dropped after ~21 months of ownership, with nothing
		// logged against a 12h main estimate — an honest abandonment and
		// textbook loss-cutting.
		"abandonment_issues": "a5",
		"cut_your_losses":    "a5",
		// The third finish of last year overtakes that year's two
		// additions.
		"backlog_negative": "a3",
		// Four finished games: cleanup_crew needs five.
	}
	got := unlockedIDs(achievements)
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}

	// Idempotent: re-running the gallery (and its backfill) must not add,
	// remove, or re-stamp anything.
	var count, before int
	database := s.DB()
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u1'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Achievements(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != before {
		t.Errorf("second backfill changed unlock count %d → %d", before, count)
	}

	// An empty user stays fully locked.
	u2, err := s.Achievements(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range u2 {
		if st.UnlockedAt != nil {
			t.Errorf("u2 has %s unlocked with an empty library", st.ID)
		}
	}
}

func TestUpdateEntryUnlocksAchievements(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	// Finishing the fifth game crosses the cleanup_crew line; a6 is a ~6
	// year dig with nothing logged, so the age ladder's lower rungs, the
	// fossil record (a6 is the second-oldest of six), and speedrun ride
	// along, and this year's finishes (one) already outnumber this year's
	// additions (none).
	_, unlocks, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got := unlockedIDs(unlocks)
	want := map[string]string{
		"first_blood":      "a6",
		"cleanup_crew":     "a6",
		"speedrun":         "a6",
		"dusty_relic":      "a6",
		"archaeologist":    "a6",
		"fossil_record":    "a6",
		"backlog_negative": "a6",
	}
	if len(got) != len(want) {
		t.Fatalf("live unlocks = %v, want %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s = %q, want %q", id, got[id], entryID)
		}
	}

	// Un-finishing and re-finishing must not unlock anything twice.
	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusBacklog)}); err != nil {
		t.Fatal(err)
	}
	_, unlocks, err = s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatal(err)
	}
	if len(unlockedIDs(unlocks)) != 0 {
		t.Errorf("re-finish unlocked %v, want nothing", unlockedIDs(unlocks))
	}
}

func TestUpdateEntryDroppedUnlocksAbandonment(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	// a6 has been owned ~6 years — dropping it is an honest abandonment.
	_, unlocks, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusDropped)})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got := unlockedIDs(unlocks)
	if got["abandonment_issues"] != "a6" {
		t.Fatalf("unlocks = %v, want abandonment_issues on a6", got)
	}
}

func TestAddSessionUnlocksCompletionist(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	// A game already marked played at 15h of a 16h completion estimate: the
	// final hour gets logged afterwards, and only then does completionist
	// tip over.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO games (id, name, time_to_beat_main, time_to_beat_complete) VALUES (7, 'Late Log', 28800, 57600)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	if _, err := database.ExecContext(ctx, `
		INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
		VALUES ('a7', 'u1', 7, 'played', ?, ?)`,
		stamp(now.AddDate(0, 0, -200)), stamp(now.AddDate(0, 0, -15))); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
		VALUES ('ps-a7', 'u1', 'a7', ?, 900)`, stamp(now.AddDate(0, 0, -16))[:10]); err != nil {
		t.Fatal(err)
	}

	_, unlocks, err := s.AddSession(ctx, "u1", "a7", now.Format("2006-01-02"), 60, "credits roll")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	got := unlockedIDs(unlocks)
	if got["completionist"] != "a7" {
		t.Fatalf("unlocks = %v, want completionist on a7", got)
	}

	// A session on a backlog game only starts it — nothing to celebrate.
	_, unlocks, err = s.AddSession(ctx, "u1", "a6", now.Format("2006-01-02"), 30, "")
	if err != nil {
		t.Fatalf("AddSession backlog: %v", err)
	}
	if len(unlockedIDs(unlocks)) != 0 {
		t.Errorf("backlog session unlocked %v, want nothing", unlockedIDs(unlocks))
	}
}

func TestSeason(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	year := time.Now().Year()
	prev := year - 1
	ymd := func(y int, monthDay string) string {
		return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "-" + monthDay
	}

	database := s.DB()
	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// u2 carries the season fixture so u1's achievements fixture stays
	// untouched: two finishes this year (one a rescue), one last year, and a
	// backlog game that owes a series its ending.
	seed(`INSERT INTO games (id, name, time_to_beat_main) VALUES
		(11, 'Season A', 36000), (12, 'Season B', 36000),
		(13, 'Season C', 36000), (14, 'Season D', 36000)`)
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at) VALUES
		('s1', 'u2', 11, 'played', ?, ?),
		('s2', 'u2', 12, 'played', ?, ?),
		('s3', 'u2', 13, 'played', ?, ?),
		('s4', 'u2', 14, 'backlog', ?, NULL)`,
		ymd(year-6, "03-01"), ymd(year, "03-10"),
		ymd(year, "01-10"), ymd(year, "05-01"),
		ymd(year-1, "06-01"), ymd(prev, "12-15"),
		ymd(year-1, "01-01"))
	seed(`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes) VALUES
		('ss1', 'u2', 's1', ?, 120),
		('ss2', 'u2', 's3', ?, 90)`, ymd(year, "02-15"), ymd(prev, "11-01"))

	// Two series: one fully finished this year, one still owed.
	seed(`INSERT INTO series (id, name) VALUES ('sr1', 'Cleared Saga'), ('sr2', 'Open Saga')`)
	seed(`INSERT INTO series_games (series_id, game_id, kind) VALUES
		('sr1', 11, 'game'), ('sr1', 12, 'game'),
		('sr2', 13, 'game'), ('sr2', 14, 'game')`)

	season, err := s.Season(ctx, "u2", year)
	if err != nil {
		t.Fatalf("Season: %v", err)
	}
	if season.GamesCompleted != 2 {
		t.Errorf("GamesCompleted = %d, want 2", season.GamesCompleted)
	}
	if season.Rescues != 1 {
		t.Errorf("Rescues = %d, want 1", season.Rescues)
	}
	if season.HoursPlayed != 2 {
		t.Errorf("HoursPlayed = %v, want 2", season.HoursPlayed)
	}
	if season.FranchisesCleared != 1 {
		t.Errorf("FranchisesCleared = %d, want 1", season.FranchisesCleared)
	}

	// Last year sees its own completion and hours, no rescue, no franchise.
	prevSeason, err := s.Season(ctx, "u2", prev)
	if err != nil {
		t.Fatalf("Season prev: %v", err)
	}
	if prevSeason.GamesCompleted != 1 || prevSeason.HoursPlayed != 1.5 ||
		prevSeason.Rescues != 0 || prevSeason.FranchisesCleared != 0 {
		t.Errorf("prev season = %+v, want 1 completion / 1.5h / 0 rescues / 0 franchises", prevSeason)
	}
}

// volumeFixture is the shared scaffolding for the volume/reduction tests: a
// fresh user u3, a batch of game rows, and entries stamped with explicit
// calendar dates so year-scoped predicates sit in known buckets.
type volumeFixture struct {
	s  *Store
	tx func(query string, args ...any)
}

func newVolumeFixture(t *testing.T, gameCount int) *volumeFixture {
	t.Helper()
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()
	f := &volumeFixture{s: s}
	f.tx = func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	f.tx(`INSERT INTO users (id, email, username, password_hash) VALUES ('u3', 'u3@example.com', 'u3', 'x')`)
	for i := 0; i < gameCount; i++ {
		f.tx(`INSERT INTO games (id, name) VALUES (?, ?)`, 100+i, "Volume Game "+string(rune('A'+i)))
	}
	return f
}

// add inserts one library entry against game i of the fixture's batch;
// finished empty means never.
func (f *volumeFixture) add(id string, game int, status, created, finished string) {
	var fin any
	if finished != "" {
		fin = finished
	}
	f.tx(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, "u3", 100+game, status, created, fin)
}

// ymdStamp formats an explicit calendar stamp — offsetYears back from the
// current year plus month-day — at a fixed hour so ties never collide.
func ymdStamp(offsetYears int, monthDay string) string {
	year := time.Now().UTC().Year()
	return time.Date(year+offsetYears, 1, 1, 12, 0, 0, 0, time.UTC).
		Format("2006") + "-" + monthDay + " 12:00:00"
}

// TestBackfillVolumeLadderAttach pins the count ladder to the game that
// crossed the line, One Down's ten-unplayed boundary, the peak-reduction
// crossing, and the running year counts behind Backlog Negative — all
// through the gallery's completion-order replay.
func TestBackfillVolumeLadderAttach(t *testing.T) {
	f := newVolumeFixture(t, 12)
	ctx := context.Background()

	// Ten games bought two years ago, finished monthly through last year
	// (entry i in month i+1). One straggler is added five days before the
	// first finish, so the peak hits eleven and the year's finishes
	// overtake its additions only at the second finish.
	for i := 0; i < 10; i++ {
		f.add(fmt.Sprintf("c%d", i), i, models.StatusPlayed,
			ymdStamp(-2, fmt.Sprintf("01-%02d", i+1)),
			ymdStamp(-1, fmt.Sprintf("%02d-15", i+1)))
	}
	f.add("straggler", 10, models.StatusBacklog, ymdStamp(-1, "01-05"), "")

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	want := map[string]string{
		// The first finish is also the oldest owned game, unlogged, and
		// leaves exactly ten unplayed behind — oldest means the fossil
		// record opens too.
		"first_blood":     "c0",
		"speedrun":        "c0",
		"the_ancient_one": "c0",
		"fossil_record":   "c0",
		"one_down":        "c0",
		// Two finishes vs one same-year addition at the second finish.
		"backlog_negative": "c1",
		// The count ladder attaches to its crossing game.
		"cleanup_crew":    "c4",
		"spring_cleaning": "c9",
		// Peak eleven, one left after the tenth finish: reduction ten.
		"making_a_dent": "c9",
	}
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}

	// Idempotent: the gallery's backfill re-runs on every load.
	database := f.s.DB()
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u3'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Achievements(ctx, "u3"); err != nil {
		t.Fatal(err)
	}
	var again int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u3'`).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != count {
		t.Errorf("second backfill changed unlock count %d → %d", count, again)
	}
}

// TestBackfillPeakReductionAndCloset drives the peak sweep through mixed
// acquisition and finish times: the peak sits mid-history above both the
// starting and ending counts, and Empty the Closet needs both a zero count
// and a ten-plus peak.
func TestBackfillPeakReductionAndCloset(t *testing.T) {
	f := newVolumeFixture(t, 12)
	ctx := context.Background()

	// Five games three years ago; the first finishes that July (count 4,
	// peak 5). Six more arrive two years ago, lifting the peak to ten.
	// Last year the remaining ten finish monthly: the tenth empties the
	// library — reduction ten and a closed closet on the same game.
	f.add("p0", 0, models.StatusPlayed, ymdStamp(-3, "01-01"), ymdStamp(-3, "07-01"))
	for i := 1; i <= 4; i++ {
		f.add(fmt.Sprintf("p%d", i), i, models.StatusPlayed,
			ymdStamp(-3, fmt.Sprintf("01-%02d", i+1)), ymdStamp(-1, fmt.Sprintf("%02d-15", i)))
	}
	for i := 5; i <= 10; i++ {
		f.add(fmt.Sprintf("p%d", i), i, models.StatusPlayed,
			ymdStamp(-2, "01-15"), ymdStamp(-1, fmt.Sprintf("%02d-15", i)))
	}

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	want := map[string]string{
		// The Y-3 finish: first, unlogged, oldest, four left unplayed.
		"first_blood":     "p0",
		"speedrun":        "p0",
		"the_ancient_one": "p0",
		"fossil_record":   "p0",
		// No additions last year: the first finish of the year tips the
		// year negative.
		"backlog_negative": "p1",
		// Fifth total finish p4, tenth p9.
		"cleanup_crew":    "p4",
		"spring_cleaning": "p9",
		// Peak ten, zero left after the eleventh total finish: the dent
		// ladder crosses exactly as the closet closes.
		"making_a_dent":    "p10",
		"empty_the_closet": "p10",
	}
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}
}

// TestBacklogNegativeYearBoundaries checks the calendar-year semantics:
// additions after a finish never count against it, prior-year additions
// never count, and equality is not negative.
func TestBacklogNegativeYearBoundaries(t *testing.T) {
	f := newVolumeFixture(t, 3)
	ctx := context.Background()

	// m1 finishes five days after its own addition. m2 is added after
	// m1's finish, so at m2's finish the year holds two additions and two
	// finishes. m3 was added last December — a different bucket — and its
	// March finish makes the count three against two.
	f.add("m1", 0, models.StatusPlayed, ymdStamp(-1, "01-10"), ymdStamp(-1, "01-15"))
	f.add("m2", 1, models.StatusPlayed, ymdStamp(-1, "01-20"), ymdStamp(-1, "02-01"))
	f.add("m3", 2, models.StatusPlayed, ymdStamp(-2, "12-01"), ymdStamp(-1, "03-01"))

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	want := map[string]string{
		"first_blood":      "m1",
		"speedrun":         "m1",
		"the_ancient_one":  "m3", // added last December, the oldest owned
		"backlog_negative": "m3",
		// m1 is the second-oldest of three (all three are the "3 oldest")
		// and finished five days after adding: fossil record plus both
		// acquisition-speed achievements.
		"fossil_record":         "m1",
		"instant_gratification": "m1",
		"no_shelf_time":         "m1",
	}
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}
}

// TestLiveDropsReduceBacklog covers the live drop path: honesty shrinks the
// unplayed count too, so the dent ladder and the closed closet both unlock
// through UpdateEntry drops.
func TestLiveDropsReduceBacklog(t *testing.T) {
	f := newVolumeFixture(t, 12)
	ctx := context.Background()

	for i := 0; i < 12; i++ {
		f.add(fmt.Sprintf("d%d", i), i, models.StatusBacklog, ymdStamp(-1, "01-01"), "")
	}

	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("d%d", i)
		_, unlocks, err := f.s.UpdateEntry(ctx, "u3", id, EntryUpdate{Status: strptr(models.StatusDropped)})
		if err != nil {
			t.Fatalf("drop %s: %v", id, err)
		}
		got := unlockedIDs(unlocks)
		switch i {
		case 4: // fifth drop: the breakup
			if got["wasnt_you_it_was_me"] != id {
				t.Errorf("after 5 drops, wasnt_you_it_was_me = %q, want %q", got["wasnt_you_it_was_me"], id)
			}
		case 5: // sixth: the breakup doesn't fire again
			if _, ok := got["wasnt_you_it_was_me"]; ok {
				t.Errorf("wasnt_you_it_was_me unlocked again at %s", id)
			}
		case 9: // tenth drop: reduction exactly ten, and the reaper
			if got["making_a_dent"] != id {
				t.Errorf("after 10 drops, making_a_dent = %q, want %q", got["making_a_dent"], id)
			}
			if got["the_reaper"] != id {
				t.Errorf("after 10 drops, the_reaper = %q, want %q", got["the_reaper"], id)
			}
		case 10: // eleventh: nothing new on the dent ladder
			if _, ok := got["making_a_dent"]; ok {
				t.Errorf("making_a_dent unlocked again at %s", id)
			}
		case 11: // twelfth: the closet closes
			if got["empty_the_closet"] != id {
				t.Errorf("after 12 drops, empty_the_closet = %q, want %q", got["empty_the_closet"], id)
			}
			if got["mass_extinction"] != "" {
				t.Errorf("mass_extinction unlocked at %s, want still locked (reduction 12 < 50)", id)
			}
		}
		if got["one_down"] != "" {
			t.Errorf("one_down unlocked on a drop, want finish-only")
		}
	}
}

// TestLiveFoldAndRemorse pins the logged-time and acquisition-age drop
// predicates through UpdateEntry: five honest hours folds 'em, one minute
// less does not; dropping inside seven days of adding is buyer's remorse.
func TestLiveFoldAndRemorse(t *testing.T) {
	f := newVolumeFixture(t, 3)
	ctx := context.Background()
	database := f.s.DB()

	// f0's game has a 30h estimate (300 logged minutes are not "losses");
	// f1 and f2 sit on a 100h estimate so the fold and cut boundaries are
	// clean.
	if _, err := database.ExecContext(ctx,
		`UPDATE games SET time_to_beat_main = 108000 WHERE id = 100`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE games SET time_to_beat_main = 360000 WHERE id IN (101, 102)`); err != nil {
		t.Fatal(err)
	}
	f.add("f0", 0, models.StatusBacklog, ymdStamp(-1, "01-01"), "")
	f.add("f1", 1, models.StatusPlaying, ymdStamp(-1, "06-01"), "")
	f.add("f2", 2, models.StatusBacklog, time.Now().UTC().Format("2006-01-02 15:04:05"), "")
	// Five hours logged on f0; four hours and change on f1.
	for i := 0; i < 5; i++ {
		if _, _, err := f.s.AddSession(ctx, "u3", "f0", ymdStamp(-1, fmt.Sprintf("02-%02d", i+1))[:10], 60, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.s.AddSession(ctx, "u3", "f1", ymdStamp(-1, "07-01")[:10], 240, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.AddSession(ctx, "u3", "f1", ymdStamp(-1, "07-02")[:10], 59, ""); err != nil {
		t.Fatal(err)
	}

	// f0: five hours exactly, added ~a year ago — fold 'em fires.
	_, unlocks, err := f.s.UpdateEntry(ctx, "u3", "f0", EntryUpdate{Status: strptr(models.StatusDropped)})
	if err != nil {
		t.Fatal(err)
	}
	if got := unlockedIDs(unlocks); got["know_when_to_fold"] != "f0" {
		t.Errorf("f0 unlocks = %v, want know_when_to_fold", got)
	}

	// f1: 299 minutes logged — fold 'em stays locked, but under a tenth
	// of the 100h estimate cuts the losses.
	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "f1", EntryUpdate{Status: strptr(models.StatusDropped)})
	if err != nil {
		t.Fatal(err)
	}
	got := unlockedIDs(unlocks)
	if _, ok := got["know_when_to_fold"]; ok {
		t.Error("299 minutes should not fold 'em")
	}
	if got["cut_your_losses"] != "f1" {
		t.Errorf("f1 unlocks = %v, want cut_your_losses", got)
	}

	// f2: added today and dropped — buyer's remorse, nothing harder.
	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "f2", EntryUpdate{Status: strptr(models.StatusDropped)})
	if err != nil {
		t.Fatal(err)
	}
	if got := unlockedIDs(unlocks); got["buyers_remorse"] != "f2" {
		t.Errorf("f2 unlocks = %v, want buyers_remorse", got)
	}
	if _, ok := unlockedIDs(unlocks)["know_when_to_fold"]; ok {
		t.Error("an unplayed day-one drop should not fold 'em")
	}
}

// TestLiveComebackCycle drives the whole redemption arc through UpdateEntry:
// drop, resume, finish — with the drop history recorded live, the resume
// fires Resurrection and the finish fires Never Give Up.
func TestLiveComebackCycle(t *testing.T) {
	f := newVolumeFixture(t, 1)
	ctx := context.Background()

	f.add("c1", 0, models.StatusBacklog, ymdStamp(-2, "03-01"), "")

	if _, _, err := f.s.UpdateEntry(ctx, "u3", "c1", EntryUpdate{Status: strptr(models.StatusDropped)}); err != nil {
		t.Fatal(err)
	}
	_, unlocks, err := f.s.UpdateEntry(ctx, "u3", "c1", EntryUpdate{Status: strptr(models.StatusPlaying)})
	if err != nil {
		t.Fatal(err)
	}
	if got := unlockedIDs(unlocks); got["resurrection"] != "c1" {
		t.Fatalf("resume unlocks = %v, want resurrection on c1", got)
	}
	// The gap is seconds, not months: no second chance yet.
	if _, ok := unlockedIDs(unlocks)["second_chance"]; ok {
		t.Error("an immediate resume should not earn second_chance")
	}

	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "c1", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatal(err)
	}
	if got := unlockedIDs(unlocks); got["never_give_up"] != "c1" {
		t.Fatalf("finish unlocks = %v, want never_give_up on c1", got)
	}
	// Direct finish from dropped also counts: re-drop, then finish without
	// a resume in between.
	if _, _, err := f.s.UpdateEntry(ctx, "u3", "c1", EntryUpdate{Status: strptr(models.StatusDropped)}); err != nil {
		t.Fatal(err)
	}
	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "c1", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatal(err)
	}
	got := unlockedIDs(unlocks)
	if _, ok := got["never_give_up"]; ok {
		t.Error("never_give_up should not unlock twice")
	}
	if _, ok := got["against_all_odds"]; ok {
		t.Error("a seconds-long drop should not earn against_all_odds")
	}
}

// TestLiveSecondChance resumes a game dropped 6+ months ago (a real history
// row, backdated) — the resume event reads the arc and fires the window
// predicates live.
func TestLiveSecondChance(t *testing.T) {
	f := newVolumeFixture(t, 1)
	ctx := context.Background()
	database := f.s.DB()

	f.add("s1", 0, models.StatusDropped, ymdStamp(-2, "03-01"), ymdStamp(-1, "01-10"))
	if _, err := database.ExecContext(ctx, `
		INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
		VALUES ('hs1', 's1', 'u3', 'playing', 'dropped', ?)`,
		ymdStamp(-1, "01-10")); err != nil {
		t.Fatal(err)
	}

	_, unlocks, err := f.s.UpdateEntry(ctx, "u3", "s1", EntryUpdate{Status: strptr(models.StatusBacklog)})
	if err != nil {
		t.Fatal(err)
	}
	got := unlockedIDs(unlocks)
	if got["resurrection"] != "s1" {
		t.Errorf("resume unlocks = %v, want resurrection on s1", got)
	}
	// Dropped mid-January last year, resumed now: well past six months.
	if got["second_chance"] != "s1" {
		t.Errorf("resume unlocks = %v, want second_chance on s1", got)
	}
	if _, ok := got["never_give_up"]; ok {
		t.Error("a resume must not fire finish predicates")
	}
}

// TestBackfillComebackArcs replays seeded status history through the gallery
// backfill: resume rows fire at their historical moments, finishes pick up
// the arcs that predate them, and a drop closed by finishing directly uses
// the finish itself as the return.
func TestBackfillComebackArcs(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	seed(`INSERT INTO users (id, email, username, password_hash) VALUES ('u4', 'u4@example.com', 'u4', 'x')`)
	seed(`INSERT INTO games (id, name, time_to_beat_main) VALUES (400, 'Arc One', 3600), (401, 'Arc Two', 3600)`)

	now := time.Now().UTC()
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	history := func(id, entry, from, to string, at time.Time) {
		seed(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
			VALUES (?, ?, 'u4', ?, ?, ?)`, id, entry, from, to, stamp(at))
	}

	// b1: dropped 3 years ago, resumed ~a year later (past both the
	// six-month and one-year windows), finished a year after that. b1 is
	// the older owned game, so the age-rank achievements ride its finish.
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
		VALUES ('b1', 'u4', 400, 'played', ?, ?)`,
		stamp(now.AddDate(-3, 0, -10)), stamp(now.AddDate(-1, 0, 0)))
	history("hb1a", "b1", "backlog", "dropped", now.AddDate(-3, 0, 0))
	history("hb1b", "b1", "dropped", "playing", now.AddDate(-1, -11, -29))
	history("hb1c", "b1", "playing", "played", now.AddDate(-1, 0, 0))
	// b2: dropped 3 years ago and finished directly 2 years later — the
	// open arc makes the finish itself the return, past the phoenix window.
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
		VALUES ('b2', 'u4', 401, 'played', ?, ?)`,
		stamp(now.AddDate(-3, 0, -5)), stamp(now.AddDate(-1, 0, 0)))
	history("hb2a", "b2", "backlog", "dropped", now.AddDate(-3, 0, 0))
	history("hb2b", "b2", "dropped", "played", now.AddDate(-1, 0, 0))

	statuses, err := s.Achievements(ctx, "u4")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	want := map[string]string{
		// b1's finish: first, oldest owned, unlogged, nothing added in
		// the finish year, and the closed arc behind it.
		"first_blood":      "b1",
		"speedrun":         "b1",
		"the_ancient_one":  "b1",
		"fossil_record":    "b1",
		"backlog_negative": "b1",
		"never_give_up":    "b1",
		"against_all_odds": "b1",
		// The resume replay: both windows crossed at the resume moment.
		"resurrection":  "b1",
		"second_chance": "b1",
		// b2's finish closes its own arc: 2 years drop → finish.
		"phoenix": "b2",
	}
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}

	// Idempotent across gallery loads.
	if _, err := s.Achievements(ctx, "u4"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u4'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(want) {
		t.Errorf("second backfill left %d unlocks, want %d", count, len(want))
	}
}

// TestAcquisitionSpeedBoundaries pins the 7/30-day windows through the
// backfill replay, where the finish stamp is exact: one user per boundary
// so idempotent unlocks can't mask a wrong crossing.
func TestAcquisitionSpeedBoundaries(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	cases := []struct {
		name      string
		owned     time.Duration
		wantInst  bool
		wantShelf bool
	}{
		{"finished exactly seven days after adding", 7 * 24 * time.Hour, true, true},
		{"finished seven days and an hour after adding", 7*24*time.Hour + time.Hour, true, false},
		{"finished exactly thirty days after adding", 30 * 24 * time.Hour, true, false},
		{"finished thirty days and an hour after adding", 30*24*time.Hour + time.Hour, false, false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, game, entry := fmt.Sprintf("u%d", 10+i), int64(300+i), fmt.Sprintf("q%d", i)
			added := time.Date(time.Now().UTC().Year(), 6, 1, 12, 0, 0, 0, time.UTC)
			finished := added.Add(tc.owned)
			stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

			seed := func(query string, args ...any) {
				t.Helper()
				if _, err := database.ExecContext(ctx, query, args...); err != nil {
					t.Fatalf("seed %q: %v", query, err)
				}
			}
			seed(`INSERT INTO users (id, email, username, password_hash) VALUES (?, ?, ?, 'x')`,
				user, user+"@example.com", user)
			seed(`INSERT INTO games (id, name) VALUES (?, 'Speed Game')`, game)
			seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
				VALUES (?, ?, ?, 'played', ?, ?)`, entry, user, game, stamp(added), stamp(finished))

			statuses, err := s.Achievements(ctx, user)
			if err != nil {
				t.Fatalf("Achievements: %v", err)
			}
			got := unlockedIDs(statuses)
			if tc.wantInst && got["instant_gratification"] != entry {
				t.Errorf("instant_gratification = %q, want %q", got["instant_gratification"], entry)
			}
			if !tc.wantInst && got["instant_gratification"] != "" {
				t.Errorf("instant_gratification unlocked at %q, want locked", got["instant_gratification"])
			}
			if tc.wantShelf && got["no_shelf_time"] != entry {
				t.Errorf("no_shelf_time = %q, want %q", got["no_shelf_time"], entry)
			}
			if !tc.wantShelf && got["no_shelf_time"] != "" {
				t.Errorf("no_shelf_time unlocked at %q, want locked", got["no_shelf_time"])
			}
		})
	}
}

// TestFossilRecordRanks checks the ownership-age rank semantics in the
// backfill: ties on created_at share a rank (two entries tied oldest push
// the next entry to rank 3, still inside the record), and the fourth
// oldest — finishing first — earns nothing.
func TestFossilRecordRanks(t *testing.T) {
	f := newVolumeFixture(t, 4)
	ctx := context.Background()

	// t1 and t2 share the oldest stamp (both rank 1), so t3 ranks 3 and
	// t4 ranks 4. t4 finishes first — outside the record — then t3's
	// finish opens it.
	oldest := ymdStamp(-2, "01-01")
	f.add("t1", 0, models.StatusPlayed, oldest, ymdStamp(-1, "06-01"))
	f.add("t2", 1, models.StatusPlayed, oldest, ymdStamp(-1, "06-02"))
	f.add("t3", 2, models.StatusPlayed, ymdStamp(-1, "03-01"), ymdStamp(-1, "05-15"))
	f.add("t4", 3, models.StatusPlayed, ymdStamp(-1, "03-02"), ymdStamp(-1, "05-01"))

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	if got["fossil_record"] != "t3" {
		t.Errorf("fossil_record = %q, want t3 (rank 3, after the rank-4 t4 finishes first)", got["fossil_record"])
	}
	// t1 is the first rank-1 entry to finish, so it keeps the Ancient One.
	if got["the_ancient_one"] != "t1" {
		t.Errorf("the_ancient_one = %q, want t1", got["the_ancient_one"])
	}
}
