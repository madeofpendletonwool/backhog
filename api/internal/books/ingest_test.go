package books

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// newTestStore opens a fully migrated store over a temp database.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "books.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(database)
}

// writeEpubZip writes zip bytes to a file under root and returns the path.
func writeEpubZip(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}
	return p
}

// buildZip builds an in-memory zip from name → content pairs.
func buildZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatalf("create %s: %v", e[0], err)
		}
		if _, err := io.WriteString(w, e[1]); err != nil {
			t.Fatalf("write %s: %v", e[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// fixtureNCX is a chaptered book with an image-only cover page, prose full
// of normalization hazards, and an NCX TOC.
func fixtureNCX(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c0" href="cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c0"/><itemref idref="c1"/><itemref idref="c2"/>
  </spine>
</package>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint><navLabel><text>One</text></navLabel><content src="ch1.xhtml"/></navPoint>
    <navPoint><navLabel><text>Two</text></navLabel><content src="ch2.xhtml"/></navPoint>
  </navMap>
</ncx>`},
		{"OEBPS/cover.xhtml", `<html><body><div><img src="cover.png"/></div></body></html>`},
		{"OEBPS/ch1.xhtml", `<html><head><style>.x{}</style><script>bad();</script></head>
<body><h1>Uno</h1><p>“It’s ﬁne,” he said—mostly.</p><p>   </p><p>Second block.</p></body></html>`},
		{"OEBPS/ch2.xhtml", `<html><body><p>Chapter two: twenty-three—things…</p></body></html>`},
	})
}

// fixtureNav is the same book shaped as EPUB 3: nav document TOC, no NCX,
// and a document whose every block normalizes away.
func fixtureNav(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, [][2]string{
		{"META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{"content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
    <item id="c" href="c.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="a"/><itemref idref="b"/><itemref idref="c"/></spine>
</package>`},
		{"nav.xhtml", `<html><body><nav epub:type="toc"><ol>
		  <li><a href="a.xhtml">Alpha</a></li>
		  <li><a href="c.xhtml">Gamma</a></li>
		</ol></nav></body></html>`},
		{"a.xhtml", `<html><body><p>Alpha text here.</p></body></html>`},
		{"b.xhtml", `<html><body><p>*** *** ***</p><p>|</p></body></html>`},
		{"c.xhtml", `<html><body><p>Gamma, the end.</p></body></html>`},
	})
}

// insertEpubFile writes a fixture to the root and inventories it, returning
// the media file row.
func insertEpubFile(t *testing.T, st *store.Store, root, name string, data []byte) models.MediaFile {
	t.Helper()
	writeEpubZip(t, root, name, data)
	var id int64
	err := st.DB().QueryRow(`
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, scanned_at)
		VALUES (?, ?, 'epub', ?, ?, ?) RETURNING id`,
		root, name, len(data), time.Now().UnixNano(), time.Now().UTC()).Scan(&id)
	if err != nil {
		t.Fatalf("insert media file: %v", err)
	}
	return models.MediaFile{ID: id, Root: root, Path: name, Kind: models.MediaFileEpub,
		SizeBytes: int64(len(data))}
}

// assertContiguous is the partition property: chapters in spine order are
// contiguous and cover [0, charCount) with no gaps or overlaps.
func assertContiguous(t *testing.T, chapters []models.EpubChapter, charCount int) {
	t.Helper()
	if len(chapters) == 0 {
		t.Fatal("no chapters")
	}
	if chapters[0].CharStart != 0 {
		t.Errorf("first chapter starts at %d, want 0", chapters[0].CharStart)
	}
	for i := 1; i < len(chapters); i++ {
		if chapters[i].CharStart != chapters[i-1].CharEnd {
			t.Errorf("gap/overlap between spine %d (ends %d) and %d (starts %d)",
				chapters[i-1].SpineIndex, chapters[i-1].CharEnd,
				chapters[i].SpineIndex, chapters[i].CharStart)
		}
	}
	if last := chapters[len(chapters)-1]; last.CharEnd != charCount {
		t.Errorf("last chapter ends at %d, want %d", last.CharEnd, charCount)
	}
}

func TestEnsureForMediaFile(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	file := insertEpubFile(t, st, root, "ncx.epub", fixtureNCX(t))
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if et.ParserVersion != ParserVersion {
		t.Errorf("parser version = %q", et.ParserVersion)
	}
	data, err := os.ReadFile(ing.TextPath(et.ID))
	if err != nil {
		t.Fatalf("read text file: %v", err)
	}
	want := "uno its fine he said mostly second block chapter two twenty three things"
	if string(data) != want {
		t.Errorf("canonical text = %q, want %q", data, want)
	}
	if et.CharCount != len(want) {
		t.Errorf("char count = %d, want %d", et.CharCount, len(want))
	}
	if et.WordCount != len(strings.Fields(want)) {
		t.Errorf("word count = %d", et.WordCount)
	}

	chapters, err := st.ListEpubChapters(context.Background(), et.ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3 (cover included)", len(chapters))
	}
	if chapters[0].Title != "" || chapters[1].Title != "One" || chapters[2].Title != "Two" {
		t.Errorf("titles = %q %q %q", chapters[0].Title, chapters[1].Title, chapters[2].Title)
	}
	assertContiguous(t, chapters, et.CharCount)
	if chapters[0].CharStart != chapters[0].CharEnd {
		t.Errorf("image-only cover should be empty, got [%d,%d)",
			chapters[0].CharStart, chapters[0].CharEnd)
	}

	// A second ensure with the same parser version must not re-parse.
	before, err := st.GetEpubText(context.Background(), file.ID)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // parsed_at has second granularity
	again, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	if !again.ParsedAt.Equal(before.ParsedAt) {
		t.Error("unchanged file was re-parsed")
	}

	// A stale parser version must re-parse in place, keeping the row id.
	if _, err := st.DB().Exec(`UPDATE epub_texts SET parser_version = '0' WHERE media_file_id = ?`, file.ID); err != nil {
		t.Fatalf("age the version: %v", err)
	}
	refreshed, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if refreshed.ID != before.ID {
		t.Errorf("re-parse changed row id: %q → %q", before.ID, refreshed.ID)
	}
	if refreshed.ParserVersion != ParserVersion {
		t.Errorf("version after re-parse = %q", refreshed.ParserVersion)
	}
	chapters2, err := st.ListEpubChapters(context.Background(), refreshed.ID)
	if err != nil {
		t.Fatalf("chapters after re-parse: %v", err)
	}
	assertContiguous(t, chapters2, refreshed.CharCount)
}

func TestEnsureForMediaFileVersionStable(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	// The EPUB 3 fixture: middle document normalizes to nothing.
	file := insertEpubFile(t, st, root, "nav.epub", fixtureNav(t))
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	chapters, err := st.ListEpubChapters(context.Background(), et.ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3", len(chapters))
	}
	if chapters[1].CharStart != chapters[1].CharEnd {
		t.Errorf("all-noise document should be empty, got [%d,%d)",
			chapters[1].CharStart, chapters[1].CharEnd)
	}
	assertContiguous(t, chapters, et.CharCount)

	if _, err := ing.ReadText(context.Background(), et, 0, 5); err != nil {
		t.Errorf("read slice: %v", err)
	}
	if _, err := ing.ReadText(context.Background(), et, et.CharCount, et.CharCount+1); err == nil {
		t.Error("out-of-range read accepted")
	}
}

// TestChapterPartitionProperty runs the contiguity assertion across both
// fixtures plus adversarial synthetic spines: all-empty, single doc,
// empties at both ends.
func TestChapterPartitionProperty(t *testing.T) {
	cases := map[string]*epub.Document{
		"ncx fixture":  mustParse(t, fixtureNCX(t)),
		"nav fixture":  mustParse(t, fixtureNav(t)),
		"all empty":    {Docs: []epub.Doc{{Href: "a"}, {Href: "b"}}},
		"single doc":   {Docs: []epub.Doc{{Href: "a", Blocks: []string{"only"}}}},
		"empty edges":  {Docs: []epub.Doc{{Href: "a"}, {Href: "b", Blocks: []string{"one", "two"}}, {Href: "c"}}},
		"empty middle": {Docs: []epub.Doc{{Href: "a", Blocks: []string{"x"}}, {Href: "b"}, {Href: "c", Blocks: []string{"y"}}}},
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			canonical, chapters, index := Canonicalize(doc)
			assertContiguous(t, chapters, len(canonical))
			if index.CharCount != len(canonical) {
				t.Errorf("index char count %d != %d", index.CharCount, len(canonical))
			}
			// The ranges must reassemble the exact canonical text.
			var rebuilt strings.Builder
			for _, ch := range chapters {
				rebuilt.WriteString(canonical[ch.CharStart:ch.CharEnd])
			}
			if rebuilt.String() != canonical {
				t.Errorf("chapter ranges do not reassemble the text:\n got %q\nwant %q", rebuilt.String(), canonical)
			}
			// Every block offset must resolve into its own document.
			for _, d := range index.Documents {
				for bi, off := range d.Blocks {
					loc, ok := index.Resolve(off)
					if !ok || loc.Href != d.Href || loc.BlockIndex != bi {
						t.Errorf("Resolve(%d) = %+v ok=%v, want doc %s block %d", off, loc, ok, d.Href, bi)
					}
					back, ok := index.Locate(d.Href, bi)
					if !ok || back != off {
						t.Errorf("Locate(%s,%d) = %d ok=%v, want %d", d.Href, bi, back, ok, off)
					}
				}
			}
			if len(canonical) > 0 {
				if _, ok := index.Resolve(len(canonical)); ok {
					t.Error("Resolve(end) should fail")
				}
				if _, ok := index.Resolve(-1); ok {
					t.Error("Resolve(-1) should fail")
				}
			}
		})
	}
}

func TestResolveMidBlock(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{
		{Href: "a.xhtml", Blocks: []string{"alpha beta gamma", "delta"}},
	}}
	canonical, _, index := Canonicalize(doc)
	if canonical != "alpha beta gamma delta" {
		t.Fatalf("canonical = %q", canonical)
	}
	// Offset inside the first block (past its start) resolves to block 0.
	loc, ok := index.Resolve(len("alpha beta"))
	if !ok || loc.BlockIndex != 0 || loc.CharStart != 0 {
		t.Errorf("mid-block resolve = %+v ok=%v", loc, ok)
	}
	// And into the second.
	loc, ok = index.Resolve(len(canonical) - 1)
	if !ok || loc.BlockIndex != 1 {
		t.Errorf("last-block resolve = %+v ok=%v", loc, ok)
	}
}

func TestEnsureForEntry(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	// Seed a user, a book, an entry, and an attached epub file.
	seed := []string{
		`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'a@a.a', 'a', 'x')`,
		`INSERT INTO books (id, title) VALUES ('OL1W', 'Test Book')`,
		`INSERT INTO library_entries (id, user_id, media_type, book_id, status)
			VALUES ('e1', 'u1', 'book', 'OL1W', 'playing')`,
	}
	for _, q := range seed {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	file := insertEpubFile(t, st, root, "book.epub", fixtureNCX(t))
	if _, err := st.DB().Exec(`UPDATE media_files SET book_id = 'OL1W' WHERE id = ?`, file.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	ctx := context.Background()
	if _, err := ing.EnsureForEntry(ctx, "u1", "missing-entry"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing entry err = %v", err)
	}

	et, err := ing.EnsureForEntry(ctx, "u1", "e1")
	if err != nil {
		t.Fatalf("ensure for entry: %v", err)
	}
	if et.MediaFileID != file.ID {
		t.Errorf("media file = %d, want %d", et.MediaFileID, file.ID)
	}

	// A book with no epub attached reports the dedicated error.
	if _, err := st.DB().Exec(`UPDATE media_files SET book_id = NULL WHERE id = ?`, file.ID); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if _, err := ing.EnsureForEntry(ctx, "u1", "e1"); !errors.Is(err, ErrNoEpub) {
		t.Errorf("no-epub err = %v, want ErrNoEpub", err)
	}
}

func TestEnsureDRMRefused(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}
	drm := buildZip(t, [][2]string{
		{"META-INF/container.xml", "<container/>"},
		{"META-INF/encryption.xml", "<encryption/>"},
	})
	file := insertEpubFile(t, st, root, "locked.epub", drm)
	if _, err := ing.EnsureForMediaFile(context.Background(), file); !errors.Is(err, epub.ErrDRM) {
		t.Errorf("err = %v, want ErrDRM", err)
	}
}

func TestPathContainment(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	// A path escaping the root via ..
	secret := filepath.Join(t.TempDir(), "secret.epub")
	if err := os.WriteFile(secret, fixtureNCX(t), 0o644); err != nil {
		t.Fatal(err)
	}
	escaping := models.MediaFile{ID: 1, Root: root, Path: "../../secret.epub", Kind: models.MediaFileEpub}
	if _, err := ing.EnsureForMediaFile(context.Background(), escaping); err == nil {
		t.Error("path escaping the root was accepted")
	}

	// A symlink pointing outside the root.
	link := filepath.Join(root, "linked.epub")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	symlinked := models.MediaFile{ID: 2, Root: root, Path: "linked.epub", Kind: models.MediaFileEpub}
	if _, err := ing.EnsureForMediaFile(context.Background(), symlinked); err == nil {
		t.Error("symlink outside the root was accepted")
	}
}

func TestCanonicalTextIsIdempotentThroughFiles(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}
	file := insertEpubFile(t, st, root, "once.epub", fixtureNCX(t))
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	first, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Force a re-parse by aging the version: the canonical text must be
	// byte-identical — the same parser over the same bytes.
	if _, err := st.DB().Exec(`UPDATE epub_texts SET parser_version = '0'`); err != nil {
		t.Fatal(err)
	}
	et, err = ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	second, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if first != second {
		t.Errorf("re-parse changed the canonical text:\n%q\n%q", first, second)
	}
}

func mustParse(t *testing.T, data []byte) *epub.Document {
	t.Helper()
	doc, err := epub.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc
}
