package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedFormats inventories one book's worth of text files — an .epub and the
// .mobi of the same title beside it, the exact shape a Calibre export leaves
// on a NAS — plus the user and library entry to attach them to.
func seedFormats(t *testing.T, s *Store, paths ...string) (userID, entryID string, ids []int64) {
	t.Helper()
	ctx := context.Background()
	userID = newTestUser(t, s, "formats@example.com", "formats")
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Carrie')`); err != nil {
		t.Fatalf("seed book: %v", err)
	}
	files := make([]models.MediaFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, models.MediaFile{
			Root: "/nas", Path: p, Kind: models.MediaFileEpub, SizeBytes: 10, Mtime: 1,
			ScannedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	if err := s.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for _, p := range paths {
		var id int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT id FROM media_files WHERE path = ?`, p).Scan(&id); err != nil {
			t.Fatalf("id of %s: %v", p, err)
		}
		ids = append(ids, id)
	}
	return userID, entryFor(t, s, userID, "OL1W"), ids
}

func seedText(t *testing.T, s *Store, fileID int64, chars int, sha string) {
	t.Helper()
	et := models.EpubText{
		MediaFileID: fileID, CharCount: chars, WordCount: chars / 6,
		NormalizedSHA256: sha, ParserVersion: "2",
	}
	if err := s.ReplaceEpubText(context.Background(), et, nil); err != nil {
		t.Fatalf("seed text for %d: %v", fileID, err)
	}
}

func primaryOf(t *testing.T, s *Store, bookID string) int64 {
	t.Helper()
	var id int64
	err := s.db.QueryRowContext(context.Background(), `
		SELECT id FROM media_files
		WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID).Scan(&id)
	if err != nil {
		t.Fatalf("primary of %s: %v", bookID, err)
	}
	return id
}

// Attaching both formats of one book must designate the higher-fidelity
// container and leave the other alone. This is the whole point of the
// alternate-format flow: a second file is a fact about what the user owns,
// not a second canonical text.
func TestAttachTextFormatsDesignatesOnePrimary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")

	files, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	primaries := 0
	for _, f := range files {
		if f.PrimaryText {
			primaries++
			if f.ID != ids[0] {
				t.Fatalf("primary is %s, want the epub", f.Path)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("primary count = %d, want exactly 1", primaries)
	}

	got, err := s.EpubMediaFileForBook(ctx, "OL1W")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != ids[0] {
		t.Fatalf("reader resolves to %s, want the epub", got.Path)
	}
}

// The mobi arriving a week after the epub was confirmed is the real-world
// order, and it must not disturb the text the book is already read from.
func TestAttachingSecondFormatLaterKeepsPrimary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.mobi", "King/Carrie.epub")

	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids[:1], models.MediaFileEpub); err != nil {
		t.Fatalf("attach mobi: %v", err)
	}
	if got := primaryOf(t, s, "OL1W"); got != ids[0] {
		t.Fatalf("primary = %d, want the only attached file %d", got, ids[0])
	}

	// The epub outranks the mobi on format, and still does not take over: a
	// designation, once made, only moves when someone moves it.
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids[1:], models.MediaFileEpub); err != nil {
		t.Fatalf("attach epub: %v", err)
	}
	if got := primaryOf(t, s, "OL1W"); got != ids[0] {
		t.Fatalf("primary moved to %d on a later attach, want %d", got, ids[0])
	}
}

// A missing primary must not fall through to a sibling. Serving the other
// container's text during a NAS outage would silently reinterpret every
// stored offset, which is worse than saying the book is unavailable.
func TestMissingPrimaryDoesNotFallBackToAlternate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := s.MarkMediaFilesMissing(ctx, []int64{ids[0]}, time.Now()); err != nil {
		t.Fatalf("mark missing: %v", err)
	}
	if _, err := s.EpubMediaFileForBook(ctx, "OL1W"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolve with the primary missing = %v, want ErrNotFound", err)
	}

	if err := s.RestoreMediaFiles(ctx, []int64{ids[0]}, time.Now()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := s.EpubMediaFileForBook(ctx, "OL1W")
	if err != nil || got.ID != ids[0] {
		t.Fatalf("resolve after restore = (%d, %v), want the epub back", got.ID, err)
	}
}

// Two containers converted from one source canonicalize identically, which
// is the common case in a Calibre library. Identical text means identical
// coordinates, so a switch must not touch a single stored number.
func TestSwitchPrimaryKeepsOffsetsWhenTextIsIdentical(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}
	seedText(t, s, ids[0], 323858, "sha-same")
	seedText(t, s, ids[1], 323858, "sha-same")
	if _, err := s.SaveBookProgress(ctx, userID, entryID, ProgressWrite{
		CharOffset: 161929, Source: models.PositionSourceRead, PercentComplete: 50,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	if _, err := s.SetPrimaryTextFile(ctx, userID, entryID, ids[1]); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got := primaryOf(t, s, "OL1W"); got != ids[1] {
		t.Fatalf("primary = %d, want the mobi %d", got, ids[1])
	}
	p, err := s.BookProgress(ctx, userID, entryID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if p.CharOffset != 161929 {
		t.Fatalf("char_offset = %d, want it untouched at 161929", p.CharOffset)
	}
}

// Different canonicalizations mean the old offset points somewhere else in
// the new text. The percentage is what the reader recognises as their place,
// so that is what survives — and the alignment, which maps offsets of the
// old text onto audio, is dropped rather than reinterpreted.
func TestSwitchPrimaryMigratesOffsetsWhenTextDiffers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}
	seedText(t, s, ids[0], 100000, "sha-epub")
	seedText(t, s, ids[1], 120000, "sha-mobi")
	if _, err := s.SaveBookProgress(ctx, userID, entryID, ProgressWrite{
		CharOffset: 25000, Source: models.PositionSourceRead, PercentComplete: 25,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	oldText, err := s.GetEpubText(ctx, ids[0])
	if err != nil {
		t.Fatalf("old text: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO alignments (id, entry_id, epub_text_id, state, coverage, mean_confidence, model)
		 VALUES ('al1', ?, ?, 'ready', 0.9, 0.9, 'base.en')`, entryID, oldText.ID); err != nil {
		t.Fatalf("seed alignment: %v", err)
	}

	if _, err := s.SetPrimaryTextFile(ctx, userID, entryID, ids[1]); err != nil {
		t.Fatalf("switch: %v", err)
	}
	p, err := s.BookProgress(ctx, userID, entryID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if p.CharOffset != 30000 {
		t.Fatalf("char_offset = %d, want 25%% of the new 120000-char text", p.CharOffset)
	}
	align, err := s.AlignmentForEntry(ctx, entryID)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if align.ID != "" {
		t.Fatalf("alignment %q survived a text switch, want it dropped", align.ID)
	}
}

// Promoting onto a container nobody has parsed would mean migrating every
// offset onto a length nobody knows. The store refuses instead of guessing.
func TestSetPrimaryTextRefusesUnparsedTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}
	seedText(t, s, ids[0], 100000, "sha-epub")

	if _, err := s.SetPrimaryTextFile(ctx, userID, entryID, ids[1]); !errors.Is(err, ErrTextNotParsed) {
		t.Fatalf("switch onto an unparsed file = %v, want ErrTextNotParsed", err)
	}
	if got := primaryOf(t, s, "OL1W"); got != ids[0] {
		t.Fatalf("primary moved to %d on a refused switch", got)
	}
}

// Detaching the canonical text must hand the designation to what is left,
// or the book reads as having no ebook at all while one is still attached.
func TestDetachPrimaryPromotesRemainingFormat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}
	seedText(t, s, ids[0], 100000, "sha-epub")
	seedText(t, s, ids[1], 120000, "sha-mobi")
	if _, err := s.SaveBookProgress(ctx, userID, entryID, ProgressWrite{
		CharOffset: 50000, Source: models.PositionSourceRead, PercentComplete: 50,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}

	if err := s.DetachMediaFile(ctx, userID, entryID, ids[0]); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if got := primaryOf(t, s, "OL1W"); got != ids[1] {
		t.Fatalf("primary = %d after detaching the epub, want the mobi %d", got, ids[1])
	}
	p, err := s.BookProgress(ctx, userID, entryID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if p.CharOffset != 60000 {
		t.Fatalf("char_offset = %d, want 50%% of the promoted 120000-char text", p.CharOffset)
	}
}

// Two primaries for one book is the state every consumer of the canonical
// text would disagree about, so the schema refuses it outright.
func TestOnlyOnePrimaryTextPerBook(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID, entryID, ids := seedFormats(t, s, "King/Carrie.epub", "King/Carrie.mobi")
	if _, err := s.AttachMediaFiles(ctx, userID, entryID, ids, models.MediaFileEpub); err != nil {
		t.Fatalf("attach: %v", err)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET is_primary_text = 1 WHERE id = ?`, ids[1])
	if err == nil {
		t.Fatal("a second primary was accepted, want the unique index to refuse it")
	}
}
