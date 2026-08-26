package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newFixtureStore opens a migrated SQLite database in a temp dir and seeds the
// two users the insights tests slice by: u1 with a full library, u2 with a
// minimal one, u3 with nothing.
func newFixtureStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "insights.db"))
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
		('u2', 'u2@example.com', 'u2', 'x'),
		('u3', 'u3@example.com', 'u3', 'x')`)

	exec(`INSERT INTO genres (id, name) VALUES (31, 'JRPG'), (32, 'Adventure'), (33, 'Puzzle')`)
	exec(`INSERT INTO platforms (id, name) VALUES (6, 'PC (Microsoft Windows)'), (130, 'Steam Deck')`)

	// day truncates to a DATE string for played_on / created_at columns.
	day := func(t time.Time) string { return t.Format("2006-01-02") }

	type game struct {
		id        int64
		name      string
		hours     int64
		released  time.Time
		genres    []int64
		platforms []int64
	}
	addGame := func(g game) {
		t.Helper()
		exec(`INSERT INTO games (id, name, time_to_beat_main, first_release_date) VALUES (?, ?, ?, ?)`,
			g.id, g.name, g.hours*3600, g.released.Unix())
		for _, gid := range g.genres {
			exec(`INSERT INTO game_genres (game_id, genre_id) VALUES (?, ?)`, g.id, gid)
		}
		for _, pid := range g.platforms {
			exec(`INSERT INTO game_platforms (game_id, platform_id) VALUES (?, ?)`, g.id, pid)
		}
	}

	for _, g := range []game{
		{1, "Ancient Relic", 10, time.Date(2009, 3, 10, 0, 0, 0, 0, time.UTC), []int64{32}, []int64{6}},
		{2, "Mega Saga", 120, time.Date(2015, 5, 20, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{130}},
		{3, "Quick Blast", 4, time.Date(2015, 11, 2, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{130}},
		{4, "Slow Epic", 200, time.Date(2011, 2, 8, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{6}},
		{5, "Indie Darling", 6, time.Date(2011, 7, 15, 0, 0, 0, 0, time.UTC), []int64{33}, []int64{6}},
		{6, "Grind Century", 30, time.Date(2011, 9, 30, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{6}},
		{7, "Cyber Knight", 25, time.Date(2011, 4, 12, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{6}},
		{8, "Dusty Classic", 50, time.Date(1998, 6, 1, 0, 0, 0, 0, time.UTC), []int64{32}, []int64{6}},
		{9, "Phantom Old", 8, time.Date(2004, 1, 1, 0, 0, 0, 0, time.UTC), []int64{32}, []int64{6}},
		{10, "Iron Tide", 15, time.Date(2011, 10, 5, 0, 0, 0, 0, time.UTC), []int64{31}, []int64{130}},
	} {
		addGame(g)
	}

	type session struct {
		minutes int
		on      time.Time
	}
	type entry struct {
		id       string
		user     string
		game     int64
		status   string
		added    time.Time
		sessions []session
	}
	addEntry := func(e entry) {
		t.Helper()
		exec(`INSERT INTO library_entries (id, user_id, game_id, status, created_at) VALUES (?, ?, ?, ?, ?)`,
			e.id, e.user, e.game, e.status, day(e.added))
		for i, ps := range e.sessions {
			// minutes is CHECK-constrained to <= 1440, so long playtime is
			// logged as multiple sessions.
			exec(`INSERT INTO play_sessions (id, user_id, entry_id, played_on, minutes) VALUES (?, ?, ?, ?, ?)`,
				psID(e.id, i), e.user, e.id, day(ps.on), ps.minutes)
		}
	}

	// Session dates are relative to now so the 90-day pace window in the debt
	// math (reused by Insights) keeps holding whenever the suite runs.
	ago := func(days int) time.Time { return time.Now().AddDate(0, 0, -days) }

	ps := func(minutes int, daysAgo int) session { return session{minutes, ago(daysAgo)} }

	for _, e := range []entry{
		{"e1", "u1", 1, "backlog", time.Date(2009, 6, 15, 0, 0, 0, 0, time.UTC), nil},
		{"e2", "u1", 2, "backlog", time.Date(2018, 1, 10, 0, 0, 0, 0, time.UTC), nil},
		{"e3", "u1", 3, "played", time.Date(2016, 3, 1, 0, 0, 0, 0, time.UTC), []session{ps(240, 10)}},
		{"e4", "u1", 4, "backlog", time.Date(2019, 4, 5, 0, 0, 0, 0, time.UTC), []session{ps(45, 3)}},
		{"e5", "u1", 5, "played", time.Date(2020, 5, 6, 0, 0, 0, 0, time.UTC), []session{ps(360, 5)}},
		// Added before e1, so it would win "oldest untouched" if the query
		// ignored logged sessions — it must not, because it has one.
		{"e6", "u1", 6, "backlog", time.Date(2008, 1, 1, 0, 0, 0, 0, time.UTC), []session{ps(30, 2)}},
		{"e7", "u1", 7, "dropped", time.Date(2022, 7, 8, 0, 0, 0, 0, time.UTC), []session{ps(90, 1)}},
		{"e8", "u1", 8, "played", time.Date(2010, 8, 9, 0, 0, 0, 0, time.UTC), []session{ps(1000, 30), ps(1000, 29), ps(1000, 28)}},
		{"e10", "u1", 10, "backlog", time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), nil},
		// u2's game predates everything of u1; ownership scoping must keep it
		// out of u1's stats, and u2's sparse library must not produce
		// threshold-starved superlatives.
		{"x1", "u2", 9, "backlog", time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC), nil},
	} {
		addEntry(e)
	}

	return s
}

func psID(entryID string, i int) string {
	return "ps-" + entryID + "-" + string(rune('a'+i))
}

func superlativeByKind(insights models.Insights, kind string) *models.Superlative {
	for i := range insights.Superlatives {
		if insights.Superlatives[i].Kind == kind {
			return &insights.Superlatives[i]
		}
	}
	return nil
}

func TestInsightsFullLibrary(t *testing.T) {
	s := newFixtureStore(t)
	insights, err := s.Insights(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}

	// Owned = 9 games (no wishlist), unplayed = 5 backlog entries, remaining
	// hours = the full time-to-beat of every backlog entry (10+120+200+30+15).
	headline := insights.Headline
	if headline.GamesOwned != 9 {
		t.Errorf("GamesOwned = %d, want 9", headline.GamesOwned)
	}
	if headline.UnplayedGames != 5 {
		t.Errorf("UnplayedGames = %d, want 5", headline.UnplayedGames)
	}
	if headline.HoursRemaining != 375 {
		t.Errorf("HoursRemaining = %v, want 375", headline.HoursRemaining)
	}
	// 3765 session minutes in the trailing window = 62.75h over 90 days
	// ≈ 4.9 h/week → 375/4.9 ≈ 76.5 weeks ≈ 1.5 years.
	if headline.YearsAtCurrentRate == nil {
		t.Fatal("YearsAtCurrentRate = nil, want a projection")
	}
	if want := 1.5; *headline.YearsAtCurrentRate < want-0.05 || *headline.YearsAtCurrentRate > want+0.05 {
		t.Errorf("YearsAtCurrentRate = %v, want ~%v", *headline.YearsAtCurrentRate, want)
	}

	if got, want := len(insights.Superlatives), 5; got != want {
		t.Fatalf("got %d superlatives, want %d: %+v", got, want, insights.Superlatives)
	}

	oldest := superlativeByKind(insights, models.SuperlativeOldestUntouched)
	if oldest == nil {
		t.Fatal("missing oldest_untouched superlative")
	}
	if oldest.Payload.Game == nil || oldest.Payload.Game.Name != "Ancient Relic" {
		t.Errorf("oldest_untouched game = %+v, want Ancient Relic", oldest.Payload.Game)
	}
	if oldest.Payload.EntryID != "e1" {
		t.Errorf("oldest_untouched entry = %q, want e1", oldest.Payload.EntryID)
	}
	if oldest.Payload.AddedOn != "2009-06-15" {
		t.Errorf("oldest_untouched added_on = %q, want 2009-06-15", oldest.Payload.AddedOn)
	}
	if want := "Purchased 2009 · 0 hours logged"; oldest.Label != want {
		t.Errorf("oldest_untouched label = %q, want %q", oldest.Label, want)
	}

	longest := superlativeByKind(insights, models.SuperlativeLongestUnplayed)
	if longest == nil {
		t.Fatal("missing longest_unplayed superlative")
	}
	// Slow Epic is 200h but has a logged session; the winner is untouched.
	if longest.Payload.Game == nil || longest.Payload.Game.Name != "Mega Saga" {
		t.Errorf("longest_unplayed game = %+v, want Mega Saga", longest.Payload.Game)
	}
	if longest.Payload.Hours == nil || *longest.Payload.Hours != 120 {
		t.Errorf("longest_unplayed hours = %v, want 120", longest.Payload.Hours)
	}
	if want := "120h still to play"; longest.Label != want {
		t.Errorf("longest_unplayed label = %q, want %q", longest.Label, want)
	}

	genre := superlativeByKind(insights, models.SuperlativeNeglectedGenre)
	if genre == nil {
		t.Fatal("missing neglected_genre superlative")
	}
	if genre.Payload.Name != "JRPG" {
		t.Errorf("neglected_genre name = %q, want JRPG", genre.Payload.Name)
	}
	if genre.Payload.Owned != 6 || genre.Payload.Played != 1 {
		t.Errorf("neglected_genre owned/played = %d/%d, want 6/1", genre.Payload.Owned, genre.Payload.Played)
	}
	if want := "JRPG — 6 games / 1 played"; genre.Label != want {
		t.Errorf("neglected_genre label = %q, want %q", genre.Label, want)
	}

	platform := superlativeByKind(insights, models.SuperlativeWorstPlatform)
	if platform == nil {
		t.Fatal("missing worst_platform superlative")
	}
	// Steam Deck only carries 2 unplayed games (below the floor of 3); PC
	// carries e1, e4, e6 = 240 hours.
	if platform.Payload.Name != "PC (Microsoft Windows)" {
		t.Errorf("worst_platform name = %q, want PC (Microsoft Windows)", platform.Payload.Name)
	}
	if platform.Payload.BacklogGames != 3 || platform.Payload.BacklogHours != 240 {
		t.Errorf("worst_platform backlog = %d games / %vh, want 3 / 240",
			platform.Payload.BacklogGames, platform.Payload.BacklogHours)
	}
	if want := "PC (Microsoft Windows) — 3 games · 240h owed"; platform.Label != want {
		t.Errorf("worst_platform label = %q, want %q", platform.Label, want)
	}

	year := superlativeByKind(insights, models.SuperlativeNeglectedYear)
	if year == nil {
		t.Fatal("missing neglected_year superlative")
	}
	if year.Payload.Year == nil || *year.Payload.Year != 2011 {
		t.Errorf("neglected_year = %v, want 2011", year.Payload.Year)
	}
	if year.Payload.Owned != 5 || year.Payload.Played != 1 {
		t.Errorf("neglected_year owned/played = %d/%d, want 5/1", year.Payload.Owned, year.Payload.Played)
	}
	if want := "2011 — 5 games / 1 played"; year.Label != want {
		t.Errorf("neglected_year label = %q, want %q", year.Label, want)
	}
}

func TestInsightsSparseLibraryDropsThresholdedStats(t *testing.T) {
	s := newFixtureStore(t)
	insights, err := s.Insights(context.Background(), "u2")
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}

	// One owned game: only the game-backed superlatives can fire.
	if len(insights.Superlatives) != 2 {
		t.Fatalf("got %d superlatives, want 2: %+v", len(insights.Superlatives), insights.Superlatives)
	}
	oldest := superlativeByKind(insights, models.SuperlativeOldestUntouched)
	if oldest == nil || oldest.Payload.Game == nil || oldest.Payload.Game.Name != "Phantom Old" {
		t.Fatalf("oldest_untouched = %+v, want u2's own Phantom Old", oldest)
	}
	longest := superlativeByKind(insights, models.SuperlativeLongestUnplayed)
	if longest == nil || longest.Payload.Hours == nil || *longest.Payload.Hours != 8 {
		t.Fatalf("longest_unplayed = %+v, want Phantom Old at 8h", longest)
	}

	if insights.Headline.GamesOwned != 1 || insights.Headline.UnplayedGames != 1 {
		t.Errorf("headline = %+v, want 1 owned / 1 unplayed", insights.Headline)
	}
	if insights.Headline.HoursRemaining != 8 {
		t.Errorf("HoursRemaining = %v, want 8", insights.Headline.HoursRemaining)
	}
}

func TestInsightsEmptyLibrary(t *testing.T) {
	s := newFixtureStore(t)
	insights, err := s.Insights(context.Background(), "u3")
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if insights.Headline.GamesOwned != 0 || insights.Headline.UnplayedGames != 0 {
		t.Errorf("headline = %+v, want all zeros", insights.Headline)
	}
	if insights.Headline.YearsAtCurrentRate != nil {
		t.Errorf("YearsAtCurrentRate = %v, want nil with no sessions", insights.Headline.YearsAtCurrentRate)
	}
	if len(insights.Superlatives) != 0 {
		t.Errorf("got %d superlatives, want none: %+v", len(insights.Superlatives), insights.Superlatives)
	}
}
