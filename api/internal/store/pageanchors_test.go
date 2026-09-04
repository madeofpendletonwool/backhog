package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// newPageAnchorStore opens a migrated store with u1 holding one book
// entry anchored to a 700-page printing, plus a second printing of the
// same work for multi-copy cases.
func newPageAnchorStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "pageanchors.db"))
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
	exec(`INSERT INTO books (id, title, authors_json) VALUES ('OL1W', 'Anathem', '[]')`)
	exec(`INSERT INTO book_editions (id, book_id, page_count) VALUES ('OL1M', 'OL1W', 700)`)
	exec(`INSERT INTO book_editions (id, book_id) VALUES ('OL2M', 'OL1W')`)
	exec(`INSERT INTO library_entries (id, user_id, media_type, book_id, edition_id, status)
		VALUES ('e1', 'u1', 'book', 'OL1W', 'OL1M', 'backlog')`)
	return s
}

func anchorCount(t *testing.T, s *Store, ctx context.Context, copyID string) int {
	t.Helper()
	anchors, err := s.PageAnchors(ctx, "u1", "e1", copyID)
	if err != nil {
		t.Fatalf("PageAnchors: %v", err)
	}
	return len(anchors)
}

// TestBorrowedCopyLifecycle walks the library loan: check a printing out
// with a due date, return it, check the same printing out again, buy it.
// Every step keeps the page map — the map is a property of the printing,
// not of the checkout — and only forgetting the copy ever deletes one.
func TestBorrowedCopyLifecycle(t *testing.T) {
	s := newPageAnchorStore(t)
	ctx := context.Background()

	due := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	c, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL1M", "library copy",
		models.CopyAcquisitionBorrowed, &due)
	if err != nil {
		t.Fatalf("CreatePhysicalCopy: %v", err)
	}
	if c.Acquisition != models.CopyAcquisitionBorrowed || c.ReturnedAt != nil {
		t.Fatalf("fresh checkout = %+v, want borrowed and in hand", c)
	}
	if c.DueAt == nil || !c.DueAt.Equal(due) {
		t.Errorf("due date = %v, want %v", c.DueAt, due)
	}

	// A due date on an owned copy, and an unknown acquisition, are
	// client bugs worth naming.
	if _, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL2M", "",
		models.CopyAcquisitionOwned, &due); err == nil {
		t.Error("a due date on an owned copy was accepted")
	}
	if _, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL2M", "",
		"liberated", nil); err == nil {
		t.Error("an unknown acquisition was accepted")
	}
	// Omitting the acquisition entirely is the old register-what-you-own.
	owned, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL2M", "",
		"", nil)
	if err != nil || owned.Acquisition != models.CopyAcquisitionOwned {
		t.Errorf("default acquisition = %q err %v, want owned", owned.Acquisition, err)
	}

	// Pin two pages, then give the book back.
	pin := func(page, offset int) {
		t.Helper()
		if _, err := s.SavePageAnchor(ctx, "u1", "e1", c.ID, models.PageAnchor{
			PrintedPage: page, CharOffset: offset, Source: models.PageAnchorSourceManual,
		}); err != nil {
			t.Fatalf("pin page %d: %v", page, err)
		}
	}
	pin(1, 0)
	pin(240, 180000)
	if n := anchorCount(t, s, ctx, c.ID); n != 2 {
		t.Fatalf("pinned %d anchors, want 2", n)
	}

	returned, err := s.ReturnPhysicalCopy(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("ReturnPhysicalCopy: %v", err)
	}
	if returned.ReturnedAt == nil {
		t.Error("return left returned_at nil")
	}
	if returned.DueAt == nil || !returned.DueAt.Equal(due) {
		t.Errorf("return touched the due date: %v", returned.DueAt)
	}
	if n := anchorCount(t, s, ctx, c.ID); n != 2 {
		t.Errorf("anchors after return = %d, want the map kept", n)
	}

	// The state machine refuses the impossible: returning twice,
	// reopening what is in hand, returning an owned copy.
	if _, err := s.ReturnPhysicalCopy(ctx, "u1", "e1", c.ID); err == nil {
		t.Error("a second return was accepted")
	}
	if _, err := s.ReopenPhysicalCopy(ctx, "u1", "e1", owned.ID, nil); err == nil {
		t.Error("reopening an owned copy was accepted")
	}
	if _, err := s.ReturnPhysicalCopy(ctx, "u1", "e1", owned.ID); err == nil {
		t.Error("returning an owned copy was accepted")
	}
	// Re-registering the returned printing is a conflict — reopening the
	// row is the path that keeps the map.
	if _, err := s.CreatePhysicalCopy(ctx, "u1", "e1", "OL1M", "",
		models.CopyAcquisitionBorrowed, nil); !errors.Is(err, ErrConflict) {
		t.Errorf("re-registering a returned printing = %v, want ErrConflict", err)
	}

	// Check the same printing out again: the same row, the map intact,
	// a fresh due date (here, none — the library never said).
	reopened, err := s.ReopenPhysicalCopy(ctx, "u1", "e1", c.ID, nil)
	if err != nil {
		t.Fatalf("ReopenPhysicalCopy: %v", err)
	}
	if reopened.ReturnedAt != nil || reopened.DueAt != nil {
		t.Errorf("reopened = %+v, want in hand with no due date", reopened)
	}
	if n := anchorCount(t, s, ctx, c.ID); n != 2 {
		t.Errorf("anchors after reopen = %d, want the map kept", n)
	}

	// The deadline is editable while the loan is live, and clearable.
	newDue := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	patched, err := s.UpdatePhysicalCopy(ctx, "u1", "e1", c.ID, CopyUpdate{
		Notes: "renewed", DueAt: &newDue, SetDueAt: true,
	})
	if err != nil {
		t.Fatalf("UpdatePhysicalCopy due date: %v", err)
	}
	if patched.Notes != "renewed" || patched.DueAt == nil || !patched.DueAt.Equal(newDue) {
		t.Errorf("patched = %+v, want renewed notes and the new due date", patched)
	}
	cleared, err := s.UpdatePhysicalCopy(ctx, "u1", "e1", c.ID, CopyUpdate{
		Notes: "no deadline after all", SetDueAt: true,
	})
	if err != nil {
		t.Fatalf("UpdatePhysicalCopy clear due date: %v", err)
	}
	if cleared.DueAt != nil {
		t.Errorf("cleared due date = %v, want nil", cleared.DueAt)
	}
	// Notes alone leave the deadline alone; an owned copy has no due
	// date to edit.
	if _, err := s.UpdatePhysicalCopy(ctx, "u1", "e1", c.ID, CopyUpdate{Notes: "x"}); err != nil {
		t.Fatalf("notes-only update: %v", err)
	}
	if _, err := s.UpdatePhysicalCopy(ctx, "u1", "e1", owned.ID,
		CopyUpdate{Notes: "x", SetDueAt: true}); err == nil {
		t.Error("a due date edit on an owned copy was accepted")
	}

	// Buying the borrowed book: owned, no return state, map kept.
	bought, err := s.OwnPhysicalCopy(ctx, "u1", "e1", c.ID)
	if err != nil {
		t.Fatalf("OwnPhysicalCopy: %v", err)
	}
	if bought.Acquisition != models.CopyAcquisitionOwned ||
		bought.DueAt != nil || bought.ReturnedAt != nil {
		t.Errorf("bought = %+v, want owned with the return state cleared", bought)
	}
	if n := anchorCount(t, s, ctx, c.ID); n != 2 {
		t.Errorf("anchors after buying = %d, want the map kept", n)
	}
	if _, err := s.OwnPhysicalCopy(ctx, "u1", "e1", c.ID); err != nil {
		t.Errorf("owning an already-owned copy: %v, want idempotent", err)
	}

	// A foreign eye changes nothing.
	if _, err := s.ReturnPhysicalCopy(ctx, "u2", "e1", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user return = %v, want ErrNotFound", err)
	}

	// Only forgetting destroys the map.
	listed, err := s.PhysicalCopies(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("PhysicalCopies: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("copies = %d, want the two printings", len(listed))
	}
	var first *models.PhysicalCopy
	for i := range listed {
		if listed[i].EditionID == "OL1M" {
			first = &listed[i]
		}
	}
	if first == nil || first.AnchorCount != 2 {
		t.Fatalf("the borrowed-then-bought printing = %+v, want its 2 anchors", first)
	}
	if err := s.DeletePhysicalCopy(ctx, "u1", "e1", c.ID); err != nil {
		t.Fatalf("DeletePhysicalCopy: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM page_anchors`).Scan(&n); err != nil {
		t.Fatalf("count anchors after forget: %v", err)
	}
	if n != 0 {
		t.Errorf("anchors after forget = %d, want the map gone", n)
	}
}
