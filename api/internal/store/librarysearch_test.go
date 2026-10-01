package store

import (
	"context"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// seedLibrarySearch inventories two books with parsed canonical texts —
// the minimum the FTS index and its scope query need to prove anything.
func seedLibrarySearch(t *testing.T, s *Store) (userID, otherUser, entryA, entryB string) {
	t.Helper()
	ctx := context.Background()
	userID = newTestUser(t, s, "shelf@example.com", "shelf")
	otherUser = newTestUser(t, s, "other@example.com", "other")
	for _, b := range []struct{ id, title string }{
		{"OL1W", "The Village Mystery"}, {"OL2W", "A Second Book"},
	} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO books (id, title) VALUES (?, ?)`, b.id, b.title); err != nil {
			t.Fatalf("seed book %s: %v", b.id, err)
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at, attached_by, is_primary_text)
			VALUES ('/nas', ?, 'epub', 10, 1, ?, ?, ?, 1)`,
			b.id+".epub", b.id, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), userID); err != nil {
			t.Fatalf("seed file %s: %v", b.id, err)
		}
	}
	entryA = entryFor(t, s, userID, "OL1W")
	entryB = entryFor(t, s, userID, "OL2W")
	return userID, otherUser, entryA, entryB
}

// indexedText returns the media file id of a book's primary file, so a test
// can index against the row the scope query will resolve.
func indexedTextID(t *testing.T, s *Store, bookID string) int64 {
	t.Helper()
	var id int64
	if err := s.db.QueryRowContext(context.Background(), `
		SELECT id FROM media_files
		WHERE book_id = ? AND kind = 'epub' AND is_primary_text = 1`, bookID).Scan(&id); err != nil {
		t.Fatalf("primary file of %s: %v", bookID, err)
	}
	return id
}

func libraryChapters(chars ...[2]int) []models.EpubChapter {
	out := make([]models.EpubChapter, len(chars))
	for i, r := range chars {
		out[i] = models.EpubChapter{SpineIndex: i, CharStart: r[0], CharEnd: r[1], TitleSource: "none"}
	}
	return out
}

func TestReplaceBookTextIndexIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	_, _, _, _ = seedLibrarySearch(t, s)
	ctx := context.Background()
	file := indexedTextID(t, s, "OL1W")

	chapters := libraryChapters([2]int{0, 26}, [2]int{27, 48})
	text := "the quiet village slept under the hill but the butler did it"
	write := func() {
		if err := s.ReplaceBookTextIndex(ctx, "OL1W", file, chapters, text); err != nil {
			t.Fatalf("replace: %v", err)
		}
	}
	write()
	write() // a re-parse lands in place, not beside itself

	var rows int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM book_text_fts WHERE media_file_id = ?`, file).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows after re-index = %d, want 2", rows)
	}

	ifed, err := s.BookTextIndexed(ctx, file)
	if err != nil || !ifed {
		t.Errorf("BookTextIndexed = %v, %v; want true", ifed, err)
	}
	ifed, err = s.BookTextIndexed(ctx, file+1)
	if err != nil || ifed {
		t.Errorf("BookTextIndexed of a stranger = %v, %v; want false", ifed, err)
	}
}

func TestSearchBookTextIndexRanksBooks(t *testing.T) {
	s := newTestStore(t)
	_, _, _, _ = seedLibrarySearch(t, s)
	ctx := context.Background()

	text1 := "the quiet village slept under the hill"
	text2 := "a loud city woke instead and the village never slept"
	if err := s.ReplaceBookTextIndex(ctx, "OL1W", indexedTextID(t, s, "OL1W"),
		libraryChapters([2]int{0, len(text1)}), text1); err != nil {
		t.Fatalf("index OL1W: %v", err)
	}
	if err := s.ReplaceBookTextIndex(ctx, "OL2W", indexedTextID(t, s, "OL2W"),
		libraryChapters([2]int{0, len(text2)}), text2); err != nil {
		t.Fatalf("index OL2W: %v", err)
	}

	// The phrase picks out only the book that contains it, verbatim.
	hits, err := s.SearchBookTextIndex(ctx, `"quiet village"`, 500)
	if err != nil {
		t.Fatalf("phrase search: %v", err)
	}
	if len(hits) != 1 || hits[0].BookID != "OL1W" {
		t.Fatalf("phrase hits = %+v, want exactly OL1W", hits)
	}

	// The terms in any order find both books, ranked (not asserted
	// numerically — only that both answer).
	hits, err = s.SearchBookTextIndex(ctx, `village slept`, 500)
	if err != nil {
		t.Fatalf("and search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("and hits = %+v, want both books", hits)
	}
}

func TestFoldForFTSSeparatesCJKRuns(t *testing.T) {
	s := newTestStore(t)
	_, _, _, _ = seedLibrarySearch(t, s)
	ctx := context.Background()

	text := "東京の物語はここから始まる"
	if err := s.ReplaceBookTextIndex(ctx, "OL1W", indexedTextID(t, s, "OL1W"),
		libraryChapters([2]int{0, len(text)}), text); err != nil {
		t.Fatalf("index: %v", err)
	}
	// The query folds through the same function, so a remembered fragment
	// matches as a phrase — the in-book search's behavior, kept.
	hits, err := s.SearchBookTextIndex(ctx, `"`+FoldForFTS("物語はここ")+`"`, 500)
	if err != nil {
		t.Fatalf("cjk search: %v", err)
	}
	if len(hits) != 1 || hits[0].BookID != "OL1W" {
		t.Fatalf("cjk hits = %+v, want OL1W", hits)
	}
}

func TestLibrarySearchScopeAppliesTheFileAccessRule(t *testing.T) {
	s := newTestStore(t)
	_, other, _, _ := seedLibrarySearch(t, s)
	ctx := context.Background()

	text := "some indexed words for the scope test"
	if err := s.ReplaceBookTextIndex(ctx, "OL1W", indexedTextID(t, s, "OL1W"),
		libraryChapters([2]int{0, len(text)}), text); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := s.ReplaceEpubText(ctx, models.EpubText{
		MediaFileID: indexedTextID(t, s, "OL1W"), CharCount: len(text),
		NormalizedSHA256: "sha-1", ParserVersion: "v1",
	}, libraryChapters([2]int{0, len(text)}), nil); err != nil {
		t.Fatalf("seed text: %v", err)
	}

	seeded, err := s.LibrarySearchScope(ctx, other, []string{"OL1W"})
	if err != nil {
		t.Fatalf("scope for a stranger: %v", err)
	}
	if len(seeded) != 0 {
		t.Errorf("a stranger with no entry scoped %+v, want none", seeded)
	}

	// The stranger's own entry still fails the file rule: the owner
	// attached the file. Unshared means invisible, same as every read path.
	entryFor(t, s, other, "OL1W")
	seeded, err = s.LibrarySearchScope(ctx, other, []string{"OL1W"})
	if err != nil {
		t.Fatalf("scope unshared: %v", err)
	}
	if len(seeded) != 0 {
		t.Errorf("unshared entry scoped %+v, want none", seeded)
	}

	// A share opens it, and the scope carries everything the handler
	// places hits with.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO book_shares (id, book_id, owner_id, shared_with_id)
		VALUES ('sh1', 'OL1W',
		        (SELECT attached_by FROM media_files WHERE book_id = 'OL1W'), ?)`, other); err != nil {
		t.Fatalf("seed share: %v", err)
	}
	seeded, err = s.LibrarySearchScope(ctx, other, []string{"OL1W"})
	if err != nil {
		t.Fatalf("scope shared: %v", err)
	}
	if len(seeded) != 1 || seeded[0].BookID != "OL1W" || seeded[0].Title != "The Village Mystery" {
		t.Fatalf("shared scope = %+v", seeded)
	}
	if seeded[0].CharCount != len(text) || seeded[0].NormalizedSHA256 != "sha-1" {
		t.Errorf("scope facts = %+v", seeded[0])
	}
}

func TestBookTextIndexCandidatesSkipsCurrentBooks(t *testing.T) {
	s := newTestStore(t)
	_, _, _, _ = seedLibrarySearch(t, s)
	ctx := context.Background()

	file := indexedTextID(t, s, "OL1W")
	text := "an indexed book"
	if err := s.ReplaceEpubText(ctx, models.EpubText{
		MediaFileID: file, CharCount: len(text),
		NormalizedSHA256: "sha-1", ParserVersion: "v1",
	}, libraryChapters([2]int{0, len(text)}), nil); err != nil {
		t.Fatalf("seed text: %v", err)
	}

	// Parsed but unindexed, plus the never-parsed second book: both
	// candidates, oldest first.
	files, err := s.BookTextIndexCandidates(ctx, "v1", 0, 10)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(files) != 2 || files[0].ID != file {
		t.Fatalf("candidates = %+v, want the unindexed file first", files)
	}

	// Indexed on both legs — FTS and names: no longer a candidate. The
	// never-parsed second book stays one, and the cursor skips past what a
	// lap already walked.
	if err := s.ReplaceBookTextIndex(ctx, "OL1W", file,
		libraryChapters([2]int{0, len(text)}), text); err != nil {
		t.Fatalf("index: %v", err)
	}
	// Names alone lagging keeps the book a candidate — the walk heals
	// both indexes, and a fresh extractor era must not read as current.
	files, err = s.BookTextIndexCandidates(ctx, "v1", 0, 10)
	if err != nil {
		t.Fatalf("candidates names-stale: %v", err)
	}
	if len(files) != 2 || files[0].ID != file {
		t.Fatalf("candidates = %+v, want the name-stale file still first", files)
	}
	if err := s.ReplaceNameIndex(ctx, file, "n1", nil); err != nil {
		t.Fatalf("names index: %v", err)
	}
	files, err = s.BookTextIndexCandidates(ctx, "v1", 0, 10)
	if err != nil {
		t.Fatalf("candidates after index: %v", err)
	}
	var second int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_files WHERE book_id = 'OL2W'`).Scan(&second); err != nil {
		t.Fatalf("second file: %v", err)
	}
	if len(files) != 1 || files[0].ID != second {
		t.Fatalf("candidates after index = %+v, want only the unparsed book", files)
	}
	files, err = s.BookTextIndexCandidates(ctx, "v1", second, 10)
	if err != nil {
		t.Fatalf("candidates past cursor: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("candidates past the cursor = %+v, want none", files)
	}
}
