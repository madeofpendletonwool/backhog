package store

import (
	"context"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// TestAddListItemsBatch covers the shelf multi-select: several entries land
// in one call, in the order they were picked, after what the list already
// held, and re-adding a member leaves it where it was.
func TestAddListItemsBatch(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, nil)

	l, err := s.CreateList(ctx, userID, "Souls", "", "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddListItem(ctx, userID, l.ID, entries[205].ID); err != nil {
		t.Fatal(err)
	}

	batch := []string{entries[203].ID, entries[200].ID, entries[205].ID, entries[201].ID}
	if err := s.AddListItems(ctx, userID, l.ID, batch); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	got, err := s.ListEntriesFor(ctx, userID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range got {
		names = append(names, e.Game.Name)
	}
	want := []string{"Shadow Tower", "Elden Ring", "Demon's Souls", "Dark Souls"}
	if len(names) != len(want) {
		t.Fatalf("list = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("list = %v, want %v", names, want)
		}
	}

	// One bad id fails the whole batch, and nothing from it lands.
	if err := s.AddListItems(ctx, userID, l.ID, []string{entries[202].ID, "nope"}); err == nil {
		t.Fatal("batch with an unknown entry succeeded")
	}
	got, _ = s.ListEntriesFor(ctx, userID, l.ID)
	if len(got) != 4 {
		t.Errorf("list has %d entries after a failed batch, want 4", len(got))
	}

	// A smart list still refuses members, batch or not.
	rules := models.RuleSet{Match: "all", Rules: []models.Rule{{Field: "status", Op: "eq", Value: "backlog"}}}
	smart, err := s.CreateList(ctx, userID, "Backlog", "", "smart", &rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddListItems(ctx, userID, smart.ID, batch); err == nil {
		t.Error("batch add into a smart list succeeded")
	}
}

// TestAddProjectItemsBatch is the same gesture aimed at a checklist: ordered,
// idempotent, and all-or-nothing when one entry is from the wrong arena.
func TestAddProjectItemsBatch(t *testing.T) {
	s, userID := newProjectsStore(t)
	ctx := context.Background()
	entries := soulsLibrary(t, s, userID, nil)

	p, err := s.CreateProject(ctx, userID, "Souls run", "", models.ProjectChecklist, models.MediaGame, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	batch := []string{entries[204].ID, entries[200].ID, entries[204].ID, entries[202].ID}
	if err := s.AddProjectItems(ctx, userID, p.ID, batch); err != nil {
		t.Fatalf("batch add: %v", err)
	}
	items, err := s.ProjectItemsFor(ctx, userID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, item := range items {
		names = append(names, item.Entry.Game.Name)
	}
	want := []string{"Sekiro", "Demon's Souls", "Dark Souls II"}
	if len(names) != len(want) {
		t.Fatalf("items = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("items = %v, want %v", names, want)
		}
	}

	// A book checklist refuses a batch of games outright.
	books, err := s.CreateProject(ctx, userID, "Reading", "", models.ProjectChecklist, models.MediaBook, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddProjectItems(ctx, userID, books.ID, batch); err == nil {
		t.Error("games landed in a book checklist")
	}
	bookItems, _ := s.ProjectItemsFor(ctx, userID, books.ID)
	if len(bookItems) != 0 {
		t.Errorf("book checklist has %d items, want 0", len(bookItems))
	}
}
