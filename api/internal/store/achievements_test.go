package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newAchievementsStore opens a migrated SQLite database in a temp dir seeded
// with u1's mixed history — four finished games across the ownership ages, one
// drop, one backlog straggler — and u2 with nothing, for scoping checks.
// Dates are relative to now so the suite holds whenever it runs.
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

	// games: (id, hours to beat main, hours to beat complete)
	for _, g := range []struct {
		id             int64
		main, complete int64
	}{
		{1, 10, 20},
		{2, 4, 8},
		{3, 60, 120},
		{4, 8, 16},
		{5, 12, 24},
		{6, 10, 20},
	} {
		exec(`INSERT INTO games (id, name, time_to_beat_main, time_to_beat_complete) VALUES (?, ?, ?, ?)`,
			g.id, "Game "+string(rune('A'+g.id-1)), g.main*3600, g.complete*3600)
	}

	now := time.Now().UTC()
	ago := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

	addEntry := func(id string, userID string, gameID int64, status string, createdDaysAgo int, finishedDaysAgo int, sessionMinutes []int) {
		t.Helper()
		var fin any
		if finishedDaysAgo > 0 {
			fin = stamp(ago(finishedDaysAgo))
		}
		exec(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, userID, gameID, status, stamp(ago(createdDaysAgo)), fin)
		for i, minutes := range sessionMinutes {
			exec(`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes)
				VALUES (?, ?, ?, ?, ?)`,
				"ps-"+id+string(rune('a'+i)), userID, id, stamp(ago(35))[:10], minutes)
		}
	}

	addEntry("a1", "u1", 1, models.StatusPlayed, 8*365, 120, []int{120, 60})
	addEntry("a2", "u1", 2, models.StatusPlayed, 40, 90, []int{300})
	addEntry("a3", "u1", 3, models.StatusPlayed, 500, 60, []int{1440, 1440, 1440, 1440, 60})
	addEntry("a4", "u1", 4, models.StatusPlayed, 200, 30, []int{960})
	addEntry("a5", "u1", 5, models.StatusDropped, 800, 100, nil)
	addEntry("a6", "u1", 6, models.StatusBacklog, 2200, 0, nil)

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
		// a1 is the first finish, the oldest-owned game, an 8-year dig with
		// 3h logged — four achievements off one game.
		"first_blood":        "a1",
		"archaeologist":      "a1",
		"speedrun":           "a1",
		"the_ancient_one":    "a1",
		// a3 is the 60h main-estimate game.
		"long_haul":          "a3",
		// a4 logged exactly its 16h completion estimate.
		"completionist":      "a4",
		// a5 was dropped after ~2.2 years of ownership.
		"abandonment_issues": "a5",
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
	// year dig with nothing logged, so archaeologist and speedrun ride along.
	_, unlocks, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusPlayed)})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got := unlockedIDs(unlocks)
	want := map[string]string{
		"first_blood":   "a6",
		"cleanup_crew":  "a6",
		"speedrun":      "a6",
		"archaeologist": "a6",
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
