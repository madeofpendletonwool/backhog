package media

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
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
	return matchCandidatesWith(t, provider, library, files, nil)
}

// matchCandidatesWith is matchCandidates plus the parsed .opf inventory the
// scanner would have recorded for the same tree.
func matchCandidatesWith(t *testing.T, provider metadata.BookProvider, library []metadata.Book,
	files []models.MediaFile, sidecars []models.MediaSidecar) []Candidate {
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
	byRoot := map[string][]models.MediaSidecar{}
	for _, car := range sidecars {
		byRoot[car.Root] = append(byRoot[car.Root], car)
	}
	for root, cars := range byRoot {
		if err := st.ReplaceMediaSidecars(ctx, root, cars); err != nil {
			t.Fatalf("insert sidecars: %v", err)
		}
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

// TestCandidatesMarshalEmptySuggestions: a candidate nobody matched must
// serialize "suggestions": [], never null — the client indexes
// suggestions[0] unguarded and a null blanks the whole review page.
func TestCandidatesMarshalEmptySuggestions(t *testing.T) {
	// A library that explains nothing, a provider that offers nothing:
	// every candidate comes back with zero suggestions.
	cs := matchCandidates(t, &fakeProvider{}, testLibrary,
		[]models.MediaFile{epubFile("zzz unrelated book.epub")})
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1: %v", len(cs), keysOf(cs))
	}
	encoded, err := json.Marshal(cs[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe struct {
		Suggestions []Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe.Suggestions == nil {
		t.Errorf("suggestions = null in %s; want []", encoded)
	}
}

// countingProvider counts searches so a test can prove the matcher's
// memoization keeps repeat candidates requests off the provider. The count
// is atomic: the request's inline searches and the background worker's
// both touch it.
type countingProvider struct {
	fakeProvider
	searches atomic.Int32
}

func (c *countingProvider) Search(_ context.Context, _ string, _ int) ([]metadata.Book, error) {
	c.searches.Add(1)
	return nil, nil
}

// TestCandidatesMemoizeProviderSearches: candidates requests are budgeted,
// so the same query on a second visit must come from the cache — otherwise
// every page load re-spends the whole Open Library budget on the same work.
func TestCandidatesMemoizeProviderSearches(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	userID := testUser(t, st)
	files := []models.MediaFile{
		plainAudio("Stephen King/1974 - Carrie/01.mp3"),
		plainAudio("Stephen King/1974 - Carrie/02.mp3"),
	}
	if err := st.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert files: %v", err)
	}
	provider := &countingProvider{}
	m := NewMatcher(st, provider)
	defer m.Close()

	for range 2 {
		if _, err := m.Candidates(ctx, userID); err != nil {
			t.Fatalf("candidates: %v", err)
		}
	}
	if got := provider.searches.Load(); got != 1 {
		t.Errorf("provider searched %d times across 2 candidates runs, want 1", got)
	}
}

// TestCandidatesGroupMultiDiscRip: a physical audiobook spanned across
// "Disc 1".."Disc n" directories is one book, so it must arrive as one
// candidate in disc order — not one candidate per platter.
func TestCandidatesGroupMultiDiscRip(t *testing.T) {
	files := []models.MediaFile{
		plainAudio("2005 - Dreams From My Father/Disc 1/01 Track.mp3"),
		plainAudio("2005 - Dreams From My Father/Disc 1/02 Track.mp3"),
		plainAudio("2005 - Dreams From My Father/Disc 10/01 Track.mp3"),
		plainAudio("2005 - Dreams From My Father/Disc 2/01 Track.mp3"),
	}
	cs := matchCandidates(t, nil, nil, files)
	if len(cs) != 1 {
		t.Fatalf("got %d candidates for one multi-disc book, want 1: %v", len(cs), keysOf(cs))
	}
	c := cs[0]
	if c.DirPath != "2005 - Dreams From My Father" {
		t.Errorf("dir_path = %q, want the disc folders folded into the book directory", c.DirPath)
	}
	want := []string{
		"2005 - Dreams From My Father/Disc 1/01 Track.mp3",
		"2005 - Dreams From My Father/Disc 1/02 Track.mp3",
		"2005 - Dreams From My Father/Disc 2/01 Track.mp3",
		"2005 - Dreams From My Father/Disc 10/01 Track.mp3",
	}
	if got := pathsOf(c); !slicesEqual(got, want) {
		t.Errorf("track order = %v, want %v", got, want)
	}
}

// TestTitleScoreIgnoresStopwords: with "the" counted as a token, any two
// "The X" titles overlap, and a shared author made every Discworld book
// propose The Colour of Magic. The stopword filter must zero that noise
// while real near-matches keep scoring.
func TestTitleScoreIgnoresStopwords(t *testing.T) {
	if s := titleScore("The Truth", "The Colour of Magic"); s != 0 {
		t.Errorf("the-only overlap scored %.2f, want 0", s)
	}
	if s := titleScore("A Hat Full of Sky", "The Wee Free Men"); s != 0 {
		t.Errorf("stopword-only overlap scored %.2f, want 0", s)
	}
	if s := titleScore("Travels with Charley", "Travels with Charley in Search of America"); s < 0.3 {
		t.Errorf("real near-match scored %.2f, want >= 0.3", s)
	}
	if s := titleScore("The Truth", "The Truth"); s != 1 {
		t.Errorf("identical titles scored %.2f, want 1", s)
	}
}

// TestCandidatesEnqueueBackgroundSearches: one candidates request answers
// inline for at most a few uncached queries and queues the rest; the
// background worker must fill those in without any request waiting on it.
// Every query ends up searched exactly once — inline or by the worker,
// never both.
func TestCandidatesEnqueueBackgroundSearches(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	userID := testUser(t, st)
	var files []models.MediaFile
	for _, q := range []string{"Alpha One", "Bravo One", "Charlie One", "Delta One", "Echo One"} {
		files = append(files, taggedAudio(fmt.Sprintf("%s/%s.mp3", q, q), q, "Author", q, 1))
	}
	if err := st.InsertMediaFiles(ctx, files); err != nil {
		t.Fatalf("insert files: %v", err)
	}
	provider := &countingProvider{}
	m := NewMatcher(st, provider)
	defer m.Close()

	if _, err := m.Candidates(ctx, userID); err != nil {
		t.Fatalf("candidates: %v", err)
	}

	// All five queries end up cached — the four the request could afford
	// inline, plus the queued fifth via the worker.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cached := 0
		for _, q := range []string{"Alpha One", "Bravo One", "Charlie One", "Delta One", "Echo One"} {
			if _, ok := m.lookupSearch(q + " Author"); ok {
				cached++
			}
		}
		if cached == 5 {
			if got := provider.searches.Load(); got != 5 {
				t.Errorf("provider searched %d times for 5 queries, want exactly 5", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("background worker never enriched every queued query; searches=%d", provider.searches.Load())
}

func slicesEqual(a, b []string) bool {
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

// TestSignalFromAuthorYearTitleFilenames: the "Author - YYYY - Title" and
// "Author - Series NN - Title" ebook conventions used to leave the author
// and series junk inside the title guess, which both polluted provider
// queries and collided ("king" token) with unrelated library books.
func TestSignalFromAuthorYearTitleFilenames(t *testing.T) {
	cases := []struct {
		path       string
		wantTitle  string
		wantAuthor string
	}{
		{"Stephen King/Stephen King - 1975 - Salem's Lot.epub", "Salem's Lot", "Stephen King"},
		{"Stephen King/Stephen King - Talisman 01 - The Talisman.epub", "The Talisman", "Stephen King"},
		{"Stephen King/Stephen King - The Shining 01 - The Shining.epub", "The Shining", "Stephen King"},
		{"Stephen King/Stephen King - 1979 - The Dead Zone.epub", "The Dead Zone", "Stephen King"},
		// Two-segment "Title - Author" still works.
		{"Mort Terry Pratchett.epub", "Mort Terry Pratchett", ""},
	}
	for _, tc := range cases {
		cs := matchCandidates(t, nil, nil, []models.MediaFile{epubFile(tc.path)})
		if len(cs) != 1 {
			t.Fatalf("%s: got %d candidates, want 1", tc.path, len(cs))
		}
		if cs[0].TitleGuess != tc.wantTitle || cs[0].AuthorGuess != tc.wantAuthor {
			t.Errorf("%s: signal = (%q, %q), want (%q, %q)",
				tc.path, cs[0].TitleGuess, cs[0].AuthorGuess, tc.wantTitle, tc.wantAuthor)
		}
	}
}

// TestKingFilesDoNotMatchKingArthur: a King-named epub must never score the
// Steinbeck-edited "Acts of King Arthur" — the only overlap was the "king"
// token in the polluted title, and the authors disagree.
func TestKingFilesDoNotMatchKingArthur(t *testing.T) {
	library := []metadata.Book{{
		ID: "OL9W", Title: "The Acts of King Arthur and his Noble Knights",
		Authors: []string{"John Steinbeck"},
	}}
	cs := matchCandidates(t, &fakeProvider{}, library,
		[]models.MediaFile{epubFile("Stephen King/Stephen King - 1979 - The Long Walk.epub")})
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cs))
	}
	for _, s := range cs[0].Suggestions {
		if s.Book.ID == "OL9W" {
			t.Errorf("King Arthur matched The Long Walk at %.0f%%", s.Confidence*100)
		}
	}
}

// TestSignalFromArchiveConventions: the real-world filename shapes that
// used to defeat the splitter — series-number-left dirs, truncated names,
// double-dash archive dumps, "by Author" names, junk bucket dirs, and the
// (#NN) series marker — all resolve to the intended (title, author).
func TestSignalFromArchiveConventions(t *testing.T) {
	cases := []struct {
		path       string
		wantTitle  string
		wantAuthor string
	}{
		{
			// The filename is truncated mid-word ("The Gobl"); the
			// directory carries the full title, so the directory wins.
			"J.K Rowling/Harry Potter 4 - Harry Potter and The Goblet of Fire (123)/Harry Potter 4 - Harry Potter and The Gobl - Rowling, J.K.epub",
			"Harry Potter and The Goblet of Fire", "J K Rowling",
		},
		{
			"Matt Dinniman/Dungeon Crawler Carl by Matt Dinniman.epub",
			"Dungeon Crawler Carl", "Matt Dinniman",
		},
		{
			"old/The Iliad.epub",
			"The Iliad", "",
		},
		{
			"old/The Odyssey.epub",
			"The Odyssey", "",
		},
		{
			"Stephen King/Stephen King - Collections - 1982 - Different Seasons.epub",
			"Different Seasons", "Stephen King",
		},
		{
			"Stephen King/Stephen King - Collections - 1999 - Hearts In Atlantis.epub",
			"Hearts In Atlantis", "Stephen King",
		},
	}
	for _, tc := range cases {
		cs := matchCandidates(t, nil, nil, []models.MediaFile{epubFile(tc.path)})
		if len(cs) != 1 {
			t.Fatalf("%s: got %d candidates, want 1", tc.path, len(cs))
		}
		if cs[0].TitleGuess != tc.wantTitle || cs[0].AuthorGuess != tc.wantAuthor {
			t.Errorf("%s: signal = (%q, %q), want (%q, %q)",
				tc.path, cs[0].TitleGuess, cs[0].AuthorGuess, tc.wantTitle, tc.wantAuthor)
		}
	}
}

// TestDoubleDashArchiveName: the Anna's-Archive-style "Title -- Author --
// Publisher -- hash -- Source" name splits on double dashes; single-dash
// rules must not misfire on the fields inside.
func TestDoubleDashArchiveName(t *testing.T) {
	path := "Cory Doctorow/Enshittification_ Why Everything Suddenly Got Worse and What -- Cory Doctorow -- Farrar, Straus and Giroux -- db86e58710172cd47a5ccd4f446d8c5d -- Anna's Archive.epub"
	cs := matchCandidates(t, nil, nil, []models.MediaFile{epubFile(path)})
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cs))
	}
	if got := cs[0].TitleGuess; got != "Enshittification Why Everything Suddenly Got Worse and What" {
		t.Errorf("title = %q", got)
	}
	if got := cs[0].AuthorGuess; got != "Cory Doctorow" {
		t.Errorf("author = %q", got)
	}
}

// TestSeriesNumberedDirAuthorWalksUp: "(#38) I Shall Wear Midnight" sits
// under a "Discworld" series folder; the author is one level above that,
// and the ordinal marker is not part of the title.
func TestSeriesNumberedDirAuthorWalksUp(t *testing.T) {
	files := []models.MediaFile{
		plainAudio("Terry Pratchett/Discworld/(#38) I Shall Wear Midnight/01.mp3"),
		plainAudio("Terry Pratchett/Discworld/(#38) I Shall Wear Midnight/02.mp3"),
	}
	cs := matchCandidates(t, nil, nil, files)
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cs))
	}
	if got := cs[0].TitleGuess; got != "I Shall Wear Midnight" {
		t.Errorf("title = %q", got)
	}
	if got := cs[0].AuthorGuess; got != "Terry Pratchett" {
		t.Errorf("author = %q, want the author above the series folder", got)
	}
}

// TestAnthologyDemotedAgainstRealBook: a slash-listed omnibus ("The Dark
// Tower (Gunslinger / ... / Waste Lands / ...)") may contain the real
// book's tokens without being it; the omnibus must lose to any title that
// actually names the candidate, and never clear the confidence bar alone.
func TestAnthologyDemotedAgainstRealBook(t *testing.T) {
	sig := signal{title: "The Waste Lands", author: "Stephen King", weight: 0.8, source: SourceSignalFile}
	omnibus := models.Book{
		ID: "OLOMN", Authors: []string{"Stephen King"},
		Title: "The Dark Tower (Gunslinger / Drawing of the Three / Waste Lands / Wizard and Glass)",
	}
	real := models.Book{ID: "OLREAL", Title: "The Waste Lands", Authors: []string{"Stephen King"}}

	omnibusScore := scoreBook(sig, omnibus)
	realScore := scoreBook(sig, real)
	if omnibusScore >= HighConfidence {
		t.Errorf("omnibus scored %.2f >= the %.2f bar", omnibusScore, HighConfidence)
	}
	if omnibusScore >= realScore {
		t.Errorf("omnibus %.2f outranked the real book %.2f", omnibusScore, realScore)
	}
}

// TestTruncatedTokenMatches: a filename cut off mid-word by the ripper's
// length limit still matches the full title ("The Gobl" → "Goblet").
func TestTruncatedTokenMatches(t *testing.T) {
	if s := titleScore("Harry Potter and The Gobl", "Harry Potter and the Goblet of Fire"); s < 0.5 {
		t.Errorf("truncated title scored %.2f, want a real match", s)
	}
}

// sidecarRow is the parsed .opf the scanner would have stored for a directory.
func sidecarRow(pathStr, title, author, isbn string) models.MediaSidecar {
	return models.MediaSidecar{Root: "/nas", Path: pathStr, Title: title,
		Author: author, ISBN: isbn, SeenAt: scannedNow}
}

// A .opf beside the files outranks everything else. Here the tags, the
// directory and the filename all say the wrong book — which is exactly the
// case a Calibre library hits, where the audio was ripped under one name and
// the sidecar carries the catalogued one.
func TestSignalFromSidecarOutranksTagsAndNames(t *testing.T) {
	files := []models.MediaFile{
		taggedAudio("Misc/unsorted/track01.mp3", "Track 01", "Unknown Artist", "Rip 2009", 1),
		taggedAudio("Misc/unsorted/track02.mp3", "Track 02", "Unknown Artist", "Rip 2009", 2),
	}
	sidecars := []models.MediaSidecar{
		sidecarRow("Misc/unsorted/metadata.opf", "Anathem", "Neal Stephenson", ""),
	}
	cs := matchCandidatesWith(t, nil, testLibrary, files, sidecars)
	c := findCandidate(t, cs, "audio:/nas:Misc/unsorted")

	if c.TitleGuess != "Anathem" || c.AuthorGuess != "Neal Stephenson" {
		t.Fatalf("guess = %q by %q; want the sidecar's, not the tags'", c.TitleGuess, c.AuthorGuess)
	}
	if len(c.Suggestions) == 0 {
		t.Fatal("no suggestions from a sidecar that names a book in the library")
	}
	top := c.Suggestions[0]
	if top.Book.ID != "OL1W" || top.Signal != SourceSignalSidecar {
		t.Errorf("top suggestion = %+v; want OL1W via the sidecar signal", top)
	}
	if !c.HighConfidence {
		t.Errorf("sidecar match should clear the bulk-confirm bar: %v", top.Confidence)
	}
}

// A sidecar in the book's own directory still applies when the audio sits in
// per-disc subfolders, because both resolve through groupDir.
func TestSidecarAppliesAcrossDiscFolders(t *testing.T) {
	files := []models.MediaFile{
		plainAudio("Rips/dune-rip/CD 1/01.mp3"),
		plainAudio("Rips/dune-rip/CD 2/01.mp3"),
	}
	sidecars := []models.MediaSidecar{
		sidecarRow("Rips/dune-rip/metadata.opf", "Dune", "Frank Herbert", ""),
	}
	cs := matchCandidatesWith(t, nil, testLibrary, files, sidecars)
	c := findCandidate(t, cs, "audio:/nas:Rips/dune-rip")
	if c.TitleGuess != "Dune" {
		t.Errorf("title guess = %q; want the sidecar to reach across disc folders", c.TitleGuess)
	}
}

// Calibre's own metadata.opf wins when a directory holds more than one, so
// the same tree matches the same way on every scan.
func TestSidecarPickIsDeterministic(t *testing.T) {
	cars := []models.MediaSidecar{
		sidecarRow("Books/Dune/aaa-export.opf", "Wrong Book", "Nobody", ""),
		sidecarRow("Books/Dune/metadata.opf", "Dune", "Frank Herbert", ""),
		sidecarRow("Books/Dune/zzz-export.opf", "Also Wrong", "Nobody", ""),
	}
	byDir := sidecarsByDir(cars)
	got := byDir[sidecarKey{"/nas", "Books/Dune"}]
	if got.Title != "Dune" {
		t.Errorf("picked %q; want Calibre's metadata.opf to win", got.Title)
	}
	// And the same answer regardless of the order they arrive in.
	reversed := []models.MediaSidecar{cars[2], cars[1], cars[0]}
	if sidecarsByDir(reversed)[sidecarKey{"/nas", "Books/Dune"}].Title != "Dune" {
		t.Error("sidecar choice depends on input order")
	}
}

// isbnProvider resolves exactly one ISBN and fails every title search, so a
// test can prove a match came from the identifier and not from a query.
type isbnProvider struct {
	isbn     string
	book     metadata.Book
	searches int
}

func (p *isbnProvider) Search(_ context.Context, _ string, _ int) ([]metadata.Book, error) {
	p.searches++
	return nil, nil
}
func (p *isbnProvider) GetByWorkKey(_ context.Context, _ string) (metadata.Book, error) {
	return metadata.Book{}, fmt.Errorf("not implemented in tests")
}
func (p *isbnProvider) GetByISBN(_ context.Context, isbn string) (metadata.Book, error) {
	if isbn == p.isbn {
		return p.book, nil
	}
	return metadata.Book{}, fmt.Errorf("no such isbn %q", isbn)
}
func (p *isbnProvider) GetEditions(_ context.Context, _ string) ([]metadata.BookEdition, error) {
	return nil, fmt.Errorf("not implemented in tests")
}

// An ISBN is an identity, not a resemblance. The sidecar's title here is the
// printing's ("...: A Novel"), which scores poorly against the work title —
// and must still resolve exactly, without spending a title search.
func TestSidecarISBNResolvesExactly(t *testing.T) {
	provider := &isbnProvider{
		isbn: "9780385334204",
		book: metadata.Book{ID: "OL9W", Title: "Breakfast of Champions",
			Authors: []string{"Kurt Vonnegut"}},
	}
	files := []models.MediaFile{epubFile("Breakfast of Champions (2804)/book.epub")}
	sidecars := []models.MediaSidecar{
		sidecarRow("Breakfast of Champions (2804)/metadata.opf",
			"Breakfast of Champions: A Novel of the Nineteen Seventies",
			"Kurt Vonnegut", "9780385334204"),
	}
	cs := matchCandidatesWith(t, provider, nil, files, sidecars)
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1: %v", len(cs), keysOf(cs))
	}
	c := cs[0]
	if len(c.Suggestions) == 0 {
		t.Fatal("an ISBN that resolves should produce a suggestion")
	}
	top := c.Suggestions[0]
	if top.Book.ID != "OL9W" {
		t.Fatalf("top suggestion = %+v; want the ISBN's book", top)
	}
	if top.Confidence != 1 {
		t.Errorf("confidence = %v; an identifier match is an identity, not a score", top.Confidence)
	}
	if provider.searches != 0 {
		t.Errorf("ran %d title searches; an identified group should need none", provider.searches)
	}
}

// An epub's own OPF metadata is the same evidence as a sidecar, from inside
// the file, and beats a filename that names the wrong thing.
func TestEpubMetadataOutranksFilename(t *testing.T) {
	f := epubFile("downloads/pg0042-final-v2.epub")
	raw, err := json.Marshal(bookTags{Title: "Anathem", Authors: []string{"Neal Stephenson"}})
	if err != nil {
		t.Fatal(err)
	}
	f.ContainerMetadata = raw

	cs := matchCandidates(t, nil, testLibrary, []models.MediaFile{f})
	if len(cs) != 1 {
		t.Fatalf("got %d candidates, want 1: %v", len(cs), keysOf(cs))
	}
	c := cs[0]
	if c.TitleGuess != "Anathem" || c.AuthorGuess != "Neal Stephenson" {
		t.Fatalf("guess = %q by %q; want the epub's own metadata", c.TitleGuess, c.AuthorGuess)
	}
	if len(c.Suggestions) == 0 || c.Suggestions[0].Book.ID != "OL1W" {
		t.Errorf("suggestions = %+v; want OL1W", c.Suggestions)
	}
	if c.Suggestions[0].Signal != SourceSignalSidecar {
		t.Errorf("signal = %q; want %q", c.Suggestions[0].Signal, SourceSignalSidecar)
	}
}
