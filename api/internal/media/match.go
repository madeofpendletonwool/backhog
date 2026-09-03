package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// HighConfidence is the bar a suggestion must clear before the UI offers it
// for bulk confirmation. A wrong attachment is worse than no attachment —
// the user has to notice and undo it — so nothing attaches without a human
// look below this line.
const HighConfidence = 0.72

// Suggestion and signal sources.
const (
	SourceLibrary     = "library"
	SourceOpenLibrary = "openlibrary"
	SourceSignalTags  = "tags"
	SourceSignalDir   = "directory"
	SourceSignalFile  = "filename"
)

// Suggestion is one proposed (book, confidence) pair for a candidate.
type Suggestion struct {
	Book       models.Book `json:"book"`
	Confidence float64     `json:"confidence"`
	// Source says where the book came from: the user's own library or an
	// Open Library search.
	Source string `json:"source"`
	// Signal says which facts produced the confidence: embedded tags, the
	// directory layout, or the bare filename.
	Signal string `json:"signal"`
	// InLibrary reports whether the user already owns a copy.
	InLibrary bool `json:"in_library"`
	// EntryID is the user's library entry for the book, when they own one —
	// the attach endpoint is entry-keyed, so confirming a suggestion needs
	// no second lookup.
	EntryID string `json:"entry_id,omitempty"`
}

// Candidate is one attachable unit: an audiobook directory of ordered audio
// files, or a single EPUB.
type Candidate struct {
	// Key identifies the group stably across requests: "audio:{root}:{dir}"
	// or "epub:{fileID}".
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Root string `json:"root"`
	// DirPath is the group's directory relative to the root ("." for a
	// file sitting directly in the root).
	DirPath string `json:"dir_path"`
	// TitleGuess and AuthorGuess are the extraction the matcher worked
	// from, shown so the user can sanity-check it.
	TitleGuess  string `json:"title_guess"`
	AuthorGuess string `json:"author_guess"`
	// Files are track-ordered for audio groups.
	Files []models.MediaFile `json:"files"`
	// TotalDurationSeconds sums the audio durations that are known.
	TotalDurationSeconds float64      `json:"total_duration_seconds"`
	Suggestions          []Suggestion `json:"suggestions"`
	HighConfidence       bool         `json:"high_confidence"`
}

// Matcher proposes books for unattached media files. It scores against the
// user's own library first, then falls back to an Open Library search whose
// results are cached so confirming a suggestion needs no second fetch.
type Matcher struct {
	store *store.Store
	books metadata.BookProvider

	// searchCache memoizes provider searches by query, so repeat candidates
	// requests never re-spend Open Library's rate limit on the same work.
	searchMu    sync.Mutex
	searchCache map[string]cachedSearch
	// pending tracks queries queued or in flight so the background worker
	// never duplicates itself.
	pending map[string]bool

	// enqueue feeds the background enrichment worker; quit stops it.
	enqueue chan string
	quit    chan struct{}
	quitOne sync.Once
}

// cachedSearch is one memoized provider result. Individual book records
// rarely change once catalogued, but the catalogue itself keeps growing —
// new releases, late-added covers, metadata fixes — so entries expire
// rather than living forever.
type cachedSearch struct {
	books []models.Book
	at    time.Time
}

// Cache windows: a query that found its books can be trusted for a day.
// A query that came back empty is re-asked much sooner, because a newly
// catalogued book — the release you just got files for — turns yesterday's
// "no results" into today's match.
const (
	searchCacheTTL      = 24 * time.Hour
	emptySearchCacheTTL = 1 * time.Hour
)

const (
	// inlineSearchBudget is how many uncached queries one candidates
	// request will answer inline. A just-scanned handful of files gets
	// its matches immediately; a full-NAS run stays fast and lets the
	// background worker fill the rest in.
	inlineSearchBudget = 4
	// inlineSearchTimeout bounds the inline portion so even a slow
	// provider cannot stall the page.
	inlineSearchTimeout = 8 * time.Second
	// backgroundSearchTimeout bounds one background lookup: the shared
	// rate limiter's wait plus the fetch.
	backgroundSearchTimeout = 45 * time.Second
	// enqueueCapacity bounds the work queue; overflow is dropped and
	// re-requested by a later candidates call.
	enqueueCapacity = 1024
)

func NewMatcher(st *store.Store, books metadata.BookProvider) *Matcher {
	m := &Matcher{
		store: st, books: books,
		searchCache: map[string]cachedSearch{},
		pending:     map[string]bool{},
		enqueue:     make(chan string, enqueueCapacity),
		quit:        make(chan struct{}),
	}
	if books != nil {
		go m.worker()
	}
	return m
}

// Close stops the background enrichment worker. The queued work is simply
// dropped; a restarted process re-derives it from the next candidates call.
func (m *Matcher) Close() {
	m.quitOne.Do(func() { close(m.quit) })
}

// worker drains the enrichment queue serially — the provider's rate limiter
// serializes the requests anyway, and one worker makes the pacing obvious.
func (m *Matcher) worker() {
	for {
		select {
		case <-m.quit:
			return
		case query := <-m.enqueue:
			m.searchInBackground(query)
		}
	}
}

// searchInBackground answers one queued query, off any request's clock.
func (m *Matcher) searchInBackground(query string) {
	defer m.clearPending(query)
	if _, ok := m.lookupSearch(query); ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundSearchTimeout)
	defer cancel()
	m.fetchAndCache(ctx, query)
}

// Candidates runs the auto-match pass for one user: unattached, present
// files grouped into audiobook directories and single EPUBs, each with a
// ranked suggestion list.
func (m *Matcher) Candidates(ctx context.Context, userID string) ([]Candidate, error) {
	files, err := m.store.ListMediaFiles(ctx, store.MediaFileFilter{Unattached: true})
	if err != nil {
		return nil, err
	}
	ignored, err := m.store.IgnoredMediaFileIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	entries, err := m.store.ListEntries(ctx, userID, store.LibraryFilter{MediaType: models.MediaBook})
	if err != nil {
		return nil, err
	}
	owned := make([]models.Book, 0, len(entries))
	ownedIDs := map[string]bool{}
	entryIDs := map[string]string{}
	for _, e := range entries {
		if e.Book != nil && !ownedIDs[e.Book.ID] {
			ownedIDs[e.Book.ID] = true
			owned = append(owned, *e.Book)
		}
		if e.Book != nil {
			entryIDs[e.Book.ID] = e.ID
		}
	}

	groups := groupCandidates(files, ignored)
	for i := range groups {
		scoreAgainst(&groups[i], owned, ownedIDs, entryIDs)
	}

	// Open Library only for the candidates the user's own library cannot
	// confidently explain. Cached searches apply instantly; a few uncached
	// ones are answered inline so a freshly scanned book matches on the
	// spot; anything beyond that is queued for the background worker and
	// appears on a later refresh — the request itself never waits on the
	// provider's rate limit.
	if m.books != nil {
		inlineCtx, cancel := context.WithTimeout(ctx, inlineSearchTimeout)
		defer cancel()
		inline := inlineSearchBudget
		for i := range groups {
			g := &groups[i]
			if topClearsLibrary(*g) {
				continue
			}
			query := g.searchQuery()
			if query == "" {
				continue
			}
			if cached, ok := m.lookupSearch(query); ok {
				g.providerBooks = append(g.providerBooks, cached...)
			} else if inline > 0 {
				inline--
				fetched := m.fetchAndCache(inlineCtx, query)
				g.providerBooks = append(g.providerBooks, fetched...)
			} else {
				m.enqueueSearch(query)
				continue
			}
			scoreAgainst(g, owned, ownedIDs, entryIDs)
		}
	}

	sortGroups(groups)
	return groupsToCandidates(groups), nil
}

// topClearsLibrary reports whether the group already has a library
// suggestion good enough that asking Open Library would add nothing.
func topClearsLibrary(g group) bool {
	for _, s := range g.suggestions {
		if s.Source == SourceLibrary && s.Confidence >= HighConfidence {
			return true
		}
	}
	return false
}

// fetchAndCache runs one provider search and memoizes it. Failures degrade
// to whatever suggestions already exist; only a run that finished inside its
// budget is cached, so a partial fetch is never mistaken for a complete one.
func (m *Matcher) fetchAndCache(ctx context.Context, query string) []models.Book {
	results, err := m.books.Search(ctx, query, 8)
	if err != nil {
		// The rate limiter turning a request away is the steady state of
		// a busy matcher, not a malfunction worth a warn per query.
		if ctx.Err() != nil {
			slog.DebugContext(ctx, "media match search out of time", "query", query, "error", err)
		} else {
			slog.WarnContext(ctx, "media match search failed", "query", query, "error", err)
		}
		return nil
	}
	var fetched []models.Book
	for _, b := range results {
		if err := m.store.UpsertBook(ctx, b, ""); err != nil {
			slog.WarnContext(ctx, "media match cache book", "book_id", b.ID, "error", err)
			continue
		}
		if book, err := m.store.GetBook(ctx, b.ID); err == nil {
			fetched = append(fetched, book)
		}
	}
	if ctx.Err() == nil {
		m.saveSearch(query, fetched)
	}
	return fetched
}

// enqueueSearch asks the background worker to look the query up. Queries
// already queued or in flight are skipped; a dropped query (worker gone or
// queue full) simply gets re-requested by a later candidates call.
func (m *Matcher) enqueueSearch(query string) {
	m.searchMu.Lock()
	if m.pending[query] {
		m.searchMu.Unlock()
		return
	}
	m.pending[query] = true
	m.searchMu.Unlock()
	select {
	case m.enqueue <- query:
	case <-m.quit:
		m.clearPending(query)
	default:
		m.clearPending(query)
	}
}

// clearPending marks a query as no longer in flight.
func (m *Matcher) clearPending(query string) {
	m.searchMu.Lock()
	delete(m.pending, query)
	m.searchMu.Unlock()
}

// lookupSearch returns a memoized provider search, if one is fresh enough.
func (m *Matcher) lookupSearch(query string) ([]models.Book, bool) {
	m.searchMu.Lock()
	defer m.searchMu.Unlock()
	cached, ok := m.searchCache[query]
	if !ok {
		return nil, false
	}
	ttl := searchCacheTTL
	if len(cached.books) == 0 {
		ttl = emptySearchCacheTTL
	}
	if time.Since(cached.at) > ttl {
		return nil, false
	}
	return cached.books, true
}

// saveSearch memoizes a provider search, empty results included — the
// catalogue grows, so an empty answer is only trusted for the short window.
func (m *Matcher) saveSearch(query string, books []models.Book) {
	m.searchMu.Lock()
	defer m.searchMu.Unlock()
	m.searchCache[query] = cachedSearch{books: books, at: time.Now()}
}

// --- grouping ---------------------------------------------------------------

// group is a candidate under construction.
type group struct {
	key     string
	kind    string
	root    string
	dirPath string
	files   []models.MediaFile
	signal  signal

	providerBooks []models.Book
	suggestions   []Suggestion
}

// signal is what the files say about themselves: a title/author extraction
// plus the weight and provenance of the evidence it came from.
type signal struct {
	title  string
	author string
	weight float64
	source string // SourceSignalTags | SourceSignalDir | SourceSignalFile
}

// discDirPattern matches the per-platter directory names rippers produce:
// "Disc 1", "CD 2", "disk 03", "Part 1".
var discDirPattern = regexp.MustCompile(`(?i)^(disc|disk|cd|part|volume)[\s._-]*\d{1,3}$`)

// groupDir canonicalises the directory a group is keyed on: trailing
// disc/volume folders ("Book/CD 3", "Book/Disc 2/Part 1") fold up into the
// book's own directory, because one rip of one audiobook is one candidate
// no matter how many platters it spans.
func groupDir(dir string) string {
	for {
		base := path.Base(dir)
		if !discDirPattern.MatchString(base) {
			return dir
		}
		parent := path.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// groupCandidates splits unattached files into candidates: audio files
// sharing a directory become one ordered audiobook; every EPUB stands
// alone. Ignored files drop out before grouping, so a fully-ignored
// directory disappears. Groups come back in a deterministic order.
func groupCandidates(files []models.MediaFile, ignored map[int64]bool) []group {
	type key struct{ root, dir string }
	audioDirs := map[key][]models.MediaFile{}
	var epubs []models.MediaFile

	for _, f := range files {
		if ignored[f.ID] {
			continue
		}
		if f.Kind == models.MediaFileAudio {
			k := key{f.Root, groupDir(path.Dir(f.Path))}
			audioDirs[k] = append(audioDirs[k], f)
		} else {
			epubs = append(epubs, f)
		}
	}

	dirs := make([]key, 0, len(audioDirs))
	for k := range audioDirs {
		dirs = append(dirs, k)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].root != dirs[j].root {
			return dirs[i].root < dirs[j].root
		}
		return dirs[i].dir < dirs[j].dir
	})
	sort.Slice(epubs, func(i, j int) bool {
		if epubs[i].Root != epubs[j].Root {
			return epubs[i].Root < epubs[j].Root
		}
		return epubs[i].Path < epubs[j].Path
	})

	groups := make([]group, 0, len(dirs)+len(epubs))
	for _, k := range dirs {
		groups = append(groups, group{
			key: "audio:" + k.root + ":" + k.dir, kind: models.MediaFileAudio,
			root: k.root, dirPath: k.dir, files: audioDirs[k],
		})
	}
	for _, f := range epubs {
		groups = append(groups, group{
			key: fmt.Sprintf("epub:%d", f.ID), kind: models.MediaFileEpub,
			root: f.Root, dirPath: path.Dir(f.Path), files: []models.MediaFile{f},
		})
	}

	for i := range groups {
		g := &groups[i]
		orderTracks(g.files)
		g.signal = extractSignal(g)
	}
	return groups
}

// orderTracks sorts an audio group's files into listening order: tag track
// numbers when every file carries one — with the natural path order as
// tiebreaker, because a merged multi-disc rip restarts at track 1 on every
// disc — otherwise natural sort on the full path, so disc 1 lands before
// disc 10 before disc 2.
func orderTracks(files []models.MediaFile) {
	tracks := make([]int, len(files))
	allTagged := len(files) > 0
	for i := range files {
		tracks[i] = tagTrack(files[i].ContainerMetadata)
		if tracks[i] <= 0 {
			allTagged = false
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if allTagged && tracks[i] != tracks[j] {
			return tracks[i] < tracks[j]
		}
		return naturalLess(files[i].Path, files[j].Path)
	})
}

// tagTrack reads the track number from a file's container metadata JSON.
func tagTrack(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var tags struct {
		Track int `json:"track"`
	}
	if err := json.Unmarshal(raw, &tags); err != nil {
		return 0
	}
	return tags.Track
}

// naturalLess compares strings in human order: digit runs compare
// numerically, everything else byte-wise, case-insensitively.
func naturalLess(a, b string) bool {
	for len(a) > 0 && len(b) > 0 {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, ra := leadingNumber(a)
			nb, rb := leadingNumber(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		ca, cb := lowerByte(a[0]), lowerByte(b[0])
		if ca != cb {
			return ca < cb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func leadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s[i:]
	}
	return n, s[i:]
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// --- signals ----------------------------------------------------------------

var (
	bracketPattern      = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	leadingTrackPattern = regexp.MustCompile(`^\s*\d{1,3}\s[-._]*\s*`)
	// leadingYearPattern strips a copyright-year prefix ("1975 - Salem's
	// Lot") — four digits, so the two-digit decade-prefix books of the
	// filename conventions this handles never collide with a real title.
	leadingYearPattern = regexp.MustCompile(`^\s*\d{4}\s*[-–—.]\s*`)
	sepPattern         = regexp.MustCompile(`[\s._]+`)
	titleAuthorSplit   = regexp.MustCompile(`\s+[-–—]\s+`)
	// seriesMarkerPattern matches the middle segment of the "Author -
	// Series NN - Title" convention: anything ending in a small number
	// ("Talisman 01", "Discworld 20").
	seriesMarkerPattern = regexp.MustCompile(`^.+\s+\d{1,3}$`)
	yearOnlyPattern     = regexp.MustCompile(`^\d{4}$`)
)

// extractSignal pulls (title, author) from a group, in priority order.
// Embedded tags win when present — rippers are usually careful with them;
// the /Author Name/Book Title directory layout is the near-universal
// audiobook convention; the bare filename is the weakest evidence.
func extractSignal(g *group) signal {
	if g.kind == models.MediaFileAudio {
		if s, ok := signalFromTags(g.files); ok {
			return s
		}
	}
	parts := dirParts(g.dirPath)
	fileTitle, fileAuthor := splitTitleAuthor(cleanBase(path.Base(g.files[0].Path)))

	if g.kind == models.MediaFileAudio && len(g.files) > 1 {
		// A directory of numbered parts: the directory names the book.
		// Directory names use the same "Author - ... - Title" conventions
		// as filenames, so parse them the same way.
		s := signal{weight: 0.9, source: SourceSignalDir}
		if len(parts) > 0 {
			dirTitle, dirNameAuthor := splitTitleAuthor(cleanTitle(parts[len(parts)-1]))
			s.title = dirTitle
			if len(parts) >= 2 {
				s.author = cleanAuthor(parts[len(parts)-2])
			}
			if s.author == "" {
				s.author = dirNameAuthor
			}
		}
		if s.title == "" {
			s.title, s.author = fileTitle, fileAuthor
			s.weight, s.source = 0.65, SourceSignalFile
		}
		return s
	}

	// Single audio file, or an epub. /Author/Book.ext is the universal
	// single-file layout, so a lone directory level is the author and the
	// filename is the title — unless the filename echoes the directory,
	// in which case the directory is the title nested one level deeper.
	if len(parts) >= 2 {
		dirTitle, dirNameAuthor := splitTitleAuthor(cleanTitle(parts[len(parts)-1]))
		dirAuthor := firstNonEmpty(cleanAuthor(parts[len(parts)-2]), dirNameAuthor)
		if fileTitle == "" || titleScore(fileTitle, dirTitle) >= 0.9 {
			return signal{title: dirTitle, author: dirAuthor, weight: 0.9, source: SourceSignalDir}
		}
		return signal{title: fileTitle, author: firstNonEmpty(dirAuthor, fileAuthor),
			weight: 0.8, source: SourceSignalFile}
	}
	if len(parts) == 1 {
		only := cleanTitle(parts[0])
		if fileTitle != "" && titleScore(fileTitle, only) >= 0.9 {
			// /Book Title/Book.m4b — the directory names the book.
			return signal{title: only, weight: 0.8, source: SourceSignalDir}
		}
		// /Author/Book.m4b — the directory names the author.
		return signal{title: fileTitle, author: firstNonEmpty(only, fileAuthor),
			weight: 0.8, source: SourceSignalFile}
	}
	weight := 0.65
	if fileAuthor != "" {
		weight = 0.75
	}
	return signal{title: fileTitle, author: fileAuthor, weight: weight, source: SourceSignalFile}
}

// signalFromTags prefers the album (the book) over the per-track title (a
// chapter); the author comes from album artist, artist or composer, in that
// order — narrator tags land in those fields in practice.
func signalFromTags(files []models.MediaFile) (signal, bool) {
	var title, author string
	for _, f := range files {
		if len(f.ContainerMetadata) == 0 {
			continue
		}
		var tags audioTags
		if err := json.Unmarshal(f.ContainerMetadata, &tags); err != nil {
			continue
		}
		if title == "" {
			title = firstNonEmpty(tags.Album, tags.Title)
		}
		if author == "" {
			author = firstNonEmpty(tags.AlbumArtist, tags.Artist, tags.Composer)
		}
	}
	if title == "" {
		return signal{}, false
	}
	return signal{title: cleanTitle(title), author: cleanAuthor(author),
		weight: 1.0, source: SourceSignalTags}, true
}

// dirParts splits a root-relative directory into components, dropping the
// "." a root-level file carries.
func dirParts(dir string) []string {
	if dir == "." || dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cleanBase strips extension, leading track numbers and years, and
// bracketed junk from a filename.
func cleanBase(name string) string {
	name = strings.TrimSuffix(name, path.Ext(name))
	name = leadingTrackPattern.ReplaceAllString(name, "")
	name = leadingYearPattern.ReplaceAllString(name, "")
	return cleanTitle(name)
}

// cleanTitle normalises a candidate title: drop bracketed annotations
// ("(Unabridged)", "[2008]"), collapse separator runs, trim.
func cleanTitle(s string) string {
	s = bracketPattern.ReplaceAllString(s, "")
	s = sepPattern.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// cleanAuthor flattens an author string the same way.
func cleanAuthor(s string) string {
	return cleanTitle(s)
}

// splitTitleAuthor recognises the filename conventions rippers actually use:
//
//   - "Title - Author"        — one separator, right side a real name.
//   - "Author - YYYY - Title" — the year-in-the-middle ebook convention.
//   - "Author - Series NN - Title" — "Stephen King - Talisman 01 - The
//     Talisman", where the middle segment is a series marker, not title.
//
// Anything else stays one lump — a wrong guess here pollutes every query
// and match downstream, so certainty is worth more than coverage.
func splitTitleAuthor(s string) (title, author string) {
	parts := titleAuthorSplit.Split(s, -1)
	switch len(parts) {
	case 2:
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left == "" || right == "" {
			return s, ""
		}
		for _, word := range strings.Fields(right) {
			if len(word) >= 3 && hasLetter(word) {
				return left, right
			}
		}
		return s, ""
	case 3:
		left := strings.TrimSpace(parts[0])
		middle := strings.TrimSpace(parts[1])
		right := strings.TrimSpace(parts[2])
		if left == "" || middle == "" || right == "" {
			return s, ""
		}
		if yearOnlyPattern.MatchString(middle) || seriesMarkerPattern.MatchString(middle) {
			if hasLetter(left) && hasLetter(right) {
				return right, left
			}
		}
		return s, ""
	}
	return s, ""
}

func hasLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// searchQuery builds the Open Library query from the best signal.
func (g *group) searchQuery() string {
	return strings.TrimSpace(g.signal.title + " " + g.signal.author)
}

// --- scoring ----------------------------------------------------------------

// normalizeMatch folds a string for comparison: lowercase, letters and
// digits only, single spaces.
func normalizeMatch(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// scoringStopwords are tokens that make any two English titles look alike.
// With "the" counted, "The Truth" and "The Colour of Magic" share a token,
// and one shared author later every Discworld book proposes every other.
// Scoring ignores them; matching on them is noise, not evidence.
var scoringStopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "of": {}, "and": {}, "or": {},
	"in": {}, "on": {}, "to": {}, "for": {}, "with": {}, "from": {},
	"at": {}, "by": {},
}

// tokens splits a normalized string into a set, minus stopwords.
func tokens(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, t := range strings.Fields(s) {
		if _, stop := scoringStopwords[t]; stop {
			continue
		}
		set[t] = struct{}{}
	}
	return set
}

// titleScore rates how well two titles agree, 0..1.
func titleScore(a, b string) float64 {
	na, nb := normalizeMatch(a), normalizeMatch(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	ta, tb := tokens(na), tokens(nb)
	if len(ta) == 0 || len(tb) == 0 {
		// A title that reduces to stopwords ("The") has nothing to
		// distinguish it by — same as no title at all.
		return 0
	}
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	if inter == 0 {
		return 0
	}
	// Containment divides by the *smaller* set: "dune" inside "dune 40th
	// anniversary edition" fully covers one side, which is the strong
	// near-match signal — not the weaker max-length overlap.
	minLen := min(len(ta), len(tb))
	overlap := float64(inter) / float64(minLen)
	jaccard := float64(inter) / float64(len(ta)+len(tb)-inter)
	if overlap == 1 {
		return 0.9
	}
	return 0.8*overlap + 0.2*jaccard
}

// authorScore rates author agreement, 0..1.
func authorScore(a, b string) float64 {
	na, nb := normalizeMatch(a), normalizeMatch(b)
	if na == "" || nb == "" {
		return 0
	}
	if strings.Contains(na, nb) || strings.Contains(nb, na) {
		return 1
	}
	ta, tb := tokens(na), tokens(nb)
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	if inter == 0 {
		return 0
	}
	return float64(inter) / float64(max(len(ta), len(tb)))
}

// scoreBook computes the confidence that a candidate's signal points at
// this book: signal weight × title agreement, adjusted by author agreement
// when both sides know an author (a mismatch drags hard — the classic
// wrong-book trap).
func scoreBook(sig signal, b models.Book) float64 {
	ts := titleScore(sig.title, b.Title)
	if ts == 0 {
		return 0
	}
	conf := sig.weight * ts
	var as float64
	known := false
	if sig.author != "" && len(b.Authors) > 0 {
		known = true
		for _, a := range b.Authors {
			if s := authorScore(sig.author, a); s > as {
				as = s
			}
		}
	}
	switch {
	case known && as >= 0.5:
		conf *= 0.7 + 0.3*as
	case known:
		conf *= 0.4
	default:
		// No author evidence on one side: mild uncertainty.
		conf *= 0.85
	}
	if conf > 1 {
		conf = 1
	}
	return conf
}

// scoreAgainst replaces a group's suggestions with a fresh ranking against
// the owned library plus any provider results collected so far. Library
// matches carry a small ownership bonus: the user already owning the exact
// book is evidence.
func scoreAgainst(g *group, owned []models.Book, ownedIDs map[string]bool, entryIDs map[string]string) {
	byID := map[string]*Suggestion{}
	add := func(b models.Book, source string, bonus float64) {
		conf := scoreBook(g.signal, b)
		if conf == 0 {
			return
		}
		if conf += bonus; conf > 1 {
			conf = 1
		}
		if existing, ok := byID[b.ID]; ok {
			if conf > existing.Confidence {
				existing.Confidence = conf
			}
			if source == SourceLibrary {
				existing.Source = SourceLibrary
			}
			return
		}
		byID[b.ID] = &Suggestion{Book: b, Confidence: conf, Source: source,
			Signal: g.signal.source, InLibrary: ownedIDs[b.ID], EntryID: entryIDs[b.ID]}
	}
	for _, b := range owned {
		add(b, SourceLibrary, 0.05)
	}
	for _, b := range g.providerBooks {
		add(b, SourceOpenLibrary, 0)
	}

	g.suggestions = g.suggestions[:0]
	for _, s := range byID {
		g.suggestions = append(g.suggestions, *s)
	}
	sort.Slice(g.suggestions, func(i, j int) bool {
		if g.suggestions[i].Confidence != g.suggestions[j].Confidence {
			return g.suggestions[i].Confidence > g.suggestions[j].Confidence
		}
		return g.suggestions[i].Book.Title < g.suggestions[j].Book.Title
	})
	if len(g.suggestions) > 3 {
		g.suggestions = g.suggestions[:3]
	}
}

// sortGroups presents the most confident candidates first, then audio
// groups (the big wins) before epubs, then by path.
func sortGroups(groups []group) {
	rank := func(g group) float64 {
		if len(g.suggestions) > 0 {
			return g.suggestions[0].Confidence
		}
		return -1
	}
	sort.SliceStable(groups, func(i, j int) bool {
		ri, rj := rank(groups[i]), rank(groups[j])
		if ri != rj {
			return ri > rj
		}
		if groups[i].kind != groups[j].kind {
			return groups[i].kind == models.MediaFileAudio
		}
		if groups[i].dirPath != groups[j].dirPath {
			return groups[i].dirPath < groups[j].dirPath
		}
		return groups[i].key < groups[j].key
	})
}

func groupsToCandidates(groups []group) []Candidate {
	out := make([]Candidate, 0, len(groups))
	for _, g := range groups {
		// A group nobody matched carries a nil slice, which JSON renders
		// as null — and a null where the client expects an array is a
		// crash. Empty means "no suggestions"; null means "no field".
		suggestions := g.suggestions
		if suggestions == nil {
			suggestions = []Suggestion{}
		}
		c := Candidate{
			Key: g.key, Kind: g.kind, Root: g.root, DirPath: g.dirPath,
			TitleGuess: g.signal.title, AuthorGuess: g.signal.author,
			Files: g.files, Suggestions: suggestions,
		}
		for _, f := range g.files {
			if f.DurationSeconds != nil {
				c.TotalDurationSeconds += *f.DurationSeconds
			}
		}
		c.HighConfidence = len(suggestions) > 0 && suggestions[0].Confidence >= HighConfidence
		out = append(out, c)
	}
	return out
}
