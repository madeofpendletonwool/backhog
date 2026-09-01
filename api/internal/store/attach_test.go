package store

import (
	"context"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedAttachFixture inventories two audiobook directories and one epub under
// one root, owned by one user whose library holds two books.
func seedAttachFixture(t *testing.T, s *Store) (userID string, book1, book2 string, audioIDs []int64, epubID int64) {
	t.Helper()
	ctx := context.Background()
	userID = newTestUser(t, s, "attach@example.com", "attacher")

	for _, b := range []struct{ id, title string }{
		{"OL1W", "Anathem"},
		{"OL2W", "Dune"},
	} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO books (id, title) VALUES (?, ?)`, b.id, b.title); err != nil {
			t.Fatalf("seed book %s: %v", b.id, err)
		}
	}

	files := []models.MediaFile{
		{Root: "/nas", Path: "Neal Stephenson/Anathem/01 - Opening.m4b", Kind: models.MediaFileAudio,
			SizeBytes: 10, Mtime: 1, ScannedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{Root: "/nas", Path: "Neal Stephenson/Anathem/02 - Continuation.m4b", Kind: models.MediaFileAudio,
			SizeBytes: 10, Mtime: 1, ScannedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{Root: "/nas", Path: "books/Anathem.epub", Kind: models.MediaFileEpub,
			SizeBytes: 10, Mtime: 1, ScannedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
	}
	if err := s.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind FROM media_files ORDER BY id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if kind == models.MediaFileAudio {
			audioIDs = append(audioIDs, id)
		} else {
			epubID = id
		}
	}
	return userID, "OL1W", "OL2W", audioIDs, epubID
}

func entryFor(t *testing.T, s *Store, userID, bookID string) string {
	t.Helper()
	entry, err := s.AddBookEntry(context.Background(), userID, bookID, nil, models.StatusBacklog)
	if err != nil {
		t.Fatalf("add entry: %v", err)
	}
	return entry.ID
}

func TestAttachMediaFilesOrdersTracks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, book1, _, audioIDs, epubID := seedAttachFixture(t, s)
	entry := entryFor(t, s, userID, book1)

	// Attached in the order the client supplies — reversed on purpose.
	attached, err := s.AttachMediaFiles(ctx, userID, entry, []int64{audioIDs[1], audioIDs[0]}, models.MediaFileAudio)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(attached) != 2 {
		t.Fatalf("attached %d files, want 2", len(attached))
	}

	files, err := s.MediaFilesForBook(ctx, book1)
	if err != nil {
		t.Fatalf("list for book: %v", err)
	}
	if len(files) != 2 || files[0].Path != "Neal Stephenson/Anathem/02 - Continuation.m4b" {
		t.Fatalf("track order wrong: %+v", files)
	}
	for i, f := range files {
		var track int
		if err := s.db.QueryRowContext(ctx,
			`SELECT track_number FROM media_files WHERE id = ?`, f.ID).Scan(&track); err != nil {
			t.Fatalf("track probe: %v", err)
		}
		if track != i+1 {
			t.Errorf("file %d track_number = %d, want %d", f.ID, track, i+1)
		}
	}

	// An epub joins the same book with no track number; kind mismatches and
	// mixed batches are refused.
	if _, err := s.AttachMediaFiles(ctx, userID, entry, []int64{epubID}, models.MediaFileAudio); err == nil {
		t.Error("epub in an audio batch was allowed")
	}
	if _, err := s.AttachMediaFiles(ctx, userID, entry, []int64{epubID}, models.MediaFileEpub); err != nil {
		t.Fatalf("attach epub: %v", err)
	}
	var track any
	if err := s.db.QueryRowContext(ctx,
		`SELECT track_number FROM media_files WHERE id = ?`, epubID).Scan(&track); err != nil {
		t.Fatalf("epub track probe: %v", err)
	}
	if track != nil {
		t.Errorf("epub track_number = %v, want NULL", track)
	}
}

func TestAttachMediaFilesScoping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, book1, book2, audioIDs, _ := seedAttachFixture(t, s)
	entry := entryFor(t, s, userID, book1)
	otherEntry := entryFor(t, s, userID, book2)
	other, err := s.CreateUser(ctx, "other@example.com", "other", "hashhash")
	if err != nil {
		t.Fatalf("other user: %v", err)
	}

	if _, err := s.AttachMediaFiles(ctx, userID, "missing-entry", audioIDs, models.MediaFileAudio); err != ErrNotFound {
		t.Errorf("unknown entry err = %v, want ErrNotFound", err)
	}
	if _, err := s.AttachMediaFiles(ctx, other.ID, entry, audioIDs, models.MediaFileAudio); err != ErrNotFound {
		t.Errorf("another user's entry err = %v, want ErrNotFound", err)
	}
	if _, err := s.AttachMediaFiles(ctx, userID, entry, []int64{99999}, models.MediaFileAudio); err != ErrNotFound {
		t.Errorf("unknown file err = %v, want ErrNotFound", err)
	}
	if _, err := s.AttachMediaFiles(ctx, userID, entry, []int64{audioIDs[0], audioIDs[0]}, models.MediaFileAudio); err == nil {
		t.Error("duplicate file id in one batch was allowed")
	}
	if _, err := s.AttachMediaFiles(ctx, userID, entry, nil, models.MediaFileAudio); err == nil {
		t.Error("empty batch was allowed")
	}

	// Attaching an already-attached file to a different book is refused —
	// detach first, by design.
	if _, err := s.AttachMediaFiles(ctx, userID, entry, audioIDs, models.MediaFileAudio); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := s.AttachMediaFiles(ctx, userID, otherEntry, audioIDs, models.MediaFileAudio); err != ErrConflict {
		t.Errorf("attaching an already-attached file to another book err = %v, want ErrConflict", err)
	}
	// Re-attaching to the same book is idempotent.
	if _, err := s.AttachMediaFiles(ctx, userID, entry, audioIDs, models.MediaFileAudio); err != nil {
		t.Errorf("re-attach to the same book: %v", err)
	}

	// Files flagged missing are not attachable: the player could not open
	// them anyway.
	if err := s.MarkMediaFilesMissing(ctx, audioIDs, time.Now()); err != nil {
		t.Fatalf("mark missing: %v", err)
	}
	if _, err := s.AttachMediaFiles(ctx, userID, entry, audioIDs, models.MediaFileAudio); err == nil {
		t.Error("missing file was attachable")
	}
}

func TestDetachMediaFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, book1, book2, audioIDs, epubID := seedAttachFixture(t, s)
	entry := entryFor(t, s, userID, book1)
	otherEntry := entryFor(t, s, userID, book2)

	if _, err := s.AttachMediaFiles(ctx, userID, entry, audioIDs, models.MediaFileAudio); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := s.DetachMediaFile(ctx, userID, otherEntry, audioIDs[0]); err != ErrNotFound {
		t.Errorf("detach through the wrong entry err = %v, want ErrNotFound", err)
	}
	if err := s.DetachMediaFile(ctx, userID, entry, epubID); err != ErrNotFound {
		t.Errorf("detach unattached file err = %v, want ErrNotFound", err)
	}
	if err := s.DetachMediaFile(ctx, userID, entry, audioIDs[0]); err != nil {
		t.Fatalf("detach: %v", err)
	}

	// The row survives, unattached, track cleared.
	var bookID *string
	var track *int
	if err := s.db.QueryRowContext(ctx,
		`SELECT book_id, track_number FROM media_files WHERE id = ?`, audioIDs[0]).Scan(&bookID, &track); err != nil {
		t.Fatalf("probe row: %v", err)
	}
	if bookID != nil || track != nil {
		t.Errorf("detached row = (book %v, track %v); want (nil, nil)", bookID, track)
	}

	// Re-attaching elsewhere is now possible, and the sibling keeps its slot.
	if _, err := s.AttachMediaFiles(ctx, userID, otherEntry, []int64{audioIDs[0]}, models.MediaFileAudio); err != nil {
		t.Fatalf("reattach: %v", err)
	}
	files, err := s.MediaFilesForBook(ctx, book1)
	if err != nil {
		t.Fatalf("list book1: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("book1 kept %d files after steal, want 1", len(files))
	}
}

func TestMediaIgnores(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, _, _, audioIDs, epubID := seedAttachFixture(t, s)
	other, err := s.CreateUser(ctx, "ignore@example.com", "ignorer", "hashhash")
	if err != nil {
		t.Fatalf("other user: %v", err)
	}

	n, err := s.IgnoreMediaFiles(ctx, userID, audioIDs)
	if err != nil || n != 2 {
		t.Fatalf("ignore = (%d, %v), want (2, nil)", n, err)
	}
	// Re-ignoring is a no-op, not a conflict.
	if n, err = s.IgnoreMediaFiles(ctx, userID, audioIDs[:1]); err != nil || n != 0 {
		t.Fatalf("re-ignore = (%d, %v), want (0, nil)", n, err)
	}

	ignored, err := s.IgnoredMediaFileIDs(ctx, userID)
	if err != nil {
		t.Fatalf("list ignored: %v", err)
	}
	if !ignored[audioIDs[0]] || !ignored[audioIDs[1]] || ignored[epubID] {
		t.Errorf("ignored set wrong: %+v", ignored)
	}

	otherIgnored, err := s.IgnoredMediaFileIDs(ctx, other.ID)
	if err != nil {
		t.Fatalf("list other ignored: %v", err)
	}
	if len(otherIgnored) != 0 {
		t.Errorf("ignores leaked across users: %+v", otherIgnored)
	}

	if err := s.UnignoreMediaFile(ctx, userID, audioIDs[0]); err != nil {
		t.Fatalf("unignore: %v", err)
	}
	if err := s.UnignoreMediaFile(ctx, userID, epubID); err != ErrNotFound {
		t.Errorf("unignore unknown err = %v, want ErrNotFound", err)
	}
}
