package store

import (
	"context"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newTestStore opens an in-memory database with all migrations applied.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(database)
}

func newTestUser(t *testing.T, s *Store, email, username string) string {
	t.Helper()
	u, err := s.CreateUser(context.Background(), email, username, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// detailGame builds a full-lookup metadata.Game, the shape the backfill and
// the lazy detail refresh produce.
func detailGame(id int64, name string, release int64, rating *float64, ttb *int64) metadata.Game {
	g := metadata.Game{ID: id, Name: name, FirstReleaseDate: &release, Rating: rating, TimeToBeatMain: ttb}
	g.Extras = &metadata.GameExtras{
		DLCs:       []metadata.RelatedGame{},
		Expansions: []metadata.RelatedGame{},
	}
	return g
}

func i64(v int64) *int64 { return &v }

// massEffectCluster builds a small franchise:
//
//	100 Mass Effect            (2007, rating 90, 20h) — franchise 55
//	101 Bring Down the Sky     (2008, rating 70,  2h) — DLC of 102
//	102 Mass Effect 2          (2010, rating 94, 25h) — franchise 55
func massEffectCluster() (metadata.Game, []metadata.Game, metadata.Game) {
	parent := detailGame(100, "Mass Effect", 1167609600, ptr(90), i64(3600*20))
	parent.Series = &metadata.GameSeries{
		Franchise: &metadata.SeriesRef{ID: 55, Name: "Mass Effect", Slug: "mass-effect"},
	}
	child := detailGame(101, "Mass Effect Bring Down the Sky", 1204243200, ptr(70), i64(3600*2))
	child.Series = &metadata.GameSeries{
		Franchise: &metadata.SeriesRef{ID: 55, Name: "Mass Effect", Slug: "mass-effect"},
	}
	second := detailGame(102, "Mass Effect 2", 1264003200, ptr(94), i64(3600*25))
	second.Series = &metadata.GameSeries{
		Franchise: &metadata.SeriesRef{ID: 55, Name: "Mass Effect", Slug: "mass-effect"},
	}
	second.Extras.DLCs = []metadata.RelatedGame{{ID: 101, Name: "Mass Effect Bring Down the Sky"}}
	return parent, []metadata.Game{child}, second
}

func applyCluster(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	parent, children, second := massEffectCluster()
	if err := s.ApplySeriesData(ctx, parent, nil); err != nil {
		t.Fatalf("apply parent cluster: %v", err)
	}
	// The backfill walk for 102 fetches its DLC (101) alongside it.
	if err := s.ApplySeriesData(ctx, second, children); err != nil {
		t.Fatalf("apply second game: %v", err)
	}
}

// TestApplySeriesDataIdempotent checks the backfill write path: running the
// same ingestion twice leaves exactly the same series, memberships, kinds and
// release ranks — no duplicates, no churn.
func TestApplySeriesDataIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	run := func() (seriesCount, memberCount int, kinds map[int64]string, orders map[int64]int) {
		applyCluster(t, s)

		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series`).Scan(&seriesCount); err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.QueryContext(ctx, `SELECT game_id, kind, release_order FROM series_games`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		kinds, orders = map[int64]string{}, map[int64]int{}
		for rows.Next() {
			var gameID int64
			var kind string
			var order int
			if err := rows.Scan(&gameID, &kind, &order); err != nil {
				t.Fatal(err)
			}
			memberCount++
			kinds[gameID] = kind
			orders[gameID] = order
		}
		return
	}

	firstSeries, firstMembers, firstKinds, firstOrders := run()
	secondSeries, secondMembers, secondKinds, secondOrders := run()

	if firstSeries != 1 || secondSeries != 1 {
		t.Errorf("series count = %d then %d, want 1 and 1", firstSeries, secondSeries)
	}
	if firstMembers != 3 || secondMembers != 3 {
		t.Errorf("member count = %d then %d, want 3 and 3", firstMembers, secondMembers)
	}
	if firstKinds[101] != "dlc" || secondKinds[101] != "dlc" {
		t.Errorf("child kind = %q then %q, want dlc both times", firstKinds[101], secondKinds[101])
	}
	for id, order := range firstOrders {
		if secondOrders[id] != order {
			t.Errorf("release_order for %d = %d then %d, want stable", id, order, secondOrders[id])
		}
	}
	if firstOrders[100] != 1 || firstOrders[101] != 2 || firstOrders[102] != 3 {
		t.Errorf("release_order = %v, want release-date ranks 100,101,102", firstOrders)
	}
}

// TestSeriesIndexRollup covers the index card: only series with two or more
// owned games appear, and counts, completion and next-up come out right.
func TestSeriesIndexRollup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "a@example.com", "alice")
	applyCluster(t, s)

	// Own the parent (backlog) and the second game (played). The DLC is not
	// owned, so it must not count toward the two-owned-games threshold.
	if _, err := s.AddEntry(ctx, userID, 100, models.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, 102, models.StatusPlayed, nil); err != nil {
		t.Fatal(err)
	}

	index, err := s.SeriesIndex(ctx, userID)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("got %d series, want 1", len(index))
	}
	summary := index[0]
	if summary.Name != "Mass Effect" {
		t.Errorf("name = %q, want Mass Effect", summary.Name)
	}
	if summary.OwnedCount != 2 || summary.PlayedCount != 1 {
		t.Errorf("owned/played = %d/%d, want 2/1", summary.OwnedCount, summary.PlayedCount)
	}
	if summary.Completion != 50 {
		t.Errorf("completion = %v, want 50", summary.Completion)
	}
	if summary.NextGame == nil || summary.NextGame.ID != 100 {
		t.Errorf("next game = %+v, want the unplayed Mass Effect", summary.NextGame)
	}
	if summary.RemainingHours != 20 {
		t.Errorf("remaining = %v, want 20 (only the backlog game's estimate)", summary.RemainingHours)
	}
}

// TestSeriesDetailPlayOrders checks each ordering mode on the same series.
func TestSeriesDetailPlayOrders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "a@example.com", "alice")
	applyCluster(t, s)

	if _, err := s.AddEntry(ctx, userID, 100, models.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, 102, models.StatusPlaying, nil); err != nil {
		t.Fatal(err)
	}

	seriesID, err := s.mainSeriesOfGame(ctx, 100)
	if err != nil || seriesID == "" {
		t.Fatalf("no series: %v", err)
	}

	ids := func(detail models.SeriesDetail) []int64 {
		out := []int64{}
		for _, m := range detail.Members {
			out = append(out, m.Game.ID)
		}
		return out
	}

	detail, err := s.SeriesDetail(ctx, userID, seriesID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if got := ids(detail); !equalIDs(got, []int64{100, 101, 102}) {
		t.Errorf("release order = %v, want [100 101 102]", got)
	}
	if detail.PlayOrder != models.PlayOrderRelease {
		t.Errorf("default play order = %q, want release", detail.PlayOrder)
	}

	// Chronological nests each game's DLC under it: 101 belongs to 102, so it
	// follows 102 rather than sitting at its 2008 release position.
	if err := s.SetSeriesPlayOrder(ctx, userID, seriesID, models.PlayOrderChronological); err != nil {
		t.Fatal(err)
	}
	detail, err = s.SeriesDetail(ctx, userID, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(detail); !equalIDs(got, []int64{100, 102, 101}) {
		t.Errorf("chronological order = %v, want [100 102 101] with the DLC nested under its parent", got)
	}

	// Recommended puts the well-regarded core first; drop the DLC's rating
	// below the floor to see the split.
	if _, err := s.db.ExecContext(ctx, `UPDATE games SET igdb_rating = 55 WHERE id = 101`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeriesPlayOrder(ctx, userID, seriesID, models.PlayOrderRecommended); err != nil {
		t.Fatal(err)
	}
	detail, err = s.SeriesDetail(ctx, userID, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(detail); !equalIDs(got, []int64{100, 102, 101}) {
		t.Errorf("recommended order = %v, want core [100 102] first, then 101", got)
	}

	// Custom seeds from release order, then a move sticks.
	if err := s.SetSeriesPlayOrder(ctx, userID, seriesID, models.PlayOrderCustom); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveSeriesGame(ctx, userID, seriesID, 102, 0, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	detail, err = s.SeriesDetail(ctx, userID, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(detail); !equalIDs(got, []int64{102, 100, 101}) {
		t.Errorf("custom order = %v, want [102 100 101] after the move", got)
	}
	if detail.Members[0].Position == nil || *detail.Members[0].Position >= *detail.Members[1].Position {
		t.Errorf("custom positions not reflected: %v vs %v",
			detail.Members[0].Position, detail.Members[1].Position)
	}
}

// TestNextUpGoodOnes checks that the "just the good ones" journey skips a
// below-threshold game when picking the next stop.
func TestNextUpGoodOnes(t *testing.T) {
	rows := []seriesRow{
		{gameID: 100, name: "Mass Effect", status: models.StatusBacklog, rating: ptr(60)},
		{gameID: 101, name: "Bring Down the Sky", status: models.StatusBacklog, rating: ptr(55)},
		{gameID: 102, name: "Mass Effect 2", status: models.StatusPlaying, rating: ptr(94)},
	}
	sortSeriesRows(rows, models.PlayOrderGoodOnes)

	next := nextUp(rows, models.PlayOrderGoodOnes)
	if next == nil || next.ID != 102 {
		t.Errorf("next up = %+v, want 102 (the only member above the good-ones floor)", next)
	}
}

// TestDLCHoursAndDebt covers the debt integration: DLC of an owed parent is
// counted, DLC of a finished parent is not, and no links at all stays null.
func TestDLCHoursAndDebt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := newTestUser(t, s, "a@example.com", "alice")
	applyCluster(t, s)

	// The DLC belongs to 102; owning 102 in the backlog owes the DLC's hours.
	entry, err := s.AddEntry(ctx, userID, 102, models.StatusBacklog, nil)
	if err != nil {
		t.Fatal(err)
	}

	hours, err := s.DLCHours(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if hours == nil || *hours != 2 {
		t.Errorf("dlc hours = %v, want 2", hours)
	}

	debt, err := s.Debt(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if debt.DLCHours == nil || *debt.DLCHours != 2 {
		t.Errorf("debt DLC hours = %v, want 2", debt.DLCHours)
	}

	// Finishing the parent drops the DLC debt entirely.
	if _, err := s.UpdateEntry(ctx, userID, entry.ID, EntryUpdate{Status: strptr(models.StatusPlayed)}); err != nil {
		t.Fatal(err)
	}
	hours, err = s.DLCHours(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if hours != nil {
		t.Errorf("dlc hours = %v, want nil once the parent is finished", hours)
	}

	// A user with no DLC links at all sees null, not a misleading zero.
	other := newTestUser(t, s, "b@example.com", "bob")
	hours, err = s.DLCHours(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if hours != nil {
		t.Errorf("dlc hours = %v, want nil with no known links", hours)
	}
}

func strptr(s string) *string { return &s }

// TestSeriesForGame covers the two-way link used by the game detail chip.
func TestSeriesForGame(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	applyCluster(t, s)

	series, err := s.SeriesForGame(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Name != "Mass Effect" || series[0].Slug != "mass-effect" {
		t.Errorf("series for game = %+v, want Mass Effect / mass-effect", series)
	}

	series, err = s.SeriesForGame(ctx, 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Errorf("series for unknown game = %+v, want none", series)
	}
}

// TestSeriesBackfillCandidates prefers games that were never enriched or whose
// cache has gone stale.
func TestSeriesBackfillCandidates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	applyCluster(t, s)

	// Freshly enriched games are not candidates again.
	ids, err := s.SeriesBackfillCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("candidates = %v, want none right after enrichment", ids)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE games SET fetched_at = datetime('now', '-40 days') WHERE id = 102`); err != nil {
		t.Fatal(err)
	}
	ids, err = s.SeriesBackfillCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 102 {
		t.Errorf("candidates = %v, want only the stale game 102", ids)
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
