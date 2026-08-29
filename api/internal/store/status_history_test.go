package store

import (
	"context"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/achievements"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// historyRows reads an entry's status history as (from, to) pairs, oldest
// first.
func historyRows(t *testing.T, s *Store, entryID string) [][2]string {
	t.Helper()
	rows, err := s.DB().QueryContext(context.Background(),
		`SELECT from_status, to_status FROM entry_status_history
		 WHERE entry_id = ? ORDER BY changed_at, rowid`, entryID)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			t.Fatalf("scan history: %v", err)
		}
		out = append(out, pair)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("history rows: %v", err)
	}
	return out
}

// TestUpdateEntryWritesStatusHistory drives UpdateEntry through a full cycle
// of transitions and asserts exactly one row lands per change — and none when
// the status is set to its current value.
func TestUpdateEntryWritesStatusHistory(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	tests := []struct {
		from, to string
	}{
		{models.StatusBacklog, models.StatusPlaying},
		{models.StatusPlaying, models.StatusPlayed},
		{models.StatusPlayed, models.StatusDropped},
		{models.StatusDropped, models.StatusBacklog},
		{models.StatusBacklog, models.StatusPlayed},
		{models.StatusPlayed, models.StatusPlaying},
		{models.StatusPlaying, models.StatusWishlist},
		{models.StatusWishlist, models.StatusIgnored},
		{models.StatusIgnored, models.StatusBacklog},
	}

	for _, tt := range tests {
		// a6 starts in backlog; set it to tt.from, then flip to tt.to.
		if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(tt.from)}); err != nil {
			t.Fatalf("set %s: %v", tt.from, err)
		}
		before := len(historyRows(t, s, "a6"))
		if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(tt.to)}); err != nil {
			t.Fatalf("%s → %s: %v", tt.from, tt.to, err)
		}
		rows := historyRows(t, s, "a6")
		if len(rows) != before+1 {
			t.Fatalf("%s → %s wrote %d rows (had %d), want exactly 1", tt.from, tt.to, len(rows)-before, before)
		}
		got := rows[len(rows)-1]
		if got[0] != tt.from || got[1] != tt.to {
			t.Errorf("%s → %s recorded [%s → %s]", tt.from, tt.to, got[0], got[1])
		}
	}

	// Re-asserting the current status is not a transition.
	before := len(historyRows(t, s, "a6"))
	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusBacklog)}); err != nil {
		t.Fatal(err)
	}
	// Notes-only updates neither.
	notes := "just browsing"
	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Notes: &notes}); err != nil {
		t.Fatal(err)
	}
	if rows := historyRows(t, s, "a6"); len(rows) != before {
		t.Errorf("no-op updates wrote %d rows, want 0", len(rows)-before)
	}
}

// TestAddSessionWritesStatusHistory covers the auto backlog→playing flip:
// logging time on an unstarted (or wishlisted) game writes the transition,
// while a session on an already-started game writes nothing.
func TestAddSessionWritesStatusHistory(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	today := time.Now().Format("2006-01-02")

	// a6 sits in the backlog: a session starts it.
	if _, _, err := s.AddSession(ctx, "u1", "a6", today, 30, ""); err != nil {
		t.Fatalf("AddSession backlog: %v", err)
	}
	if rows := historyRows(t, s, "a6"); len(rows) != 1 || rows[0] != [2]string{models.StatusBacklog, models.StatusPlaying} {
		t.Fatalf("backlog session history = %v, want [backlog → playing]", rows)
	}

	// A second session on the now-playing entry: no new transition.
	if _, _, err := s.AddSession(ctx, "u1", "a6", today, 30, ""); err != nil {
		t.Fatalf("AddSession playing: %v", err)
	}
	if rows := historyRows(t, s, "a6"); len(rows) != 1 {
		t.Fatalf("playing session added %d rows, want 0", len(rows)-1)
	}

	// Wishlist flips too — logging time on a wishlisted game means playing.
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE library_entries SET status = 'wishlist' WHERE id = 'a6'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSession(ctx, "u1", "a6", today, 30, ""); err != nil {
		t.Fatalf("AddSession wishlist: %v", err)
	}
	if rows := historyRows(t, s, "a6"); len(rows) != 2 || rows[1] != [2]string{models.StatusWishlist, models.StatusPlaying} {
		t.Fatalf("wishlist session history = %v, want [... wishlist → playing]", rows)
	}
}

// TestResumedEventFires exercises the comeback wiring end to end: a drop
// (which stamps history) followed by a resume to playing, then to backlog.
// No achievement keys on EventResumed yet — the assertions are on the
// history trail the predicates will read.
func TestResumedEventFires(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusDropped)}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// Resuming a dropped game by playing it again.
	if _, unlocks, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusPlaying)}); err != nil {
		t.Fatalf("resume to playing: %v", err)
	} else if len(unlocks) != 0 {
		t.Errorf("resume to playing unlocked %v, want nothing yet", unlocks)
	}
	// Dropping again, then re-shelving to backlog: also a resume.
	if _, _, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusDropped)}); err != nil {
		t.Fatalf("re-drop: %v", err)
	}
	if _, unlocks, err := s.UpdateEntry(ctx, "u1", "a6", EntryUpdate{Status: strptr(models.StatusBacklog)}); err != nil {
		t.Fatalf("resume to backlog: %v", err)
	} else if len(unlocks) != 0 {
		t.Errorf("resume to backlog unlocked %v, want nothing yet", unlocks)
	}

	want := [][2]string{
		{models.StatusBacklog, models.StatusDropped},
		{models.StatusDropped, models.StatusPlaying},
		{models.StatusPlaying, models.StatusDropped},
		{models.StatusDropped, models.StatusBacklog},
	}
	if rows := historyRows(t, s, "a6"); len(rows) != len(want) {
		t.Fatalf("history = %v, want %v", rows, want)
	} else {
		for i, pair := range want {
			if rows[i] != pair {
				t.Errorf("history[%d] = %v, want %v", i, rows[i], pair)
			}
		}
	}
}

// TestLastDroppedAt verifies the graceful degradation: the drop time comes
// from status history when it exists, falls back to finished_at for
// pre-feature drops, and is nil when the game was never dropped.
func TestLastDroppedAt(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	database := s.DB()
	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO games (id, name) VALUES (21, 'Drop A'), (22, 'Drop B'), (23, 'Drop C')`)
	now := time.Now().UTC()
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

	// Pre-feature drop: finished_at set, no history rows.
	droppedAt := now.AddDate(0, 0, -90)
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at, finished_at)
		VALUES ('d1', 'u1', 21, 'dropped', ?, ?)`, stamp(now.AddDate(0, 0, -400)), stamp(droppedAt))
	// Post-feature drop: history row, finished_at already wiped by the resume.
	resumedAt := now.AddDate(0, 0, -10)
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at)
		VALUES ('d2', 'u1', 22, 'playing', ?)`, stamp(now.AddDate(0, 0, -300)))
	seed(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
		VALUES ('h1', 'd2', 'u1', 'backlog', 'dropped', ?)`, stamp(now.AddDate(0, 0, -60)))
	seed(`INSERT INTO entry_status_history (id, entry_id, user_id, from_status, to_status, changed_at)
		VALUES ('h2', 'd2', 'u1', 'dropped', 'playing', ?)`, stamp(resumedAt))
	// Never dropped, no fallback offered.
	seed(`INSERT INTO library_entries (id, user_id, game_id, status, created_at)
		VALUES ('d3', 'u1', 23, 'playing', ?)`, stamp(now.AddDate(0, 0, -30)))

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	fallback := droppedAt
	tests := []struct {
		name    string
		entryID string
		fall    *time.Time
		want    time.Time
		nilWant bool
	}{
		{name: "history wins over fallback", entryID: "d2", fall: &fallback, want: now.AddDate(0, 0, -60)},
		{name: "pre-feature falls back to finished_at", entryID: "d1", fall: &fallback, want: droppedAt},
		{name: "never dropped with no fallback", entryID: "d3", fall: nil, nilWant: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lastDroppedAtTx(ctx, tx, tt.entryID, tt.fall)
			if err != nil {
				t.Fatalf("lastDroppedAtTx: %v", err)
			}
			if tt.nilWant {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %v", tt.want)
			}
			// changed_at is stored at second granularity.
			if got.Unix() != tt.want.Unix() {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSnapshotAggregates checks the richer event snapshot against the shared
// fixture: u1 owns six entries — four finished (a1 oldest), one dropped
// pre-feature, one in the backlog — with a5..a6 the oldest stretch.
func TestSnapshotAggregates(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	// Give game 6 a platform and a series so those snapshot fields fill.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO platforms (id, name) VALUES (51, 'Switch')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE library_entries SET platform_id = 51 WHERE id = 'a6'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO series (id, name) VALUES ('sr9', 'Saga')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO series_games (series_id, game_id) VALUES ('sr9', 6), ('sr9', 3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE games SET first_release_date = ? WHERE id = 6`, time.Date(2017, 3, 3, 0, 0, 0, 0, time.UTC).Unix()); err != nil {
		t.Fatal(err)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var e achievements.Entry
	e.ID = "a6"
	e.CreatedAt = time.Now().UTC().AddDate(0, 0, -2200)
	e.At = time.Now().UTC()
	if err := snapshotAggregatesTx(ctx, tx, "u1", &e); err != nil {
		t.Fatalf("snapshotAggregatesTx: %v", err)
	}

	if e.PlayedCount != 4 {
		t.Errorf("PlayedCount = %d, want 4", e.PlayedCount)
	}
	if e.DroppedCount != 1 {
		t.Errorf("DroppedCount = %d, want 1 (a5, pre-feature)", e.DroppedCount)
	}
	if e.UnplayedCount != 1 {
		t.Errorf("UnplayedCount = %d, want 1 (a6)", e.UnplayedCount)
	}
	if e.CreatedAtRank != 2 {
		t.Errorf("CreatedAtRank = %d, want 2 (a1 is older)", e.CreatedAtRank)
	}
	if e.IsOldestOwned {
		t.Error("a6 should not be the oldest owned entry")
	}
	if len(e.SeriesIDs) != 1 || e.SeriesIDs[0] != "sr9" {
		t.Errorf("SeriesIDs = %v, want [sr9]", e.SeriesIDs)
	}
	if e.PlatformID == nil || *e.PlatformID != 51 {
		t.Errorf("PlatformID = %v, want 51", e.PlatformID)
	}
	if e.FirstReleaseDate == nil || *e.FirstReleaseDate != time.Date(2017, 3, 3, 0, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("FirstReleaseDate = %v, want 2017-03-03", e.FirstReleaseDate)
	}
	if e.FinishYear != e.At.Year() || e.FinishMonth != int(e.At.Month()) {
		t.Errorf("finish month/year = %d/%d, want %d/%d", e.FinishMonth, e.FinishYear, int(e.At.Month()), e.At.Year())
	}

	// The oldest entry generalizes: rank 1 is IsOldestOwned.
	var oldest achievements.Entry
	oldest.ID = "a1"
	oldest.CreatedAt = time.Now().UTC().AddDate(0, 0, -8*365)
	oldest.At = time.Now().UTC()
	if err := snapshotAggregatesTx(ctx, tx, "u1", &oldest); err != nil {
		t.Fatalf("snapshotAggregatesTx oldest: %v", err)
	}
	if oldest.CreatedAtRank != 1 || !oldest.IsOldestOwned {
		t.Errorf("a1 rank = %d, IsOldestOwned = %v, want 1/true", oldest.CreatedAtRank, oldest.IsOldestOwned)
	}
}

// TestLazyTimeWindowEvaluation drives the lazy mechanism with synthetic
// definitions (defs are a parameter precisely so tests don't touch the real
// catalogue): a predicate fires once MAX(created_at) is old enough, unlocks
// with no triggering entry, stays silent for users with no entries, and
// doesn't double-unlock.
func TestLazyTimeWindowEvaluation(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()
	database := s.DB()

	restraint := achievements.Definition{
		Achievement: models.Achievement{ID: "test_restraint", Title: "Restraint", Tier: models.TierSilver},
		TimePredicate: func(ts achievements.TimeSnapshot) bool {
			return ts.Now.Sub(ts.LastAcquiredAt) >= 30*24*time.Hour
		},
	}
	forever := achievements.Definition{
		Achievement: models.Achievement{ID: "test_discipline", Title: "Discipline", Tier: models.TierGold},
		TimePredicate: func(ts achievements.TimeSnapshot) bool {
			return ts.Now.Sub(ts.LastAcquiredAt) >= 365*24*time.Hour
		},
	}

	run := func(user string, defs ...achievements.Definition) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := s.evaluateTimeWindowAchievementsTx(ctx, tx, user, defs); err != nil {
			return err
		}
		return tx.Commit()
	}

	// u3 owns exactly one entry, added 40 days ago: restraint yes,
	// discipline no — the window is measured from MAX(created_at).
	if _, err := database.ExecContext(ctx,
		`INSERT INTO users (id, email, username, password_hash) VALUES
			('u3', 'u3@example.com', 'u3', 'x')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO library_entries (id, user_id, game_id, status, created_at)
		 VALUES ('w1', 'u3', 1, 'backlog', ?)`,
		now.AddDate(0, 0, -40).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if err := run("u3", restraint, forever); err != nil {
		t.Fatalf("lazy eval: %v", err)
	}
	var n int
	var entryID *string
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*), (SELECT entry_id FROM achievement_unlocks WHERE user_id = 'u3' AND achievement_id = 'test_restraint')
		 FROM achievement_unlocks WHERE user_id = 'u3' AND achievement_id = 'test_restraint'`).Scan(&n, &entryID); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("restraint unlock rows = %d, want 1", n)
	}
	if entryID != nil {
		t.Errorf("restraint entry_id = %v, want NULL (no triggering game)", *entryID)
	}

	// Idempotent: re-running fills no gaps.
	if err := run("u3", restraint, forever); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u3'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("second lazy eval left %d rows, want 1", n)
	}

	// u2 owns nothing: no last-acquired timestamp, nothing unlocks.
	if err := run("u2", restraint); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM achievement_unlocks WHERE user_id = 'u2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("empty user unlocked %d achievements, want 0", n)
	}
}

// TestAchievementsGalleryShape pins the gallery contract: every entry carries
// a valid tier, and — until any hidden achievement ships — locked entries
// show their real identity.
func TestAchievementsGalleryShape(t *testing.T) {
	s := newAchievementsStore(t)
	ctx := context.Background()

	statuses, err := s.Achievements(ctx, "u1")
	if err != nil {
		t.Fatalf("Achievements: %v", err)
	}
	if len(statuses) != len(achievements.Catalogue) {
		t.Fatalf("gallery has %d entries, catalogue has %d", len(statuses), len(achievements.Catalogue))
	}
	for _, st := range statuses {
		if !models.ValidTier(st.Tier) {
			t.Errorf("%s has invalid tier %q", st.ID, st.Tier)
		}
		def := achievements.ByID(st.ID)
		if def == nil {
			t.Fatalf("gallery id %q not in catalogue", st.ID)
		}
		if st.UnlockedAt == nil && (st.Title != def.Title || st.Description != def.Description) {
			t.Errorf("locked entry %s is masked (%q), want real identity", st.ID, st.Title)
		}
	}
}
