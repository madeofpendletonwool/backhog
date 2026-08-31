package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
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
		// a3 is the third June finish of last year and the 60h
		// main-estimate game.
		"hat_trick": "a3",
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
		// The lazy windows: nothing non-wishlist has been added since
		// a4, well over a year ago.
		"restraint":  "",
		"discipline": "",
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
	// The live finish stamps now, so which month-scoped achievements ride
	// along depends on when the suite runs.
	switch time.Now().UTC().Month() {
	case time.January:
		want["season_opener"] = "a6"
	case time.December:
		want["strong_finish"] = "a6"
	}
	if len(got) != len(want) {
		t.Fatalf("live unlocks = %v, want %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s = %q, want %q", id, got[id], entryID)
		}
	}

	// Un-finishing and re-finishing must not unlock anything twice. The
	// one newcomer is legitimate: returning a6 to the queue leaves it
	// sitting alone at the top, so the re-finish earns Next!.
	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusBacklog)}); err != nil {
		t.Fatal(err)
	}
	_, unlocks, err = s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatal(err)
	}
	if got := unlockedIDs(unlocks); len(got) != 1 || got["next_up"] != "a6" {
		t.Errorf("re-finish unlocked %v, want only next_up on a6", got)
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
		// record opens too. January finish, so the season opens with it.
		"first_blood":     "c0",
		"speedrun":        "c0",
		"the_ancient_one": "c0",
		"fossil_record":   "c0",
		"one_down":        "c0",
		"season_opener":   "c0",
		// Two finishes vs one same-year addition at the second finish.
		"backlog_negative": "c1",
		// The count ladder attaches to its crossing game.
		"cleanup_crew":    "c4",
		"spring_cleaning": "c9",
		// Peak eleven, one left after the tenth finish: reduction ten.
		"making_a_dent": "c9",
		// Ten straight monthly finishes: the machine's third consecutive
		// month lands on c2 (Jan→Feb→Mar).
		"backlog_machine": "c2",
		// The lazy windows: nothing added since the January straggler.
		"restraint":  "",
		"discipline": "",
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
		// The year's monthly run: January opens the season, and the
		// third consecutive month (Jan→Feb→Mar) starts the machine.
		"season_opener":   "p1",
		"backlog_machine": "p3",
		// The lazy windows: nothing added since two years ago.
		"restraint":  "",
		"discipline": "",
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
		// The three finishes run Jan→Feb→Mar: the January one opens the
		// season, the March one is the third consecutive month.
		"season_opener":   "m1",
		"backlog_machine": "m3",
		// m1 is the second-oldest of three (all three are the "3 oldest")
		// and finished five days after adding: fossil record plus both
		// acquisition-speed achievements.
		"fossil_record":         "m1",
		"instant_gratification": "m1",
		"no_shelf_time":         "m1",
		// The lazy windows: the year-old additions are far behind.
		"restraint":  "",
		"discipline": "",
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
		// The lazy windows: nothing added in three years.
		"restraint":  "",
		"discipline": "",
	}
	// Both finishes land in the calendar month a year before the suite
	// runs, so a January or December run adds the month-scoped pair.
	switch time.Now().UTC().Month() {
	case time.January:
		want["season_opener"] = "b1"
	case time.December:
		want["strong_finish"] = "b1"
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

// TestBackfillCalendarLadder drives the calendar predicates through the
// completion-order replay: month buckets grow exactly as they did live, the
// streak crosses the Dec→Jan year wrap, and the season bookends attach to
// their months.
func TestBackfillCalendarLadder(t *testing.T) {
	f := newVolumeFixture(t, 8)
	ctx := context.Background()

	// Eight games bought three years back (spread days for clean
	// ownership ranks): one October finish, five November finishes, a
	// December finish, and a January finish after the year wraps.
	f.add("o1", 0, models.StatusPlayed, ymdStamp(-3, "01-01"), ymdStamp(-1, "10-05"))
	f.add("n1", 1, models.StatusPlayed, ymdStamp(-3, "01-02"), ymdStamp(-1, "11-02"))
	f.add("n2", 2, models.StatusPlayed, ymdStamp(-3, "01-03"), ymdStamp(-1, "11-09"))
	f.add("n3", 3, models.StatusPlayed, ymdStamp(-3, "01-04"), ymdStamp(-1, "11-16"))
	f.add("n4", 4, models.StatusPlayed, ymdStamp(-3, "01-05"), ymdStamp(-1, "11-23"))
	f.add("n5", 5, models.StatusPlayed, ymdStamp(-3, "01-06"), ymdStamp(-1, "11-28"))
	f.add("d0", 6, models.StatusPlayed, ymdStamp(-3, "01-07"), ymdStamp(-1, "12-10"))
	f.add("j0", 7, models.StatusPlayed, ymdStamp(-3, "01-08"), ymdStamp(0, "01-08"))

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	want := map[string]string{
		"first_blood":      "o1",
		"speedrun":         "o1",
		"the_ancient_one":  "o1",
		"fossil_record":    "o1",
		"backlog_negative": "o1",
		// The November run: hat trick on the third finish of the month,
		// Cleanup Crew (fifth total) on n4, Cleanup Month (fifth of
		// November) on n5.
		"hat_trick":     "n3",
		"cleanup_crew":  "n4",
		"cleanup_month": "n5",
		// Oct→Nov→Dec: the machine crosses on the December finish, which
		// is itself a strong finish. The January finish after the wrap
		// is also a three-year dig.
		"backlog_machine": "d0",
		"strong_finish":   "d0",
		"season_opener":   "j0",
		"dusty_relic":     "j0",
		// The lazy windows: nothing added in three years.
		"restraint":  "",
		"discipline": "",
	}
	got := unlockedIDs(statuses)
	if len(got) != len(want) {
		t.Fatalf("unlocked %v, want exactly %v", got, want)
	}
	for id, entryID := range want {
		if got[id] != entryID {
			t.Errorf("%s attached to %q, want %q", id, got[id], entryID)
		}
	}
}

// TestBackfillStreakBoundaries pins the consecutive-month semantics: a
// missed month resets the count to one, and the Dec→Jan wrap continues a
// live streak instead of breaking it.
func TestBackfillStreakBoundaries(t *testing.T) {
	t.Run("a missed month breaks the streak", func(t *testing.T) {
		f := newVolumeFixture(t, 2)
		ctx := context.Background()
		f.add("g0", 0, models.StatusPlayed, ymdStamp(-2, "01-01"), ymdStamp(-1, "10-05"))
		f.add("g1", 1, models.StatusPlayed, ymdStamp(-2, "01-02"), ymdStamp(-1, "12-05"))

		statuses, err := f.s.Achievements(ctx, "u3")
		if err != nil {
			t.Fatalf("Achievements: %v", err)
		}
		if got := unlockedIDs(statuses); got["backlog_machine"] != "" {
			t.Errorf("backlog_machine unlocked on %q with a November gap, want locked", got["backlog_machine"])
		}
	})

	t.Run("the year wrap continues the streak", func(t *testing.T) {
		f := newVolumeFixture(t, 3)
		ctx := context.Background()
		f.add("w0", 0, models.StatusPlayed, ymdStamp(-2, "01-01"), ymdStamp(-1, "11-05"))
		f.add("w1", 1, models.StatusPlayed, ymdStamp(-2, "01-02"), ymdStamp(-1, "12-05"))
		f.add("w2", 2, models.StatusPlayed, ymdStamp(-2, "01-03"), ymdStamp(0, "01-05"))

		statuses, err := f.s.Achievements(ctx, "u3")
		if err != nil {
			t.Fatalf("Achievements: %v", err)
		}
		if got := unlockedIDs(statuses); got["backlog_machine"] != "w2" {
			t.Errorf("backlog_machine = %q, want w2 (Nov→Dec→Jan crosses the year wrap)", got["backlog_machine"])
		}
	})
}

// TestBackfillSummerBucket checks that Summer Cleanup pools only the
// June–August stretch of a single year: five spread across one summer
// unlocks, five split across two summers never does.
func TestBackfillSummerBucket(t *testing.T) {
	t.Run("five across one summer", func(t *testing.T) {
		f := newVolumeFixture(t, 5)
		ctx := context.Background()
		f.add("s1", 0, models.StatusPlayed, ymdStamp(-2, "01-01"), ymdStamp(-1, "06-05"))
		f.add("s2", 1, models.StatusPlayed, ymdStamp(-2, "01-02"), ymdStamp(-1, "06-20"))
		f.add("s3", 2, models.StatusPlayed, ymdStamp(-2, "01-03"), ymdStamp(-1, "07-10"))
		f.add("s4", 3, models.StatusPlayed, ymdStamp(-2, "01-04"), ymdStamp(-1, "07-25"))
		f.add("s5", 4, models.StatusPlayed, ymdStamp(-2, "01-05"), ymdStamp(-1, "08-15"))

		statuses, err := f.s.Achievements(ctx, "u3")
		if err != nil {
			t.Fatalf("Achievements: %v", err)
		}
		if got := unlockedIDs(statuses); got["summer_cleanup"] != "s5" {
			t.Errorf("summer_cleanup = %q, want s5 (fifth finish of the summer)", got["summer_cleanup"])
		}
	})

	t.Run("two summers do not pool", func(t *testing.T) {
		f := newVolumeFixture(t, 5)
		ctx := context.Background()
		f.add("s1", 0, models.StatusPlayed, ymdStamp(-3, "01-01"), ymdStamp(-1, "06-05"))
		f.add("s2", 1, models.StatusPlayed, ymdStamp(-3, "01-02"), ymdStamp(-1, "06-20"))
		f.add("s3", 2, models.StatusPlayed, ymdStamp(-3, "01-03"), ymdStamp(-1, "07-10"))
		f.add("s4", 3, models.StatusPlayed, ymdStamp(-3, "01-04"), ymdStamp(-1, "07-25"))
		f.add("s5", 4, models.StatusPlayed, ymdStamp(-3, "01-05"), ymdStamp(0, "06-05"))

		statuses, err := f.s.Achievements(ctx, "u3")
		if err != nil {
			t.Fatalf("Achievements: %v", err)
		}
		if got := unlockedIDs(statuses); got["summer_cleanup"] != "" {
			t.Errorf("summer_cleanup unlocked on %q with finishes split 4+1 across summers, want locked", got["summer_cleanup"])
		}
	})
}

// TestBackfillPerfectSeason walks a full calendar year of monthly finishes:
// only the December finish — the twelfth distinct month — crowns it, and a
// year with a gap (the streak/summer fixtures) never gets there.
func TestBackfillPerfectSeason(t *testing.T) {
	f := newVolumeFixture(t, 12)
	ctx := context.Background()

	for i := 1; i <= 12; i++ {
		f.add(fmt.Sprintf("p%02d", i), i-1, models.StatusPlayed,
			ymdStamp(-2, fmt.Sprintf("01-%02d", i)),
			ymdStamp(-1, fmt.Sprintf("%02d-15", i)))
	}

	statuses, err := f.s.Achievements(ctx, "u3")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	want := map[string]string{
		// The January finish opens everything an only-finish can —
		// including One Down, with all eleven later games unplayed.
		"first_blood":      "p01",
		"speedrun":         "p01",
		"the_ancient_one":  "p01",
		"fossil_record":    "p01",
		"backlog_negative": "p01",
		"one_down":         "p01",
		"season_opener":    "p01",
		// The ladders cross on their own games.
		"cleanup_crew":    "p05",
		"backlog_machine": "p03",
		"spring_cleaning": "p10",
		// The tenth finish leaves two of twelve unplayed — reduction
		// exactly ten. December carries the triple crown: strong finish,
		// the emptied closet, and the twelfth distinct month.
		"making_a_dent":    "p10",
		"strong_finish":    "p12",
		"empty_the_closet": "p12",
		"perfect_season":   "p12",
		// The lazy windows.
		"restraint":  "",
		"discipline": "",
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

// TestLiveQueueTopFinish exercises the Next! capture on the real update
// path: the queue rank is read before the finishing UPDATE clears the
// entry's position, so the queue top earns it, mid-queue does not, and a
// game that left the queue by starting to play cannot earn it afterwards.
func TestLiveQueueTopFinish(t *testing.T) {
	f := newVolumeFixture(t, 3)
	ctx := context.Background()
	database := f.s.DB()

	f.add("q1", 0, models.StatusBacklog, ymdStamp(-1, "01-01"), "")
	f.add("q2", 1, models.StatusBacklog, ymdStamp(-1, "01-02"), "")
	f.add("q3", 2, models.StatusBacklog, ymdStamp(-1, "01-03"), "")
	for id, pos := range map[string]float64{"q1": 1024, "q2": 2048, "q3": 3072} {
		if _, err := database.ExecContext(ctx,
			`UPDATE library_entries SET queue_position = ? WHERE id = ?`, pos, id); err != nil {
			t.Fatal(err)
		}
	}

	// Finishing from the middle of the queue earns nothing queue-shaped.
	_, unlocks, err := f.s.UpdateEntry(ctx, "u3", "q2", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish q2: %v", err)
	}
	if got := unlockedIDs(unlocks); got["next_up"] != "" {
		t.Errorf("mid-queue finish earned next_up on %q", got["next_up"])
	}

	// Finishing the top of the queue does.
	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "q1", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish q1: %v", err)
	}
	if got := unlockedIDs(unlocks); got["next_up"] != "q1" {
		t.Errorf("queue-top finish: next_up = %q, want q1", got["next_up"])
	}

	// Starting the last one moves it out of the queue (the auto
	// backlog→playing flip clears its position); finishing it afterwards
	// must not count as finishing the queue top.
	if _, _, err := f.s.AddSession(ctx, "u3", "q3", time.Now().UTC().Format("2006-01-02"), 30, ""); err != nil {
		t.Fatalf("session q3: %v", err)
	}
	_, unlocks, err = f.s.UpdateEntry(ctx, "u3", "q3", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish q3: %v", err)
	}
	if got := unlockedIDs(unlocks); got["next_up"] != "" {
		t.Errorf("finish after leaving the queue earned next_up on %q", got["next_up"])
	}
}

// TestRestraintDisciplineWindows drives the lazy windows through the
// gallery: 30/90-day gaps measured from the last non-wishlist add, with
// wishlist additions explicitly not resetting the clock.
func TestRestraintDisciplineWindows(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	now := time.Now().UTC()
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	user := func(id string, gameID int64, status string, created time.Time) {
		t.Helper()
		if _, err := database.ExecContext(ctx,
			`INSERT INTO users (id, email, username, password_hash) VALUES (?, ?, ?, 'x')`,
			id, id+"@example.com", id); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO games (id, name) VALUES (?, ?)`, gameID, "Window Game "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO library_entries (id, user_id, game_id, status, created_at) VALUES (?, ?, ?, ?, ?)`,
			"w-"+id, id, gameID, status, stamp(created)); err != nil {
			t.Fatal(err)
		}
	}

	// Ten days quiet, forty days quiet, and a hundred days quiet with a
	// wishlist entry added yesterday (a different game — one entry per
	// user per game).
	user("u20", 600, models.StatusBacklog, now.AddDate(0, 0, -10))
	user("u21", 601, models.StatusBacklog, now.AddDate(0, 0, -40))
	user("u22", 602, models.StatusBacklog, now.AddDate(0, 0, -100))
	if _, err := database.ExecContext(ctx,
		`INSERT INTO games (id, name) VALUES (603, 'Window Wish')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO library_entries (id, user_id, game_id, status, created_at)
		VALUES ('w-u22w', 'u22', 603, 'wishlist', ?)`, stamp(now.AddDate(0, 0, -1))); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		user                          string
		wantRestraint, wantDiscipline bool
	}{
		{"u20", false, false},
		{"u21", true, false},
		{"u22", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			statuses, err := s.Achievements(ctx, tt.user)
			if err != nil {
				t.Fatalf("Achievements: %v", err)
			}
			got := unlockedIDs(statuses)
			if _, ok := got["restraint"]; ok != tt.wantRestraint {
				t.Errorf("restraint unlocked = %v, want %v", ok, tt.wantRestraint)
			}
			if _, ok := got["discipline"]; ok != tt.wantDiscipline {
				t.Errorf("discipline unlocked = %v, want %v", ok, tt.wantDiscipline)
			}
			// A lazy unlock carries no triggering game.
			if tt.wantRestraint && got["restraint"] != "" {
				t.Errorf("restraint attached to entry %q, want no triggering game", got["restraint"])
			}
		})
	}
}

// seriesFixture seeds a series-flavoured library: user u5 owns a trilogy
// plus its DLC and one foreign game (series srA), user u6 owns a six-entry
// saga (series srB) with one member dropped, and users u7/u8 get backlog
// rows to finish live. Every series predicate has a deterministic crossing
// somewhere in here.
func seriesFixture(t *testing.T) *Store {
	t.Helper()
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	addSeriesEntry := func(id, user string, game int64, status, created, finished string) {
		t.Helper()
		var fin any
		if finished != "" {
			fin = finished
		}
		seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?)`, id, user, game, status, created, fin)
	}

	seed(`INSERT INTO users (id, email, username, password_hash) VALUES
		('u5', 'u5@example.com', 'u5', 'x'),
		('u6', 'u6@example.com', 'u6', 'x'),
		('u7', 'u7@example.com', 'u7', 'x'),
		('u8', 'u8@example.com', 'u8', 'x')`)

	for i := int64(0); i < 5; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 500+i, fmt.Sprintf("Saga A %d", i))
	}
	for i := int64(0); i < 6; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 510+i, fmt.Sprintf("Saga B %d", i))
	}
	seed(`INSERT INTO series (id, name) VALUES ('srA', 'Saga A'), ('srB', 'Saga B')`)
	seed(`INSERT INTO series_games (series_id, game_id, kind) VALUES
		('srA', 500, 'game'), ('srA', 501, 'game'), ('srA', 502, 'game'),
		('srA', 503, 'dlc'),
		('srB', 510, 'game'), ('srB', 511, 'game'), ('srB', 512, 'game'),
		('srB', 513, 'game'), ('srB', 514, 'game'), ('srB', 515, 'game')`)

	// u5: the trilogy fixture. Two series finishes back to back, then the
	// DLC (which must not count), then a foreign game (which must break
	// the run), then the third real entry completing everything.
	addSeriesEntry("as1", "u5", 500, models.StatusPlayed, ymdStamp(-2, "01-01"), ymdStamp(-1, "01-10"))
	addSeriesEntry("as2", "u5", 501, models.StatusPlayed, ymdStamp(-2, "01-02"), ymdStamp(-1, "01-20"))
	addSeriesEntry("as4", "u5", 503, models.StatusPlayed, ymdStamp(-2, "01-03"), ymdStamp(-1, "02-01"))
	addSeriesEntry("as5", "u5", 504, models.StatusPlayed, ymdStamp(-2, "01-04"), ymdStamp(-1, "02-15"))
	addSeriesEntry("as3", "u5", 502, models.StatusPlayed, ymdStamp(-2, "01-05"), ymdStamp(-1, "03-10"))

	// u6: a six-entry saga finished across three years with one member
	// dropped — the marathon never pools three in a year, and the loop
	// closes without the set ever being full.
	addSeriesEntry("bs1", "u6", 510, models.StatusPlayed, ymdStamp(-3, "01-01"), ymdStamp(-2, "06-01"))
	addSeriesEntry("bs2", "u6", 511, models.StatusPlayed, ymdStamp(-3, "01-02"), ymdStamp(-2, "11-15"))
	addSeriesEntry("bs3", "u6", 512, models.StatusDropped, ymdStamp(-3, "01-03"), ymdStamp(-1, "07-01"))
	addSeriesEntry("bs4", "u6", 513, models.StatusPlayed, ymdStamp(-3, "01-04"), ymdStamp(-1, "08-01"))
	addSeriesEntry("bs5", "u6", 514, models.StatusPlayed, ymdStamp(-3, "01-05"), ymdStamp(-1, "09-01"))
	addSeriesEntry("bs6", "u6", 515, models.StatusPlayed, ymdStamp(-3, "01-06"), ymdStamp(0, "01-10"))

	// u7: one trilogy entry already finished, the rest waiting to finish
	// live. u8: one saga entry finished, a foreign finish after it, three
	// more owned entries still backlog so the saga bar (5 owned) is met.
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	now := time.Now().UTC()
	addSeriesEntry("ls1", "u7", 500, models.StatusPlayed, stamp(now.AddDate(0, 0, -30)), stamp(now.AddDate(0, 0, -10)))
	addSeriesEntry("ls2", "u7", 501, models.StatusBacklog, stamp(now.AddDate(0, 0, -29)), "")
	addSeriesEntry("ls4", "u7", 503, models.StatusBacklog, stamp(now.AddDate(0, 0, -28)), "")
	addSeriesEntry("ls3", "u7", 502, models.StatusBacklog, stamp(now.AddDate(0, 0, -27)), "")
	addSeriesEntry("ls6", "u8", 510, models.StatusPlayed, stamp(now.AddDate(0, 0, -30)), stamp(now.AddDate(0, 0, -10)))
	addSeriesEntry("lf1", "u8", 504, models.StatusPlayed, stamp(now.AddDate(0, 0, -25)), stamp(now.AddDate(0, 0, -5)))
	addSeriesEntry("ls7", "u8", 511, models.StatusBacklog, stamp(now.AddDate(0, 0, -24)), "")
	addSeriesEntry("ls8", "u8", 512, models.StatusBacklog, stamp(now.AddDate(0, 0, -23)), "")
	addSeriesEntry("ls9", "u8", 513, models.StatusBacklog, stamp(now.AddDate(0, 0, -22)), "")
	addSeriesEntry("ls10", "u8", 514, models.StatusBacklog, stamp(now.AddDate(0, 0, -21)), "")

	return s
}

// seriesAchievements is the series ladder's id set, so tests can assert a
// complete picture of what fired and what stayed locked.
var seriesAchievements = []string{
	"trilogy", "back_to_back", "saga", "franchise_mode",
	"marathon_series", "closing_the_loop", "full_set",
}

// assertSeries checks every series achievement's unlock state against want
// (entry id = unlocked and attached there; "" = locked).
func assertSeries(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	for _, id := range seriesAchievements {
		if got[id] != want[id] {
			t.Errorf("%s = %q, want %q", id, got[id], want[id])
		}
	}
}

// TestBackfillSeriesMasteryA replays the trilogy fixture: the DLC finish
// never counts toward the series, the foreign finish breaks Back to Back,
// and the third real finish carries trilogy, the same-year marathon, the
// closed loop, and the full set at once.
func TestBackfillSeriesMasteryA(t *testing.T) {
	s := seriesFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u5")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	assertSeries(t, unlockedIDs(statuses), map[string]string{
		"back_to_back":     "as2", // as1 → as2, nothing between
		"trilogy":          "as3", // third kind-'game' finish (as4 is DLC)
		"marathon_series":  "as3", // as1, as2, as3 all last year
		"closing_the_loop": "as3", // srA owned: as1, as2, as3, all played
		"full_set":         "as3",
		"saga":             "", // only 3 owned entries
		"franchise_mode":   "", // only 3 finishes
	})

	// Idempotent across gallery loads.
	if _, err := s.Achievements(ctx, "u5"); err != nil {
		t.Fatal(err)
	}
}

// TestBackfillSeriesMasteryB replays the six-entry saga: the saga bar is
// met at the first finish, the marathon never pools three finishes into
// one calendar year, and the loop closes on the last unplayed member
// while the dropped entry keeps the set from ever being full.
func TestBackfillSeriesMasteryB(t *testing.T) {
	s := seriesFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u6")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	assertSeries(t, unlockedIDs(statuses), map[string]string{
		"saga":             "bs1", // six owned entries at the first finish
		"back_to_back":     "bs2", // bs1 → bs2
		"trilogy":          "bs4", // bs1, bs2, bs4
		"franchise_mode":   "bs6", // fifth finish
		"closing_the_loop": "bs6", // bs6 was the last unplayed member
		"marathon_series":  "",    // 2 + 2 + 1 across three years
		"full_set":         "",    // bs3 is dropped, played 5 of 6
	})
}

// TestLiveSeriesFinishes drives the series predicates through the live
// update path: Back to Back against a seeded earlier finish, a DLC finish
// that counts for nothing, the trilogy completing live, the saga bar
// clearing on a first live finish, and a foreign finish keeping the run
// broken.
func TestLiveSeriesFinishes(t *testing.T) {
	s := seriesFixture(t)
	ctx := context.Background()

	// u7: ls2 finishes right after ls1 (same series) — Back to Back.
	_, unlocks, err := s.UpdateEntry(ctx, "u7", "ls2", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish ls2: %v", err)
	}
	got := unlockedIDs(unlocks)
	if got["back_to_back"] != "ls2" {
		t.Errorf("ls2 unlocks = %v, want back_to_back", got)
	}
	if got["trilogy"] != "" {
		t.Errorf("second series finish unlocked trilogy on %q", got["trilogy"])
	}

	// The DLC finish counts for nothing series-shaped.
	_, unlocks, err = s.UpdateEntry(ctx, "u7", "ls4", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish ls4: %v", err)
	}
	got = unlockedIDs(unlocks)
	for _, id := range seriesAchievements {
		if got[id] != "" {
			t.Errorf("DLC finish unlocked %s on %q, want nothing series-shaped", id, got[id])
		}
	}

	// The third real finish completes the set — trilogy, loop, and full
	// set together. The DLC in between keeps Back to Back from refiring.
	_, unlocks, err = s.UpdateEntry(ctx, "u7", "ls3", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish ls3: %v", err)
	}
	got = unlockedIDs(unlocks)
	for _, id := range []string{"trilogy", "closing_the_loop", "full_set"} {
		if got[id] != "ls3" {
			t.Errorf("ls3 unlocks = %v, want %s on ls3", got, id)
		}
	}
	if got["back_to_back"] != "" {
		t.Error("ls3 re-unlocked back_to_back after the DLC finish")
	}

	// u8: five owned saga entries, but the previous finish (lf1) is
	// foreign — the saga clears, Back to Back does not.
	_, unlocks, err = s.UpdateEntry(ctx, "u8", "ls7", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish ls7: %v", err)
	}
	got = unlockedIDs(unlocks)
	if got["saga"] != "ls7" {
		t.Errorf("ls7 unlocks = %v, want saga on ls7", got)
	}
	if got["back_to_back"] != "" {
		t.Errorf("a foreign finish in between unlocked back_to_back on %q", got["back_to_back"])
	}
}

// platformMasteryFixture seeds a platform-flavoured library on top of the
// base achievements store: the catalog platforms classified through the
// real startup sync (plus one unknown platform that must degrade), and
// users whose finishes cross each diversity and platform-mastery line at a
// deterministic game. u10 walks the Nintendo breadth, u11 the PlayStation,
// Xbox, and handheld runs, u12/u13 bracket the genre-year window, u14 pins
// the retroactive boundaries, and u15 holds backlog rows to finish live.
func platformMasteryFixture(t *testing.T) *Store {
	t.Helper()
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	addFinish := func(id, user string, game int64, platform any, created, finished string) {
		t.Helper()
		var fin any
		if finished != "" {
			fin = finished
		}
		seed(`INSERT INTO library_entries (id, user_id, game_id, status, platform_id, created_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, user, game, models.StatusPlayed, platform, created, fin)
	}
	addBacklog := func(id, user string, game int64, platform int64, created string) {
		t.Helper()
		seed(`INSERT INTO library_entries (id, user_id, game_id, status, platform_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, user, game, models.StatusBacklog, platform, created)
	}

	// The classified platform rows, healed through the real startup sync
	// so classification matches production; 9999 stays unknown on purpose.
	seed(`INSERT INTO platforms (id, name) VALUES
		(18, 'NES'), (99, 'Famicom'), (19, 'SNES'), (4, 'Nintendo 64'),
		(21, 'GameCube'), (5, 'Wii'), (41, 'Wii U'), (130, 'Switch'),
		(33, 'Game Boy'), (22, 'Game Boy Color'), (24, 'Game Boy Advance'),
		(87, 'Virtual Boy'), (20, 'DS'), (37, '3DS'),
		(7, 'PS1'), (8, 'PS2'), (9, 'PS3'), (48, 'PS4'), (167, 'PS5'),
		(11, 'Xbox'), (12, 'Xbox 360'), (49, 'Xbox One'), (169, 'Xbox Series X|S'),
		(6, 'PC'), (9999, 'WeirdOS')`)
	if err := s.SyncPlatformMeta(ctx); err != nil {
		t.Fatalf("SyncPlatformMeta: %v", err)
	}

	seed(`INSERT INTO users (id, email, username, password_hash) VALUES
		('u10', 'u10@example.com', 'u10', 'x'),
		('u11', 'u11@example.com', 'u11', 'x'),
		('u12', 'u12@example.com', 'u12', 'x'),
		('u13', 'u13@example.com', 'u13', 'x'),
		('u14', 'u14@example.com', 'u14', 'x'),
		('u15', 'u15@example.com', 'u15', 'x')`)

	// u10's walk: four Nintendo consoles first, then the fifth distinct
	// platform (a Game Boy, family-switching) crosses World Tour, the GB
	// line completes two finishes later, the Wii's generation 7 crosses
	// Generation Gap and the fifth Nintendo console in one finish, and
	// the Switch seals The Big N. Famicom, PC, and the unknown platform
	// come after every line and must move nothing.
	for i := int64(0); i < 13; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 700+i, fmt.Sprintf("Mastery A %d", i))
	}
	addFinish("ma1", "u10", 700, 18, ymdStamp(-3, "02-10"), ymdStamp(-3, "02-20"))
	addFinish("ma2", "u10", 701, 19, ymdStamp(-3, "03-01"), ymdStamp(-3, "03-10"))
	addFinish("ma3", "u10", 702, 4, ymdStamp(-3, "04-01"), ymdStamp(-3, "04-10"))
	addFinish("ma4", "u10", 703, 21, ymdStamp(-3, "05-01"), ymdStamp(-3, "05-10"))
	addFinish("ma5", "u10", 704, 33, ymdStamp(-3, "06-01"), ymdStamp(-3, "06-10"))
	addFinish("ma6", "u10", 705, 22, ymdStamp(-2, "01-01"), ymdStamp(-2, "01-10"))
	addFinish("ma7", "u10", 706, 24, ymdStamp(-2, "02-01"), ymdStamp(-2, "02-10"))
	addFinish("ma8", "u10", 707, 5, ymdStamp(-2, "03-01"), ymdStamp(-2, "03-10"))
	addFinish("ma9", "u10", 708, 41, ymdStamp(-2, "04-01"), ymdStamp(-2, "04-10"))
	addFinish("ma10", "u10", 709, 130, ymdStamp(-2, "05-01"), ymdStamp(-2, "05-10"))
	addFinish("ma11", "u10", 710, 99, ymdStamp(-1, "01-01"), ymdStamp(-1, "01-10"))
	addFinish("ma12", "u10", 711, 6, ymdStamp(-1, "02-01"), ymdStamp(-1, "02-10"))
	addFinish("ma13", "u10", 712, 9999, ymdStamp(-1, "03-01"), ymdStamp(-1, "03-10"))

	// u11's run: the five PlayStation stations complete the pilgrimage,
	// the four Xbox generations go green, and four handheld generations
	// make the historian.
	for i := int64(0); i < 13; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 713+i, fmt.Sprintf("Mastery B %d", i))
	}
	addFinish("mb1", "u11", 713, 7, ymdStamp(-3, "01-01"), ymdStamp(-3, "01-05"))
	addFinish("mb2", "u11", 714, 8, ymdStamp(-3, "02-01"), ymdStamp(-3, "02-05"))
	addFinish("mb3", "u11", 715, 9, ymdStamp(-3, "03-01"), ymdStamp(-3, "03-05"))
	addFinish("mb4", "u11", 716, 48, ymdStamp(-3, "04-01"), ymdStamp(-3, "04-05"))
	addFinish("mb5", "u11", 717, 167, ymdStamp(-3, "05-01"), ymdStamp(-3, "05-05"))
	addFinish("mb6", "u11", 718, 11, ymdStamp(-2, "01-01"), ymdStamp(-2, "01-05"))
	addFinish("mb7", "u11", 719, 12, ymdStamp(-2, "02-01"), ymdStamp(-2, "02-05"))
	addFinish("mb8", "u11", 720, 49, ymdStamp(-2, "03-01"), ymdStamp(-2, "03-05"))
	addFinish("mb9", "u11", 721, 169, ymdStamp(-2, "04-01"), ymdStamp(-2, "04-05"))
	addFinish("mb10", "u11", 722, 33, ymdStamp(-2, "05-01"), ymdStamp(-2, "05-05"))
	addFinish("mb11", "u11", 723, 87, ymdStamp(-2, "06-01"), ymdStamp(-2, "06-05"))
	addFinish("mb12", "u11", 724, 24, ymdStamp(-2, "07-01"), ymdStamp(-2, "07-05"))
	addFinish("mb13", "u11", 725, 20, ymdStamp(-2, "08-01"), ymdStamp(-2, "08-05"))

	// u12/u13's genre fixtures: five distinct genres in one calendar year
	// unlock the sampler; the same five split across two years do not.
	seed(`INSERT INTO genres (id, name) VALUES
		(1, 'RPG'), (2, 'Platformer'), (3, 'Shooter'), (4, 'Puzzle'), (5, 'Racing'),
		(6, 'Fighting'), (7, 'Strategy'), (8, 'Sim'), (9, 'Adventure'), (10, 'Horror')`)
	for i := int64(0); i < 10; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 730+i, fmt.Sprintf("Genre Game %d", i))
		seed(`INSERT INTO game_genres (game_id, genre_id) VALUES (?, ?)`, 730+i, i+1)
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("sa%d", i+1)
		addFinish(id, "u12", 730+int64(i), nil, ymdStamp(-1, "05-01"), ymdStamp(-1, fmt.Sprintf("05-%02d", i+1)))
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("sb%d", i+1)
		addFinish(id, "u13", 735+int64(i), nil, ymdStamp(-1, "06-01"), ymdStamp(-1, fmt.Sprintf("06-%02d", i+1)))
	}
	addFinish("sb5", "u13", 739, nil, ymdStamp(0, "01-01"), ymdStamp(0, "01-15"))

	// u14's retroactive boundaries, all finishing the same day so the
	// unlock attaches to the first dormant replay: no session at all, a
	// session exactly five years back (blocks), one day earlier (dormant),
	// one on the finish day (blocks), and one the day after (dormant).
	for i := int64(0); i < 5; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 740+i, fmt.Sprintf("Retro Game %d", i))
	}
	session := func(entry string, platform int64, playedOn string) {
		seed(`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
			VALUES (?, 'u14', ?, ?, 60)`, "ps-"+entry, entry, playedOn)
	}
	addFinish("ra1", "u14", 740, 167, "2019-01-01 12:00:00", "2024-06-15 12:00:00")
	addFinish("ra2", "u14", 741, 18, "2019-01-01 12:00:00", "2024-06-15 12:00:00")
	session("ra2", 18, "2019-06-15")
	addFinish("ra3", "u14", 742, 19, "2019-01-01 12:00:00", "2024-06-15 12:00:00")
	session("ra3", 19, "2019-06-14")
	addFinish("ra4", "u14", 743, 4, "2019-01-01 12:00:00", "2024-06-15 12:00:00")
	session("ra4", 4, "2024-06-15")
	addFinish("ra5", "u14", 744, 5, "2019-01-01 12:00:00", "2024-06-15 12:00:00")
	session("ra5", 5, "2024-06-16")

	// u15's live stage: four platforms finished already, one Game Boy and
	// one PS5 game waiting in the backlog.
	for i := int64(0); i < 6; i++ {
		seed(`INSERT INTO games (id, name) VALUES (?, ?)`, 750+i, fmt.Sprintf("Live Game %d", i))
	}
	now := time.Now().UTC()
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	addFinish("lm1", "u15", 750, 18, stamp(now.AddDate(0, 0, -60)), stamp(now.AddDate(0, 0, -50)))
	addFinish("lm2", "u15", 751, 19, stamp(now.AddDate(0, 0, -59)), stamp(now.AddDate(0, 0, -40)))
	addFinish("lm3", "u15", 752, 4, stamp(now.AddDate(0, 0, -58)), stamp(now.AddDate(0, 0, -30)))
	addFinish("lm4", "u15", 753, 21, stamp(now.AddDate(0, 0, -57)), stamp(now.AddDate(0, 0, -20)))
	addBacklog("lb1", "u15", 754, 33, stamp(now.AddDate(0, 0, -56)))
	addBacklog("lb2", "u15", 755, 167, stamp(now.AddDate(0, 0, -55)))

	return s
}

// platformAchievements is the diversity and platform-mastery batch's id
// set, so tests can assert a complete picture of what fired and what
// stayed locked.
var platformAchievements = []string{
	"sampler", "world_tour", "retroactive", "generation_gap",
	"nintendo_time_machine", "the_big_n", "game_boy_generation",
	"handheld_historian", "playstation_pilgrim", "green_across_the_ages",
}

// assertPlatformMastery checks every platform achievement's unlock state
// against want (entry id = unlocked and attached there; "" = locked).
func assertPlatformMastery(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	for _, id := range platformAchievements {
		if got[id] != want[id] {
			t.Errorf("%s = %q, want %q", id, got[id], want[id])
		}
	}
}

// TestBackfillNintendoBreadth replays u10's walk: World Tour crosses on
// the fifth distinct platform even though the Game Boy adds no new
// generation, the Game Boy line completes on its third system, the Wii
// crosses Generation Gap and Nintendo Time Machine together, The Big N
// seals on the Switch, and the trailing Famicom, PC, and unknown-platform
// finishes move nothing — the unknown hardware counts as a platform but
// never as a generation or family member.
func TestBackfillNintendoBreadth(t *testing.T) {
	s := platformMasteryFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u10")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	assertPlatformMastery(t, unlockedIDs(statuses), map[string]string{
		"world_tour":            "ma5",  // NES, SNES, N64, GC, GB — five distinct platforms
		"game_boy_generation":   "ma7",  // GB, GBC, GBA
		"generation_gap":        "ma8",  // generations 3, 4, 5, 6, 7
		"nintendo_time_machine": "ma8",  // NES, SNES, N64, GC, Wii
		"the_big_n":             "ma10", // all seven curated consoles
		"handheld_historian":    "ma10", // GB(4), GBC(5), GBA(6), Switch(8) — the hybrid is handheld
		"retroactive":           "ma1",  // no sessions exist, so the first platform finish is dormant
		"playstation_pilgrim":   "",
		"green_across_the_ages": "",
		"sampler":               "", // no genres seeded
	})

	// Idempotent across gallery loads.
	if _, err := s.Achievements(ctx, "u10"); err != nil {
		t.Fatal(err)
	}
}

// TestBackfillPlaystationXboxHandheld replays u11's run: the pilgrimage
// completes at PS5, Xbox goes green at the Series finish, and the fourth
// handheld generation makes the historian at the DS.
func TestBackfillPlaystationXboxHandheld(t *testing.T) {
	s := platformMasteryFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u11")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	assertPlatformMastery(t, unlockedIDs(statuses), map[string]string{
		"playstation_pilgrim":   "mb5",
		"green_across_the_ages": "mb9",
		"handheld_historian":    "mb13", // GB(4), Virtual Boy(5), GBA(6), DS(7)
		"world_tour":            "mb5",  // five distinct platforms at PS5
		"generation_gap":        "mb5",  // PS1–PS5 span generations 5–9
		"retroactive":           "mb1",  // no sessions exist
		"the_big_n":             "",
		"nintendo_time_machine": "",
		"game_boy_generation":   "", // GB and GBA, no GBC
		"sampler":               "",
	})
}

// TestBackfillGenreYearWindow brackets Sampler: five distinct genres in
// one calendar year unlock on the fifth finish; the same spread split
// across two years stays locked.
func TestBackfillGenreYearWindow(t *testing.T) {
	s := platformMasteryFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u12")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	got := unlockedIDs(statuses)
	if got["sampler"] != "sa5" {
		t.Errorf("u12 sampler = %q, want sa5", got["sampler"])
	}

	statuses, err = s.Achievements(ctx, "u13")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	if got := unlockedIDs(statuses); got["sampler"] != "" {
		t.Errorf("u13 sampler unlocked on %q, want locked", got["sampler"])
	}
}

// TestBackfillRetroactiveBoundaries pins the five-year lookback: a session
// exactly five years before the finish blocks, one day earlier does not,
// a session on the finish day blocks, one the day after does not, and a
// platform with no session at all unlocks on the first finish.
func TestBackfillRetroactiveBoundaries(t *testing.T) {
	s := platformMasteryFixture(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u14")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	if got := unlockedIDs(statuses); got["retroactive"] != "ra1" {
		t.Errorf("retroactive = %q, want ra1 (first dormant replay)", got["retroactive"])
	}
}

// TestLivePlatformFinishes drives the platform batch through the live
// update path: the fifth distinct platform crosses World Tour live, a
// finish on a platform Backhog has never seen a session on fires
// Retroactive, and the locked curated sets carry their progress onto the
// gallery cards.
func TestLivePlatformFinishes(t *testing.T) {
	s := platformMasteryFixture(t)
	ctx := context.Background()

	_, unlocks, err := s.UpdateEntry(ctx, "u15", "lb1", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish lb1: %v", err)
	}
	got := unlockedIDs(unlocks)
	if got["world_tour"] != "lb1" {
		t.Errorf("lb1 unlocks = %v, want world_tour on lb1", got)
	}
	if got["retroactive"] != "lb1" {
		t.Errorf("lb1 unlocks = %v, want retroactive on lb1 (GB never played in Backhog)", got)
	}

	// The gallery shows the locked curated sets' progress in their
	// descriptions, served server-side like the tonight picks' reasons.
	// The Big N is hidden now: its locked card is fully masked — a
	// progress string would give the game away.
	statuses, err := s.Achievements(ctx, "u15")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	for _, st := range statuses {
		switch st.ID {
		case "the_big_n":
			if st.UnlockedAt != nil {
				continue
			}
			if st.Title != achievements.MaskedTitle || st.Icon != achievements.MaskedIcon {
				t.Errorf("locked the_big_n = %q/%q, want masked %q/%q",
					st.Title, st.Icon, achievements.MaskedTitle, achievements.MaskedIcon)
			}
			if st.Description != achievements.MaskedHint("the_big_n") {
				t.Errorf("locked the_big_n description = %q, want the tease %q",
					st.Description, achievements.MaskedHint("the_big_n"))
			}
		case "game_boy_generation":
			want := "Finish a game on Game Boy, Game Boy Color, and Game Boy Advance. 1/3 systems so far."
			if st.UnlockedAt == nil && st.Description != want {
				t.Errorf("locked game_boy_generation description = %q, want %q", st.Description, want)
			}
		case "playstation_pilgrim":
			want := "Finish a game on PS1, PS2, PS3, PS4, and PS5."
			if st.UnlockedAt == nil && st.Description != want {
				t.Errorf("locked playstation_pilgrim description = %q, want the plain text (0/5 says nothing)", st.Description)
			}
		}
	}

	// The PS5 finish rides a platform with no session either, but
	// Retroactive is idempotent — nothing new fires for the set lines.
	_, unlocks, err = s.UpdateEntry(ctx, "u15", "lb2", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("finish lb2: %v", err)
	}
	if got := unlockedIDs(unlocks); got["retroactive"] != "" || got["playstation_pilgrim"] != "" {
		t.Errorf("lb2 unlocks = %v, want no retroactive refire and no pilgrim", got)
	}
}

// TestUnlockEgg covers the easter-egg unlock path: the whitelist, the
// idempotent insert, the reveal, and the masking that stays intact until
// the egg actually unlocks.
func TestUnlockEgg(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	// u2 has no history: every egg sits masked on their wall.
	statuses, err := s.Achievements(ctx, "u2")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	for _, st := range statuses {
		if !st.Egg {
			continue
		}
		if st.UnlockedAt != nil {
			t.Fatalf("%s: egg unlocked without the endpoint", st.ID)
		}
		if st.Title != achievements.MaskedTitle || st.Icon != achievements.MaskedIcon {
			t.Errorf("locked egg %s = %q/%q, want masked", st.ID, st.Title, st.Icon)
		}
		if st.Description != achievements.MaskedHint(st.ID) {
			t.Errorf("locked egg %s description = %q, want the tease", st.ID, st.Description)
		}
	}

	// Predicates never fire for eggs: u1's rich backfill history leaves
	// all four locked.
	statuses, err = s.Achievements(ctx, "u1")
	if err != nil {
		t.Fatalf("Achievements u1: %v", err)
	}
	got := unlockedIDs(statuses)
	for _, id := range []string{"night_owl", "hog_watcher", "konami", "queue_shuffler"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s unlocked from backfill, want locked", id)
		}
	}

	// The unlock reveals the real identity and reports itself as new.
	status, newly, err := s.UnlockEgg(ctx, "u2", "konami")
	if err != nil {
		t.Fatalf("UnlockEgg: %v", err)
	}
	if !newly {
		t.Error("first UnlockEgg not reported as new")
	}
	if status.Title != "Old Habits" || status.Egg != true || status.Hidden != true {
		t.Errorf("revealed egg = %+v, want the real catalogue entry", status.Achievement)
	}
	if status.UnlockedAt == nil {
		t.Error("revealed egg has no unlock timestamp")
	}
	if status.Entry != nil {
		t.Errorf("egg unlock carries entry %+v, want none", status.Entry)
	}

	// Idempotent: the second call reports the original unlock, not a new one.
	again, newly, err := s.UnlockEgg(ctx, "u2", "konami")
	if err != nil {
		t.Fatalf("second UnlockEgg: %v", err)
	}
	if newly {
		t.Error("second UnlockEgg reported as new")
	}
	if !again.UnlockedAt.Equal(*status.UnlockedAt) {
		t.Errorf("second UnlockEgg timestamp = %v, want the original %v", again.UnlockedAt, status.UnlockedAt)
	}

	// The wall reflects the reveal for u2 only — u1 stays locked and
	// masked, so one user's egg cannot unlock another's.
	statuses, err = s.Achievements(ctx, "u2")
	if err != nil {
		t.Fatalf("Achievements u2 after unlock: %v", err)
	}
	if _, ok := unlockedIDs(statuses)["konami"]; !ok {
		t.Error("konami not unlocked for u2 after UnlockEgg")
	}
	statuses, err = s.Achievements(ctx, "u1")
	if err != nil {
		t.Fatalf("Achievements u1 after u2's unlock: %v", err)
	}
	if _, ok := unlockedIDs(statuses)["konami"]; ok {
		t.Error("u1 unlocked konami from u2's egg — unlocks must be user-scoped")
	}

	// The whitelist: non-egg catalogue ids and unknown ids are rejected.
	for _, id := range []string{"first_blood", "the_big_n", "rest", "nope"} {
		if _, _, err := s.UnlockEgg(ctx, "u2", id); err != ErrNotAnEgg {
			t.Errorf("UnlockEgg(%q) error = %v, want ErrNotAnEgg", id, err)
		}
	}
}
