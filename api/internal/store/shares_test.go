package store

import (
	"context"
	"errors"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// sharedFixture is the shape every test here needs: one book with an EPUB and
// two audio tracks attached by the owner, and a second account holding their
// own entry for the very same work. That second entry is the whole point —
// before file ownership existed, adding a book from Open Library was enough
// to stream whatever anyone else had attached to it.
type sharedFixture struct {
	owner, friend       string
	book                string
	ownerEntry          string
	friendEntry         string
	audioIDs            []int64
	epubID              int64
	unattachedBookEntry string
}

func seedSharedFixture(t *testing.T, s *Store) sharedFixture {
	t.Helper()
	owner, book1, book2, audioIDs, epubID := seedAttachFixture(t, s)
	friend := newTestUser(t, s, "friend@example.com", "friend")

	f := sharedFixture{
		owner: owner, friend: friend, book: book1,
		audioIDs: audioIDs, epubID: epubID,
	}
	f.ownerEntry = entryFor(t, s, owner, book1)
	f.friendEntry = entryFor(t, s, friend, book1)
	// A second work nobody has attached anything to, for the "no owner, no
	// question" branch of the rule.
	f.unattachedBookEntry = entryFor(t, s, friend, book2)

	ctx := context.Background()
	if _, err := s.AttachMediaFiles(ctx, owner, f.ownerEntry, []int64{epubID}, models.MediaFileEpub); err != nil {
		t.Fatalf("attach epub: %v", err)
	}
	if _, err := s.AttachMediaFiles(ctx, owner, f.ownerEntry, audioIDs, models.MediaFileAudio); err != nil {
		t.Fatalf("attach audio: %v", err)
	}
	return f
}

// mustAccess asserts whether the user can reach the files behind an entry,
// through both doors: the entry-scoped resolver every read path calls, and
// the book-scoped predicate behind it.
func mustAccess(t *testing.T, s *Store, userID, entryID, bookID string, want bool) {
	t.Helper()
	ctx := context.Background()

	_, err := s.BookFilesForEntry(ctx, userID, entryID)
	switch {
	case want && err != nil:
		t.Fatalf("BookFilesForEntry: got %v, want access", err)
	case !want && !errors.Is(err, ErrNotFound):
		t.Fatalf("BookFilesForEntry: got %v, want ErrNotFound", err)
	}

	ok, err := s.CanAccessBookFiles(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("CanAccessBookFiles: %v", err)
	}
	if ok != want {
		t.Fatalf("CanAccessBookFiles = %v, want %v", ok, want)
	}
}

// TestAttachedFilesAreNotPublic is the regression this whole feature turns
// on. Two accounts, one work, one set of files: adding the book must not be
// enough to read somebody else's copy of it.
func TestAttachedFilesAreNotPublic(t *testing.T) {
	s := newTestStore(t)
	f := seedSharedFixture(t, s)

	mustAccess(t, s, f.owner, f.ownerEntry, f.book, true)
	mustAccess(t, s, f.friend, f.friendEntry, f.book, false)

	// The refusal is specific to files. A book nobody has attached anything
	// to belongs to whoever added it, exactly as before.
	mustAccess(t, s, f.friend, f.unattachedBookEntry, "OL2W", true)
}

func TestShareGrantsAndRevokeWithdraws(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	share, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.friend)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if share.Username != "friend" || share.BookID != f.book {
		t.Fatalf("share = %+v, want the friend on %s", share, f.book)
	}
	if !share.InLibrary {
		t.Error("share.InLibrary = false, but the friend already holds an entry for this book")
	}
	mustAccess(t, s, f.friend, f.friendEntry, f.book, true)

	// Sharing twice is idempotent: the picker sends the state it wants
	// without first reading the state that is.
	again, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.friend)
	if err != nil {
		t.Fatalf("re-share: %v", err)
	}
	if again.ID != share.ID {
		t.Errorf("re-share made a second row: %s then %s", share.ID, again.ID)
	}

	if err := s.RevokeShare(ctx, f.owner, f.ownerEntry, f.friend); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	mustAccess(t, s, f.friend, f.friendEntry, f.book, false)

	// Revoking took the grant and nothing else: the friend still has their
	// own entry, ready for the book to be shared again.
	if _, err := s.GetEntry(ctx, f.friend, f.friendEntry); err != nil {
		t.Fatalf("friend lost their entry to a revoke: %v", err)
	}
	if err := s.RevokeShare(ctx, f.owner, f.ownerEntry, f.friend); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke = %v, want ErrNotFound", err)
	}
}

// TestShareCannotLaunderOthersFiles covers the clause that makes the rule
// safe: a share counts only while the sharer actually owns files on the
// book. Otherwise anyone could add a work, share it onward, and hand out
// access to files a third account attached.
func TestShareCannotLaunderOthersFiles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	third := newTestUser(t, s, "third@example.com", "third")
	thirdEntry := entryFor(t, s, third, f.book)

	// The friend owns no files on this book, but does hold an entry — and
	// so can ask for the share to be created.
	if _, err := s.ShareBook(ctx, f.friend, f.friendEntry, third); err != nil {
		t.Fatalf("share: %v", err)
	}
	mustAccess(t, s, third, thirdEntry, f.book, false)

	// It starts granting the moment the sharer really does own files.
	if err := s.DetachMediaFile(ctx, f.owner, f.ownerEntry, f.audioIDs[0]); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if _, err := s.AttachMediaFiles(ctx, f.friend, f.friendEntry,
		[]int64{f.audioIDs[0]}, models.MediaFileAudio); err != nil {
		t.Fatalf("friend attaches: %v", err)
	}
	mustAccess(t, s, third, thirdEntry, f.book, true)
}

func TestShareRejectsSelfAndStrangers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	if _, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.owner); !errors.Is(err, ErrSelfShare) {
		t.Errorf("self-share = %v, want ErrSelfShare", err)
	}
	if _, err := s.ShareBook(ctx, f.owner, f.ownerEntry, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("share with unknown account = %v, want ErrNotFound", err)
	}
	// Sharing an entry that is not yours is a 404, not a hint that it exists.
	if _, err := s.ShareBook(ctx, f.friend, f.ownerEntry, f.owner); !errors.Is(err, ErrNotFound) {
		t.Errorf("share of a foreign entry = %v, want ErrNotFound", err)
	}
	// Nor may you share with an account that cannot sign in.
	if _, err := s.SetUserDisabled(ctx, f.friend, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.friend); !errors.Is(err, ErrNotFound) {
		t.Errorf("share with a disabled account = %v, want ErrNotFound", err)
	}
}

func TestShareListingsAndBadge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	candidates, err := s.ShareCandidatesForEntry(ctx, f.owner, f.ownerEntry)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Username != "friend" {
		t.Fatalf("candidates = %+v, want just the friend", candidates)
	}
	if candidates[0].Shared {
		t.Error("candidate marked shared before anything was shared")
	}
	if !candidates[0].InLibrary {
		t.Error("candidate should show the friend already has this book")
	}

	if _, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.friend); err != nil {
		t.Fatalf("share: %v", err)
	}

	out, err := s.SharesByOwner(ctx, f.owner)
	if err != nil {
		t.Fatalf("shares by owner: %v", err)
	}
	if len(out) != 1 || out[0].Username != "friend" || out[0].BookTitle != "Anathem" {
		t.Fatalf("SharesByOwner = %+v, want one row for Anathem", out)
	}

	in, err := s.SharesWithUser(ctx, f.friend)
	if err != nil {
		t.Fatalf("shares with user: %v", err)
	}
	if len(in) != 1 || in[0].OwnerName != "attacher" {
		t.Fatalf("SharesWithUser = %+v, want one row from the attacher", in)
	}

	// The listing badge: the friend's copy is read through somebody else's
	// files, and says so. The owner's own copy is not badged.
	entries, err := s.ListEntries(ctx, f.friend, LibraryFilter{MediaType: models.MediaBook})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	var badged int
	for _, e := range entries {
		if e.ID == f.friendEntry {
			if e.SharedBy != "attacher" {
				t.Errorf("friend's entry SharedBy = %q, want attacher", e.SharedBy)
			}
			badged++
		}
		if e.ID == f.unattachedBookEntry && e.SharedBy != "" {
			t.Errorf("unshared book badged as %q", e.SharedBy)
		}
	}
	if badged != 1 {
		t.Fatalf("found %d copies of the friend's entry, want 1", badged)
	}

	ownerEntries, err := s.ListEntries(ctx, f.owner, LibraryFilter{MediaType: models.MediaBook})
	if err != nil {
		t.Fatalf("list owner entries: %v", err)
	}
	for _, e := range ownerEntries {
		if e.SharedBy != "" {
			t.Errorf("owner's own entry badged as shared by %q", e.SharedBy)
		}
	}
}

// TestSharedReaderSeesFilesButNotPaths pairs with the handler's redaction:
// the store hands a shared reader the same rows it hands the owner, so the
// blanking of root and path is the HTTP layer's job and has to stay there.
func TestSharedReaderGetsTheFileList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	if _, err := s.MediaFilesForEntry(ctx, f.friend, f.friendEntry); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unshared file list = %v, want ErrNotFound", err)
	}
	if _, err := s.ShareBook(ctx, f.owner, f.ownerEntry, f.friend); err != nil {
		t.Fatalf("share: %v", err)
	}
	files, err := s.MediaFilesForEntry(ctx, f.friend, f.friendEntry)
	if err != nil {
		t.Fatalf("shared file list: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("shared file list has %d files, want 3", len(files))
	}
	for _, file := range files {
		if file.AttachedBy == nil || *file.AttachedBy != f.owner {
			t.Errorf("file %d attached_by = %v, want the owner", file.ID, file.AttachedBy)
		}
	}
}

// TestDetachClearsOwnership keeps the two halves of an attachment together:
// a file that is no longer attached has no owner either, so it cannot leave
// a stale grant behind when it is attached somewhere else.
func TestDetachClearsOwnership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	f := seedSharedFixture(t, s)

	if err := s.DetachMediaFile(ctx, f.owner, f.ownerEntry, f.audioIDs[0]); err != nil {
		t.Fatalf("detach: %v", err)
	}
	var attachedBy *string
	if err := s.db.QueryRowContext(ctx,
		`SELECT attached_by FROM media_files WHERE id = ?`, f.audioIDs[0]).Scan(&attachedBy); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if attachedBy != nil {
		t.Errorf("attached_by = %q after detach, want NULL", *attachedBy)
	}
}
