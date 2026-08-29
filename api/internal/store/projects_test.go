package store

import (
	"context"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newProjectsStore opens a migrated in-memory database with one user and the
// "Finish the Souls games" fixture library: six games totalling 400 estimated
// hours, exactly one of them played.
func newProjectsStore(t *testing.T) (*Store, string) {
	t.Helper()
	database, err := db.Open(":memory:")
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
	exec(`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'u1@example.com', 'u1', 'x')`)
	exec(`INSERT INTO platforms (id, name) VALUES (38, 'PlayStation Portable')`)

	// hours: 20 + 40 + 45 + 60 + 100 + 135 = 400 estimated for the set.
	souls := []struct {
		id    int64
		name  string
		hours int64
		ps2   bool
	}{
		{200, "Demon's Souls", 20, true},
		{201, "Dark Souls", 40, false},
		{202, "Dark Souls II", 45, false},
		{203, "Elden Ring", 60, false},
		{204, "Sekiro", 100, false},
		{205, "Shadow Tower", 135, true},
	}
	for _, g := range souls {
		exec(`INSERT INTO games (id, name, time_to_beat_main) VALUES (?, ?, ?)`, g.id, g.name, g.hours*3600)
		if g.ps2 {
			exec(`INSERT INTO game_platforms (game_id, platform_id) VALUES (?, 38)`, g.id)
		}
	}
	return s, "u1"
}

// soulsLibrary seeds one entry per fixture game; played names the games that
// are already finished. Returns the entries keyed by game id.
func soulsLibrary(t *testing.T, s *Store, userID string, played map[int64]bool) map[int64]models.Entry {
	t.Helper()
	ctx := context.Background()
	entries := map[int64]models.Entry{}
	for _, id := range []int64{200, 201, 202, 203, 204, 205} {
		status := models.StatusBacklog
		if played[id] {
			status = models.StatusPlayed
		}
		e, err := s.AddEntry(ctx, userID, id, status, nil)
		if err != nil {
			t.Fatalf("add entry %d: %v", id, err)
		}
		entries[id] = e
	}
	return entries
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
func strPtr(v string) *string { return &v }

func mustCreateChecklist(t *testing.T, s *Store, userID string, entries map[int64]models.Entry) models.Project {
	t.Helper()
	ctx := context.Background()
	p, err := s.CreateProject(ctx, userID, "Finish the Souls games", "Every one, start to finish", models.ProjectChecklist, nil, nil)
	if err != nil {
		t.Fatalf("create checklist: %v", err)
	}
	for _, id := range []int64{200, 201, 202, 203, 204, 205} {
		if err := s.AddProjectItem(ctx, userID, p.ID, entries[id].ID); err != nil {
			t.Fatalf("add item %d: %v", id, err)
		}
	}
	return p
}

// TestChecklistProgressMath covers the headline numbers: "1/6 · 20/400
// estimated hours", percent, and the remaining-hours floor.
func TestChecklistProgressMath(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, map[int64]bool{200: true})
	p := mustCreateChecklist(t, s, userID, entries)

	got, err := s.GetProject(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	pr := got.Progress
	if pr.TargetCount != 6 || pr.CompletedCount != 1 {
		t.Errorf("counts = %d/%d, want 1/6", pr.CompletedCount, pr.TargetCount)
	}
	if pr.EstHoursTotal != 400 || pr.EstHoursDone != 20 || pr.EstHoursRemaining != 380 {
		t.Errorf("hours = done %v / total %v / remaining %v, want 20/400/380",
			pr.EstHoursDone, pr.EstHoursTotal, pr.EstHoursRemaining)
	}
	if pr.Percent != 16.7 {
		t.Errorf("percent = %v, want 16.7", pr.Percent)
	}
	if got.CompletedAt != nil {
		t.Errorf("completed_at set with 1/6 done")
	}
}

// TestChecklistAutoStamping covers the completed_at lifecycle: stamped when
// the target is met, sticky when progress later falls back, and manual close
// versus reopen through the update path.
func TestChecklistAutoStamping(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, map[int64]bool{200: true})
	p := mustCreateChecklist(t, s, userID, entries)

	// Finish the remaining five; the next read stamps completion.
	for _, id := range []int64{201, 202, 203, 204, 205} {
		if _, _, err := s.UpdateEntry(ctx, userID, entries[id].ID,
			EntryUpdate{Status: strPtr(models.StatusPlayed)}); err != nil {
			t.Fatalf("play %d: %v", id, err)
		}
	}

	got, err := s.GetProject(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedAt == nil {
		t.Fatal("completed_at not stamped after the target was met")
	}
	if got.Progress.CompletedCount != 6 || got.Progress.Percent != 100 {
		t.Errorf("progress = %d/%d (%v%%), want 6/6 (100)", got.Progress.CompletedCount, got.Progress.TargetCount, got.Progress.Percent)
	}

	// The stamp is sticky: un-finishing one game does not reopen the project.
	if _, _, err := s.UpdateEntry(ctx, userID, entries[205].ID,
		EntryUpdate{Status: strPtr(models.StatusBacklog)}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProject(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedAt == nil {
		t.Error("completed_at was cleared by a later status change")
	}
	if got.Progress.CompletedCount != 5 {
		t.Errorf("completed_count = %d, want 5 after un-finishing one", got.Progress.CompletedCount)
	}

	// Manual reopen clears the stamp, and manual close sets it back.
	reopened, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{Completed: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.CompletedAt != nil {
		t.Error("manual reopen did not clear completed_at")
	}
	closed, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{Completed: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if closed.CompletedAt == nil {
		t.Error("manual close did not set completed_at")
	}
}

// TestChecklistManualOverride covers the per-item done override: forced done,
// forced not-done (beating an already-played status), and clearing back to
// status-derived completion.
func TestChecklistManualOverride(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, map[int64]bool{200: true})
	p := mustCreateChecklist(t, s, userID, entries)

	if err := s.SetProjectItemDone(ctx, userID, p.ID, entries[204].ID, boolPtr(true)); err != nil {
		t.Fatalf("force done: %v", err)
	}
	got, _ := s.GetProject(ctx, userID, p.ID)
	if got.Progress.CompletedCount != 2 {
		t.Errorf("completed_count = %d after forcing one done, want 2", got.Progress.CompletedCount)
	}

	// The played game can be manually excluded from this project.
	if err := s.SetProjectItemDone(ctx, userID, p.ID, entries[200].ID, boolPtr(false)); err != nil {
		t.Fatalf("force not-done: %v", err)
	}
	got, _ = s.GetProject(ctx, userID, p.ID)
	if got.Progress.CompletedCount != 1 {
		t.Errorf("completed_count = %d after excluding the played game, want 1", got.Progress.CompletedCount)
	}

	// Null returns the item to status-derived completion.
	if err := s.SetProjectItemDone(ctx, userID, p.ID, entries[200].ID, nil); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	got, _ = s.GetProject(ctx, userID, p.ID)
	if got.Progress.CompletedCount != 2 {
		t.Errorf("completed_count = %d after clearing the override, want 2", got.Progress.CompletedCount)
	}

	// Overrides alone can complete the project.
	for _, id := range []int64{201, 202, 203, 205} {
		if err := s.SetProjectItemDone(ctx, userID, p.ID, entries[id].ID, boolPtr(true)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetProject(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedAt == nil || got.Progress.CompletedCount != 6 {
		t.Errorf("overrides did not complete the project: %+v", got.Progress)
	}

	items, err := s.ProjectItemsFor(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	for _, item := range items {
		// Demon's Souls had its override cleared back to status-derived;
		// every other member carries an explicit forced-done override.
		if item.Entry.Game.Name == "Demon's Souls" {
			if item.Done != nil {
				t.Errorf("Demon's Souls: done override = %v, want nil (cleared)", item.Done)
			}
			continue
		}
		if item.Done == nil || !*item.Done {
			t.Errorf("item %s: done override = %v, want true", item.Entry.Game.Name, item.Done)
		}
	}
}

// TestCountGoalProgress covers "10 games before buying another": played games
// count, the wishlist never enters the pool, and a target already exceeded at
// creation completes immediately.
func TestCountGoalProgress(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()

	// 3 played, 3 backlog; a 7th wishlist game must stay out of the pool.
	soulsLibrary(t, s, userID, map[int64]bool{200: true, 201: true, 202: true})
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO games (id, name, time_to_beat_main) VALUES (206, 'Wishlisted', 3600)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, 206, models.StatusWishlist, nil); err != nil {
		t.Fatal(err)
	}

	p, err := s.CreateProject(ctx, userID, "10 before buying another", "", models.ProjectCountGoal, intPtr(5), nil)
	if err != nil {
		t.Fatalf("create count goal: %v", err)
	}
	got, _ := s.GetProject(ctx, userID, p.ID)
	if got.Progress.CompletedCount != 3 || got.Progress.TargetCount != 5 {
		t.Errorf("counts = %d/%d, want 3/5", got.Progress.CompletedCount, got.Progress.TargetCount)
	}
	if got.Progress.Percent != 60 {
		t.Errorf("percent = %v, want 60", got.Progress.Percent)
	}
	if got.CompletedAt != nil {
		t.Error("count goal stamped complete at 3/5")
	}

	// A target already met completes on first read, capped at 100%.
	early, err := s.CreateProject(ctx, userID, "2 quick wins", "", models.ProjectCountGoal, intPtr(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProject(ctx, userID, early.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedAt == nil {
		t.Error("met count goal was not stamped complete")
	}
	if got.Progress.Percent != 100 {
		t.Errorf("percent = %v past target, want capped 100", got.Progress.Percent)
	}
	if got.Progress.CompletedCount != 3 {
		t.Errorf("completed_count = %d, want the real played count 3", got.Progress.CompletedCount)
	}
}

// TestCountGoalValidation: a count goal without a positive target is
// rejected at create and at update.
func TestCountGoalValidation(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()

	if _, err := s.CreateProject(ctx, userID, "no target", "", models.ProjectCountGoal, nil, nil); err == nil {
		t.Error("count goal without target_count was allowed")
	}
	if _, err := s.CreateProject(ctx, userID, "zero target", "", models.ProjectCountGoal, intPtr(0), nil); err == nil {
		t.Error("count goal with target_count 0 was allowed")
	}

	p, err := s.CreateProject(ctx, userID, "fine", "", models.ProjectCountGoal, intPtr(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{ClearTarget: true}); err == nil {
		t.Error("clearing the target of a count goal was allowed")
	}
	if _, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{TargetCount: intPtr(-1)}); err == nil {
		t.Error("negative target_count was allowed")
	}
}

// TestRuleGoalEvaluation covers rule goals end to end: the live match pool,
// played-matches counting as done, explicit targets vs "the whole pool",
// retargeting by replacing the rules, and validation at write time.
func TestRuleGoalEvaluation(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	// 200 Demon's Souls (20h, PS2, played), 205 Shadow Tower (135h, PS2),
	// 201 Dark Souls (40h, played), 202 Dark Souls II (45h), others backlog.
	soulsLibrary(t, s, userID, map[int64]bool{200: true, 201: true})

	ps2Rule := models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "platform", Op: "in", Value: []any{"PlayStation Portable"}}},
	}

	// No explicit target: the goal is the whole pool (2 PS2 games, 155h).
	p, err := s.CreateProject(ctx, userID, "Play my PS2 backlog", "", models.ProjectRuleGoal, nil, &ps2Rule)
	if err != nil {
		t.Fatalf("create rule goal: %v", err)
	}
	got, _ := s.GetProject(ctx, userID, p.ID)
	if got.Progress.TargetCount != 2 || got.Progress.CompletedCount != 1 {
		t.Errorf("counts = %d/%d, want 1/2 (Demon's Souls played)", got.Progress.CompletedCount, got.Progress.TargetCount)
	}
	if got.Progress.EstHoursTotal != 155 || got.Progress.EstHoursDone != 20 {
		t.Errorf("hours = %v/%v, want 20/155", got.Progress.EstHoursDone, got.Progress.EstHoursTotal)
	}
	if got.CompletedAt != nil {
		t.Error("rule goal stamped complete at 1/2")
	}

	// Explicit target: first N matches count as the goal.
	targeted, err := s.CreateProject(ctx, userID, "One PS2 game down", "", models.ProjectRuleGoal, intPtr(1), &ps2Rule)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProject(ctx, userID, targeted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedAt == nil || got.Progress.Percent != 100 {
		t.Errorf("target of 1 with 1 played match not complete: %+v", got.Progress)
	}

	// The item view is the live match pool, in rule order.
	items, err := s.ProjectItemsFor(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("match pool = %d items, want 2", len(items))
	}
	for _, item := range items {
		if item.Done != nil {
			t.Error("rule goal items must not carry a done override")
		}
	}

	// Retarget by replacing the rules; the pool follows immediately.
	under50 := models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "hours_to_beat", Op: "lt", Value: 50.0}},
	}
	retargeted, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{Rules: &under50})
	if err != nil {
		t.Fatalf("retarget: %v", err)
	}
	if retargeted.Progress.TargetCount != 3 || retargeted.Progress.CompletedCount != 2 {
		t.Errorf("after retarget counts = %d/%d, want 2/3 (games under 50h)",
			retargeted.Progress.CompletedCount, retargeted.Progress.TargetCount)
	}

	// Invalid rule sets are rejected at write time, not at read time.
	bad := models.RuleSet{Match: "all", Rules: []models.Rule{{Field: "nope", Op: "eq", Value: 1}}}
	if _, err := s.CreateProject(ctx, userID, "bad", "", models.ProjectRuleGoal, nil, &bad); err == nil {
		t.Error("invalid rule set was allowed at create")
	}
	if _, err := s.UpdateProject(ctx, userID, p.ID, ProjectUpdate{Rules: &bad}); err == nil {
		t.Error("invalid rule set was allowed at update")
	}

	// Only rule goals carry rules.
	checklist, err := s.CreateProject(ctx, userID, "curated", "", models.ProjectChecklist, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(ctx, userID, checklist.ID, ProjectUpdate{Rules: &ps2Rule}); err == nil {
		t.Error("rules accepted for a checklist project")
	}
}

// TestProjectKindValidation covers create-time kind and shape enforcement.
func TestProjectKindValidation(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()

	if _, err := s.CreateProject(ctx, userID, "weird", "", "bucket_list", nil, nil); err == nil {
		t.Error("unknown kind was allowed")
	}
	if _, err := s.CreateProject(ctx, userID, "  ", "", models.ProjectChecklist, nil, nil); err == nil {
		t.Error("blank name was allowed")
	}
	rules := models.RuleSet{Match: "all", Rules: []models.Rule{{Field: "status", Op: "eq", Value: models.StatusBacklog}}}
	if _, err := s.CreateProject(ctx, userID, "ruleless", "", models.ProjectRuleGoal, nil, nil); err == nil {
		t.Error("rule goal without rules was allowed")
	}
	// A checklist silently drops target and rules: the member list is the target.
	checklist, err := s.CreateProject(ctx, userID, "curated", "", models.ProjectChecklist, intPtr(4), &rules)
	if err != nil {
		t.Fatal(err)
	}
	if checklist.TargetCount != nil || checklist.Rules != nil {
		t.Error("checklist kept a target_count or rules")
	}
}

// TestProjectItemMembership covers ordering, duplicate adds, reorder, removal,
// and that goal projects refuse membership entirely.
func TestProjectItemMembership(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, nil)
	p := mustCreateChecklist(t, s, userID, entries)

	// Adding an existing member again is a no-op, not an error.
	if err := s.AddProjectItem(ctx, userID, p.ID, entries[200].ID); err != nil {
		t.Fatalf("duplicate add: %v", err)
	}
	items, err := s.ProjectItemsFor(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("members = %d after duplicate add, want 6", len(items))
	}

	// Move the first game between the second and the third.
	if err := s.MoveProjectItem(ctx, userID, p.ID, entries[200].ID, entries[201].ID, entries[202].ID); err != nil {
		t.Fatalf("move: %v", err)
	}
	items, err = s.ProjectItemsFor(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	for _, item := range items {
		order = append(order, item.Entry.Game.Name)
	}
	want := []string{"Dark Souls", "Demon's Souls", "Dark Souls II", "Elden Ring", "Sekiro", "Shadow Tower"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}

	// Membership lookups only see the caller's projects.
	ids, err := s.ProjectIDsForEntry(ctx, userID, entries[200].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != p.ID {
		t.Errorf("entry projects = %v, want [%s]", ids, p.ID)
	}

	if err := s.RemoveProjectItem(ctx, userID, p.ID, entries[204].ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ := s.GetProject(ctx, userID, p.ID)
	if got.Progress.TargetCount != 5 {
		t.Errorf("target = %d after removal, want 5", got.Progress.TargetCount)
	}

	// Goal projects have no members to manage.
	goal, err := s.CreateProject(ctx, userID, "10 games", "", models.ProjectCountGoal, intPtr(10), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddProjectItem(ctx, userID, goal.ID, entries[200].ID); err == nil {
		t.Error("membership add accepted for a count goal")
	}
	goalItems, err := s.ProjectItemsFor(ctx, userID, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(goalItems) != 0 {
		t.Errorf("count goal items = %d, want 0", len(goalItems))
	}
}

// TestProjectIsolation: another user's projects, and their members, are
// invisible — every path returns ErrNotFound rather than leaking existence.
func TestProjectIsolation(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	other, err := s.CreateUser(ctx, "other@example.com", "other", "hash")
	if err != nil {
		t.Fatal(err)
	}

	entries := soulsLibrary(t, s, userID, nil)
	p := mustCreateChecklist(t, s, userID, entries)

	if _, err := s.GetProject(ctx, other.ID, p.ID); err != ErrNotFound {
		t.Errorf("cross-user GetProject = %v, want ErrNotFound", err)
	}
	all, err := s.GetProjects(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("cross-user GetProjects = %d projects, want 0", len(all))
	}
	if err := s.AddProjectItem(ctx, other.ID, p.ID, entries[200].ID); err != ErrNotFound {
		t.Errorf("cross-user AddProjectItem = %v, want ErrNotFound", err)
	}
	if err := s.DeleteProject(ctx, other.ID, p.ID); err != ErrNotFound {
		t.Errorf("cross-user DeleteProject = %v, want ErrNotFound", err)
	}
	// The owner still sees it.
	if _, err := s.GetProject(ctx, userID, p.ID); err != nil {
		t.Errorf("owner lost sight of the project: %v", err)
	}
}
