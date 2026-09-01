package media

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// fakeProvider is a canned BookProvider: Search returns the same hits for
// any query, already shaped like Open Library results.
type fakeProvider struct {
	hits []metadata.Book
}

func (f *fakeProvider) Search(_ context.Context, _ string, _ int) ([]metadata.Book, error) {
	return f.hits, nil
}
func (f *fakeProvider) GetByWorkKey(_ context.Context, _ string) (metadata.Book, error) {
	return metadata.Book{}, fmt.Errorf("not implemented in tests")
}
func (f *fakeProvider) GetByISBN(_ context.Context, _ string) (metadata.Book, error) {
	return metadata.Book{}, fmt.Errorf("not implemented in tests")
}
func (f *fakeProvider) GetEditions(_ context.Context, _ string) ([]metadata.BookEdition, error) {
	return nil, fmt.Errorf("not implemented in tests")
}

// matchFixture seeds a user's library with books and inventories unattached
// files, then returns the matcher's candidates.
func matchCandidates(t *testing.T, provider metadata.BookProvider, library []metadata.Book, files []models.MediaFile) []Candidate {
	t.Helper()
	st := newTestStore(t)
	ctx := context.Background()
	userID := testUser(t, st)
	for _, b := range library {
		if err := st.UpsertBook(ctx, b, ""); err != nil {
			t.Fatalf("seed book %s: %v", b.ID, err)
		}
		if _, err := st.AddBookEntry(ctx, userID, b.ID, nil, models.StatusBacklog); err != nil {
			t.Fatalf("add entry %s: %v", b.ID, err)
		}
	}
	if err := st.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert files: %v", err)
	}
	m := NewMatcher(st, provider)
	candidates, err := m.Candidates(ctx, userID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	return candidates
}

func testUser(t *testing.T, st *store.Store) string {
	t.Helper()
	u, err := st.CreateUser(context.Background(), "matcher@example.com", "matcher", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// taggedAudio builds an audio file row with embedded container tags.
func taggedAudio(pathStr string, title, artist, album string, track int) models.MediaFile {
	raw, _ := json.Marshal(audioTags{Title: title, Artist: artist, Album: album, Track: track})
	return models.MediaFile{Root: "/nas", Path: pathStr, Kind: models.MediaFileAudio,
		SizeBytes: 1, Mtime: 1, ContainerMetadata: raw,
		DurationSeconds: ptrFloat(60.0), ScannedAt: scannedNow}
}

func plainAudio(pathStr string) models.MediaFile {
	return models.MediaFile{Root: "/nas", Path: pathStr, Kind: models.MediaFileAudio,
		SizeBytes: 1, Mtime: 1, ScannedAt: scannedNow}
}

func epubFile(pathStr string) models.MediaFile {
	return models.MediaFile{Root: "/nas", Path: pathStr, Kind: models.MediaFileEpub,
		SizeBytes: 1, Mtime: 1, ScannedAt: scannedNow}
}

func ptrFloat(v float64) *float64 { return &v }

var scannedNow = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

var testLibrary = []metadata.Book{
	{ID: "OL1W", Title: "Anathem", Authors: []string{"Neal Stephenson"}},
	{ID: "OL2W", Title: "Project Hail Mary", Authors: []string{"Andy Weir"}},
	{ID: "OL3W", Title: "Dune", Authors: []string{"Frank Herbert"}},
}

func findCandidate(t *testing.T, cs []Candidate, key string) Candidate {
	t.Helper()
	for _, c := range cs {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no candidate with key %q among %v", key, keysOf(cs))
	return Candidate{}
}

func keysOf(cs []Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

func pathsOf(c Candidate) []string {
	out := make([]string, 0, len(c.Files))
	for _, f := range c.Files {
		out = append(out, f.Path)
	}
	return out
}

// TestMatchDirectoryPerBookTree: the near-universal audiobook layout — a
// directory of numbered parts under /Author/Book Title/. Tagged and
// untagged variants both resolve to one grouped, ordered candidate with a
// confident library suggestion.
func TestMatchDirectoryPerBookTree(t *testing.T) {
	// Tagged: the tags carry album and artist, the directory agrees.
	tagged := []models.MediaFile{
		taggedAudio("Neal Stephenson/Anathem/01 - Opening.m4b", "Opening", "Neal Stephenson", "Anathem", 1),
		taggedAudio("Neal Stephenson/Anathem/02 - Continuation.m4b", "Continuation", "Neal Stephenson", "Anathem", 2),
	}
	cs := matchCandidates(t, nil, testLibrary, tagged)
	c := findCandidate(t, cs, "audio:/nas:Neal Stephenson/Anathem")
	if len(c.Files) != 2 {
		t.Fatalf("grouped %d files, want 2", len(c.Files))
	}
	if c.TitleGuess != "Anathem" || c.AuthorGuess != "Neal Stephenson" {
		t.Errorf("tag signal = (%q, %q)", c.TitleGuess, c.AuthorGuess)
	}
	if !c.HighConfidence || len(c.Suggestions) == 0 {
		t.Fatalf("tagged dir candidate not high confidence: %+v", c.Suggestions)
	}
	if top := c.Suggestions[0]; top.Book.ID != "OL1W" || top.Source != SourceLibrary || top.Signal != SourceSignalTags {
		t.Errorf("top suggestion = %+v", top)
	}
	if c.TotalDurationSeconds != 120 {
		t.Errorf("total duration = %v, want 120", c.TotalDurationSeconds)
	}

	// Untagged: the directory layout alone still carries it.
	untagged := []models.MediaFile{
		plainAudio("Andy Weir/Project Hail Mary/Part 1.m4b"),
		plainAudio("Andy Weir/Project Hail Mary/Part 2.m4b"),
	}
	cs = matchCandidates(t, nil, testLibrary, untagged)
	c = findCandidate(t, cs, "audio:/nas:Andy Weir/Project Hail Mary")
	if c.TitleGuess != "Project Hail Mary" || c.AuthorGuess != "Andy Weir" {
		t.Errorf("dir signal = (%q, %q)", c.TitleGuess, c.AuthorGuess)
	}
	if !c.HighConfidence {
		t.Errorf("dir-layout candidate not high confidence: %+v", c.Suggestions)
	}
	if top := c.Suggestions[0]; top.Book.ID != "OL2W" || top.Signal != SourceSignalDir {
		t.Errorf("top suggestion = %+v", top)
	}
}

// TestMatchSingleFilePerBookTree: /Author/Book.m4b — one file, no tags. The
// filename names the book, the directory names the author.
func TestMatchSingleFilePerBookTree(t *testing.T) {
	files := []models.MediaFile{plainAudio("Frank Herbert/Dune.m4b")}
	cs := matchCandidates(t, nil, testLibrary, files)

	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1: %v", len(cs), keysOf(cs))
	}
	audio := cs[0]
	if audio.Kind != models.MediaFileAudio {
		t.Fatalf("candidate kind = %s", audio.Kind)
	}
	if audio.TitleGuess != "Dune" || audio.AuthorGuess != "Frank Herbert" {
		t.Fatalf("signal = (%q, %q), want (Dune, Frank Herbert)", audio.TitleGuess, audio.AuthorGuess)
	}
	if !audio.HighConfidence {
		t.Errorf("single-file layout not high confidence: %+v", audio.Suggestions)
	}
	if top := audio.Suggestions[0]; top.Book.ID != "OL3W" {
		t.Errorf("top suggestion = %+v", top)
	}
}

// TestMatchFlatDump: a root-level pile of mixed files. Audio files sharing
// the empty directory form one group (the layout gives no reason to split
// them); the EPUB stands alone; filename evidence alone never earns the
// bulk-confirm stamp.
func TestMatchFlatDump(t *testing.T) {
	files := []models.MediaFile{
		plainAudio("Anathem.m4b"),
		epubFile("Dune.epub"),
		plainAudio("track01.mp3"),
	}
	cs := matchCandidates(t, nil, testLibrary, files)

	if len(cs) != 2 {
		t.Fatalf("got %d candidates, want 2 (one audio group, one epub): %v", len(cs), keysOf(cs))
	}
	audio := findCandidate(t, cs, "audio:/nas:.")
	var epub Candidate
	for _, cand := range cs {
		if cand.Kind == models.MediaFileEpub && len(cand.Files) == 1 && cand.Files[0].Path == "Dune.epub" {
			epub = cand
		}
	}
	if epub.Key == "" {
		t.Fatalf("no epub candidate for Dune.epub among %v", keysOf(cs))
	}

	// The audio group keeps natural filename order and guesses from its
	// first file. A real suggestion arrives, but filename-only evidence is
	// not bulk-confirmable.
	if got := pathsOf(audio); len(got) != 2 || got[0] != "Anathem.m4b" || got[1] != "track01.mp3" {
		t.Fatalf("flat audio group files = %v", got)
	}
	if audio.TitleGuess != "Anathem" {
		t.Errorf("flat audio title = %q", audio.TitleGuess)
	}
	if top := audio.Suggestions[0]; top.Book.ID != "OL1W" || top.Signal != SourceSignalFile {
		t.Errorf("flat audio top = %+v", top)
	}
	if audio.HighConfidence {
		t.Errorf("filename-only candidate flagged high confidence: %+v", audio.Suggestions)
	}
	if epub.TitleGuess != "Dune" {
		t.Errorf("flat epub title = %q", epub.TitleGuess)
	}
	if top := epub.Suggestions[0]; top.Book.ID != "OL3W" {
		t.Errorf("flat epub top = %+v", top)
	}
	if epub.HighConfidence {
		t.Errorf("filename-only epub flagged high confidence: %+v", epub.Suggestions)
	}
}

// TestMatchThirtyPartOrdering: the acceptance case — 30 parts, zero-padded
// and non-padded names mixed, with and without tag track numbers.
func TestMatchThirtyPartOrdering(t *testing.T) {
	// Without tags: natural sort must interleave padded and unpadded names.
	var files []models.MediaFile
	files = append(files, plainAudio("Neal Stephenson/Anathem/Chapter 2.mp3"))
	for i := 1; i <= 30; i++ {
		// Chapters 1..9 padded two ways ("01", "1"), 10..30 unpadded.
		if i <= 9 {
			files = append(files, plainAudio(fmt.Sprintf("Neal Stephenson/Anathem/%02d - Part.mp3", i)))
		} else {
			files = append(files, plainAudio(fmt.Sprintf("Neal Stephenson/Anathem/%d - Part.mp3", i)))
		}
	}
	cs := matchCandidates(t, nil, testLibrary, files)
	c := findCandidate(t, cs, "audio:/nas:Neal Stephenson/Anathem")
	if len(c.Files) != 31 {
		t.Fatalf("grouped %d files, want 31", len(c.Files))
	}
	got := pathsOf(c)
	// Numbered parts sort numerically whether zero-padded or not, and the
	// un-numbered "Chapter 2" lands after them all.
	expectAt := map[int]string{
		0:  "Neal Stephenson/Anathem/01 - Part.mp3",
		1:  "Neal Stephenson/Anathem/02 - Part.mp3",
		8:  "Neal Stephenson/Anathem/09 - Part.mp3",
		9:  "Neal Stephenson/Anathem/10 - Part.mp3",
		10: "Neal Stephenson/Anathem/11 - Part.mp3",
		28: "Neal Stephenson/Anathem/29 - Part.mp3",
		29: "Neal Stephenson/Anathem/30 - Part.mp3",
		30: "Neal Stephenson/Anathem/Chapter 2.mp3",
	}
	for idx, want := range expectAt {
		if got[idx] != want {
			t.Fatalf("natural order broken at %d: %q, want %q\nfull: %v", idx, got[idx], want, got)
		}
	}

	// With tags: track numbers win over the filename order.
	var tagged []models.MediaFile
	for i := 1; i <= 30; i++ {
		// Name says 31-i, tag says i: tags must dominate.
		tagged = append(tagged, taggedAudio(
			fmt.Sprintf("Book/%02d.mp3", 31-i), "", "", "Book", i))
	}
	cs = matchCandidates(t, nil, testLibrary, tagged)
	c = findCandidate(t, cs, "audio:/nas:Book")
	if len(c.Files) != 30 {
		t.Fatalf("grouped %d tagged files, want 30", len(c.Files))
	}
	for i, f := range c.Files {
		want := fmt.Sprintf("Book/%02d.mp3", 30-i)
		if f.Path != want {
			t.Fatalf("tag order broken at %d: %q, want %q", i, f.Path, want)
		}
	}
}

// TestMatchFallsBackToOpenLibrary: when the library cannot explain the
// files, the provider is searched, its results are cached, and the
// suggestion carries the openlibrary source. The provider is skipped
// entirely when the library already clears the bar.
func TestMatchFallsBackToOpenLibrary(t *testing.T) {
	provider := &fakeProvider{hits: []metadata.Book{
		{ID: "OL9W", Title: "A Fire Upon the Deep", Authors: []string{"Vernor Vinge"}},
	}}
	files := []models.MediaFile{
		plainAudio("Vernor Vinge/A Fire Upon the Deep/01.mp3"),
		plainAudio("Vernor Vinge/A Fire Upon the Deep/02.mp3"),
		plainAudio("Neal Stephenson/Anathem/01.mp3"),
		plainAudio("Neal Stephenson/Anathem/02.mp3"),
	}
	cs := matchCandidates(t, provider, testLibrary, files)

	vinge := findCandidate(t, cs, "audio:/nas:Vernor Vinge/A Fire Upon the Deep")
	if len(vinge.Suggestions) == 0 {
		t.Fatal("no suggestions for the unknown book")
	}
	top := vinge.Suggestions[0]
	if top.Book.ID != "OL9W" || top.Source != SourceOpenLibrary || top.InLibrary {
		t.Errorf("provider suggestion = %+v", top)
	}
	if !vinge.HighConfidence {
		t.Errorf("dir+author provider match not high confidence: %+v", vinge.Suggestions)
	}

	anathem := findCandidate(t, cs, "audio:/nas:Neal Stephenson/Anathem")
	if top := anathem.Suggestions[0]; top.Source != SourceLibrary {
		t.Errorf("library candidate lost to the provider: %+v", top)
	}
}

// TestMatchIgnoresAndThresholds: ignored files vanish from the pass, and a
// strong title with a disagreeing author stays below the bar.
func TestMatchIgnoresAndThresholds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	userID := testUser(t, st)
	for _, b := range testLibrary {
		if err := st.UpsertBook(ctx, b, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := st.AddBookEntry(ctx, userID, b.ID, nil, models.StatusBacklog); err != nil {
			t.Fatalf("entry: %v", err)
		}
	}
	files := []models.MediaFile{
		plainAudio("Andy Weir/Dune.m4b"), // author disagrees with the title
		plainAudio("Frank Herbert/Mystery Book/01.mp3"),
		plainAudio("Frank Herbert/Mystery Book/02.mp3"),
	}
	if err := st.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Ignore the mystery directory before matching.
	mysteryIDs := make([]int64, 0, 2)
	rows, err := st.DB().QueryContext(ctx, `SELECT id FROM media_files WHERE path LIKE 'Frank Herbert/Mystery Book/%'`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		mysteryIDs = append(mysteryIDs, id)
	}
	rows.Close()
	if len(mysteryIDs) != 2 {
		t.Fatalf("found %d mystery ids", len(mysteryIDs))
	}
	if n, err := st.IgnoreMediaFiles(ctx, userID, mysteryIDs); err != nil || n != 2 {
		t.Fatalf("ignore = (%d, %v)", n, err)
	}

	m := NewMatcher(st, nil)
	cs, err := m.Candidates(ctx, userID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d candidates after ignore, want 1: %v", len(cs), keysOf(cs))
	}
	c := cs[0]
	if c.TitleGuess != "Dune" || c.AuthorGuess != "Andy Weir" {
		t.Fatalf("signal = (%q, %q)", c.TitleGuess, c.AuthorGuess)
	}
	if c.HighConfidence {
		t.Errorf("author mismatch cleared the confidence bar: %+v", c.Suggestions)
	}
	if top := c.Suggestions[0]; top.Book.ID != "OL3W" || top.Confidence >= HighConfidence {
		t.Errorf("top = %+v, want Dune below the bar", top)
	}
}
