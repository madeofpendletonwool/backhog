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

	mobigo "github.com/madeofpendletonwool/mobi-go"

	"github.com/collinpendleton/backhog/api/internal/books/epub"
	"github.com/collinpendleton/backhog/api/internal/books/passage"
	"github.com/collinpendleton/backhog/api/internal/books/pdf"
	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/fixtures"
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
	// Two chapters, not three: the image-only cover owns no text and is no
	// longer a chapter of its own — it is swallowed by the first real one,
	// which is what keeps its art reachable without listing a nameless
	// zero-length "Section 1" above chapter one.
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(chapters), chapters)
	}
	if chapters[0].Title != "One" || chapters[1].Title != "Two" {
		t.Errorf("titles = %q %q, want \"One\" \"Two\"", chapters[0].Title, chapters[1].Title)
	}
	if chapters[0].TitleSource != TitleSourceTOC {
		t.Errorf("title source = %q, want %q", chapters[0].TitleSource, TitleSourceTOC)
	}
	// The cover is inside the first chapter's span, so its illustration is
	// still addressable.
	if chapters[0].SpineIndex != 0 {
		t.Errorf("first chapter spine index = %d, want 0 (the cover)", chapters[0].SpineIndex)
	}
	assertContiguous(t, chapters, et.CharCount)

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

// The alignment worker shares the data volume read-only but runs as its
// own uid, so the canonical text files must come out world-readable —
// os.CreateTemp's 0600 locked the worker out of every text it needed.
func TestCanonicalTextFilesWorldReadable(t *testing.T) {
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

	for _, name := range []string{
		ing.TextPath(et.ID),
		ing.IndexPath(et.ID),
		ing.DisplayPath(et.ID),
	} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644", filepath.Base(name), info.Mode().Perm())
		}
	}
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
	// The middle document normalizes to nothing, so it joins the chapter it
	// sits inside rather than standing as an empty one.
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(chapters), chapters)
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
			canonical, _, chapters, index := Canonicalize(doc)
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
	canonical, _, _, index := Canonicalize(doc)
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

// TestCanonicalizeAnchorsImages checks the one place image anchoring can go
// wrong: a block that normalizes away. Raw block indexes and canonical ones
// diverge there, and an image anchored to a dropped block has to land on the
// next surviving one rather than at a stale index.
func TestCanonicalizeAnchorsImages(t *testing.T) {
	doc := &epub.Document{Docs: []epub.Doc{{
		Href: "OEBPS/c1.xhtml",
		// Block 1 is punctuation only: Normalize empties it, so the
		// canonical document has two blocks, not three.
		Blocks: []string{"First", "---", "Second"},
		Images: []epub.Image{
			{Href: "OEBPS/a.png", BeforeBlock: 0},
			{Href: "OEBPS/b.png", Alt: "dropped neighbour", BeforeBlock: 1},
			{Href: "OEBPS/c.png", BeforeBlock: 2},
			{Href: "OEBPS/d.png", BeforeBlock: 3},
		},
	}}}

	_, _, _, index := Canonicalize(doc)
	if got := len(index.Documents[0].Blocks); got != 2 {
		t.Fatalf("canonical blocks = %d, want 2", got)
	}
	want := []int{0, 1, 1, 2}
	for i, img := range index.Documents[0].Images {
		if img.BeforeBlock != want[i] {
			t.Errorf("image %s before_block = %d, want %d", img.Href, img.BeforeBlock, want[i])
		}
	}
	if len(index.Documents[0].Images) != len(want) {
		t.Fatalf("images = %d, want %d", len(index.Documents[0].Images), len(want))
	}
}

// --- MOBI / AZW3 ingest ------------------------------------------------------
//
// The mobi path must be indistinguishable from the epub path one layer
// down: same canonicalize, same chapter rows, same partition property,
// same companion files. The fixtures are mobi-go's oracle-verified
// synthetic books (see internal/fixtures).

// insertBookFile writes raw book bytes to the root and inventories them as
// a text-side media file, whatever the container.
func insertBookFile(t *testing.T, st *store.Store, root, name string, data []byte) models.MediaFile {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
		t.Fatalf("write book: %v", err)
	}
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

func TestEnsureForMediaFileMOBI(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	file := insertBookFile(t, st, root, "book.mobi", fixtures.MOBI6Palmdoc)
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if et.ParserVersion != ParserVersion {
		t.Errorf("parser version = %q", et.ParserVersion)
	}

	// PalmDOC-compressed, EXTH-carrying, INDX-NCX TOC: four chapters from
	// the four NCX entries, each titled, partitioning the text exactly.
	chapters, err := st.ListEpubChapters(context.Background(), et.ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	// Three chapters, not four: the NCX's "Begin Reading" guide anchor sits
	// at the same byte as the first real chapter and owns no text, so it no
	// longer appears as a zero-length row above it.
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3: %+v", len(chapters), chapters)
	}
	wantTitles := []string{"Synthetic PalmDOC", "Second Chapter", "Third Chapter"}
	for i, want := range wantTitles {
		if chapters[i].Title != want {
			t.Errorf("chapter %d title = %q, want %q", i, chapters[i].Title, want)
		}
	}
	assertContiguous(t, chapters, et.CharCount)

	// The reader's ranged fetch: every chapter's canonical slice reads back
	// as the canonical text's own bytes.
	text, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if len(text) != et.CharCount {
		t.Errorf("text length %d != char count %d", len(text), et.CharCount)
	}
	for _, ch := range chapters {
		slice, err := ing.ReadText(context.Background(), et, ch.CharStart, ch.CharEnd)
		if err != nil {
			t.Fatalf("read chapter %d: %v", ch.SpineIndex, err)
		}
		if slice != text[ch.CharStart:ch.CharEnd] {
			t.Errorf("chapter %d ranged read disagrees with the canonical text", ch.SpineIndex)
		}
	}
	if !strings.Contains(text, "this book exists to be parsed") {
		t.Errorf("canonical text missing chapter prose: %q", text)
	}
	if !strings.Contains(text, "third chapter see the beginning") {
		t.Errorf("canonical text missing last chapter prose: %q", text)
	}

	// The block index resolves an offset inside chapter 3 to that chapter —
	// the "which chapter am I in" query invariant 7 backs.
	index, err := ing.LoadIndex(context.Background(), et)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	last := chapters[len(chapters)-1]
	loc, ok := index.Resolve(last.CharStart)
	if !ok {
		t.Fatal("resolve last chapter start")
	}
	if loc.SpineIndex != last.SpineIndex {
		t.Errorf("resolved spine index = %d, want %d", loc.SpineIndex, last.SpineIndex)
	}

	// Re-ensure with a current parse must not re-parse (the companion files
	// and row exist), and a stale version must re-parse in place.
	again, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	if !again.ParsedAt.Equal(et.ParsedAt) {
		t.Error("unchanged mobi was re-parsed")
	}
	if _, err := st.DB().Exec(`UPDATE epub_texts SET parser_version = '0' WHERE media_file_id = ?`, file.ID); err != nil {
		t.Fatalf("age the version: %v", err)
	}
	refreshed, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if refreshed.ID != et.ID || refreshed.ParserVersion != ParserVersion {
		t.Errorf("re-parse = id %q version %q; want %q / %q", refreshed.ID, refreshed.ParserVersion, et.ID, ParserVersion)
	}
}

func TestEnsureForMediaFileKF8(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	file := insertBookFile(t, st, root, "book.azw3", fixtures.AZW3KF8)
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// One chapter per linear KF8 section, the non-linear colophon skipped,
	// the NCX titles resolved through the pos pairs.
	chapters, err := st.ListEpubChapters(context.Background(), et.ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	// The image-only cover section holds no text, so it folds into the first
	// chapter instead of listing as an empty one.
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(chapters), chapters)
	}
	wantTitles := []string{"Chapter 1", "Chapter 2"}
	for i, want := range wantTitles {
		if chapters[i].Title != want {
			t.Errorf("chapter %d title = %q, want %q", i, chapters[i].Title, want)
		}
	}
	assertContiguous(t, chapters, et.CharCount)

	// The exact canonical text: the same pinned Normalize over KF8 blocks.
	data, err := os.ReadFile(ing.TextPath(et.ID))
	if err != nil {
		t.Fatalf("read text file: %v", err)
	}
	want := "chapter one the first section reassembles from a skeleton and its fragments " +
		"fragments splice at insert offsets even mid tag chapter two second section also fragmented"
	if string(data) != want {
		t.Errorf("canonical text = %q, want %q", data, want)
	}
	if et.CharCount != len(want) {
		t.Errorf("char count = %d, want %d", et.CharCount, len(want))
	}
}

func TestEnsureForMediaFileDRMRefused(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	file := insertBookFile(t, st, root, "locked.mobi", fixtures.MOBI6DRM)
	_, err = ing.EnsureForMediaFile(context.Background(), file)
	if !errors.Is(err, mobigo.ErrDRM) {
		t.Fatalf("error = %v, want one wrapping mobi.ErrDRM", err)
	}
	// Refused whole, never half-parsed: no canonical-text row exists.
	if _, err := st.GetEpubText(context.Background(), file.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("epub_texts row after DRM refusal: %v", err)
	}
}

// The passage matcher — the OCR/page-anchor side of the arena — runs over a
// MOBI-sourced canonical text the same as an EPUB-sourced one: both sides
// speak books.Normalize, so a photographed sentence lands on its offset.
// The alignment worker consumes the identical text file through the same
// normalize, which is what makes MOBI-sourced alignment work by
// construction.
func TestPassageMatchingOverMOBIText(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}
	file := insertBookFile(t, st, root, "book.azw3", fixtures.AZW3KF8)
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	matcher := passage.New(func(ctx context.Context, id string) (string, error) {
		data, err := os.ReadFile(ing.TextPath(id))
		if err != nil {
			return "", err
		}
		return string(data), nil
	})

	// The query as a phone would OCR it: wrong case, curly quotes, an em
	// dash, a stray comma. Normalization on both sides absorbs it. (The
	// matcher refuses queries under ten words, so the query is a full
	// sentence pair.)
	res, err := matcher.Find(context.Background(), et.ID,
		`The FIRST section reassembles from a skeleton and its fragments. “Fragments splice at INSERT offsets—even mid-tag.”`)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	text, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := text[res.Match.CharOffset:res.Match.CharEnd]
	want := "the first section reassembles from a skeleton and its fragments fragments splice at insert offsets even mid tag"
	if got != want {
		t.Errorf("matched span = %q (offset %d)", got, res.Match.CharOffset)
	}
}

// --- PDF ingest ----------------------------------------------------------------
//
// The third container, held to the mobi playbook: same canonicalize, same
// chapter rows, same partition property, same companion files — plus the
// quality gate's routing, which is the pdf's alone.

func TestEnsureForMediaFilePDF(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	file := insertBookFile(t, st, root, "book.pdf", fixtures.BuildProsePDF())
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if et.ParserVersion != ParserVersion {
		t.Errorf("parser version = %q", et.ParserVersion)
	}

	// Three chapters from the outline bookmarks, page four absorbed by
	// chapter three; each titled, partitioning the text exactly.
	chapters, err := st.ListEpubChapters(context.Background(), et.ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3: %+v", len(chapters), chapters)
	}
	for i, want := range []string{"Chapter One", "Chapter Two", "Chapter Three"} {
		if chapters[i].Title != want {
			t.Errorf("chapter %d title = %q, want %q", i, chapters[i].Title, want)
		}
		if chapters[i].TitleSource != TitleSourceTOC {
			t.Errorf("chapter %d title source = %q, want the outline's", i, chapters[i].TitleSource)
		}
	}
	assertContiguous(t, chapters, et.CharCount)

	// The reader's ranged fetch: every chapter's canonical slice reads back
	// as the canonical text's own bytes, and the dehyphenated words the
	// parser rejoined are single words in the stored text — Normalize's
	// dash rule would have frozen them as two.
	text, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if len(text) != et.CharCount {
		t.Errorf("text length %d != char count %d", len(text), et.CharCount)
	}
	for _, ch := range chapters {
		slice, err := ing.ReadText(context.Background(), et, ch.CharStart, ch.CharEnd)
		if err != nil {
			t.Fatalf("read chapter %d: %v", ch.SpineIndex, err)
		}
		if slice != text[ch.CharStart:ch.CharEnd] {
			t.Errorf("chapter %d ranged read disagrees with the canonical text", ch.SpineIndex)
		}
	}
	for _, want := range []string{"extraordinary", "twentythree", "ordinary across the page boundary"} {
		if !strings.Contains(text, want) {
			t.Errorf("canonical text missing %q: %q", want, text)
		}
	}
	// The running heads the parser strips must not have come back.
	if strings.Contains(text, "the synthetic book") {
		t.Errorf("canonical text kept a running head: %q", text)
	}

	// The block index resolves an offset inside chapter 3 to that chapter —
	// the "which chapter am I in" query invariant 7 backs, over a
	// pdf-sourced text exactly like an epub-sourced one.
	index, err := ing.LoadIndex(context.Background(), et)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	last := chapters[len(chapters)-1]
	loc, ok := index.Resolve(last.CharStart)
	if !ok {
		t.Fatal("resolve last chapter start")
	}
	if loc.SpineIndex != last.SpineIndex {
		t.Errorf("resolved spine index = %d, want %d", loc.SpineIndex, last.SpineIndex)
	}

	// Re-ensure with a current parse must not re-parse, and a stale version
	// must re-parse in place keeping the row id.
	again, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	if !again.ParsedAt.Equal(et.ParsedAt) {
		t.Error("unchanged pdf was re-parsed")
	}
	if _, err := st.DB().Exec(`UPDATE epub_texts SET parser_version = '0' WHERE media_file_id = ?`, file.ID); err != nil {
		t.Fatalf("age the version: %v", err)
	}
	refreshed, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if refreshed.ID != et.ID || refreshed.ParserVersion != ParserVersion {
		t.Errorf("re-parse = id %q version %q; want %q / %q", refreshed.ID, refreshed.ParserVersion, et.ID, ParserVersion)
	}
}

// The quality gate's routing, as the issue states it: image-native → no
// canonical text row with a named refusal, never a half-parse; drm → the
// drm refusal, again whole. Both leave epub_texts empty for the file.
func TestEnsureForMediaFilePDFRefusals(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}

	imageFile := insertBookFile(t, st, root, "comic.pdf", fixtures.BuildImageOnlyPDF())
	_, err = ing.EnsureForMediaFile(context.Background(), imageFile)
	if !errors.Is(err, pdf.ErrImageNative) {
		t.Fatalf("image-native error = %v, want pdf.ErrImageNative", err)
	}
	if _, err := st.GetEpubText(context.Background(), imageFile.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("epub_texts row after image-native refusal: %v", err)
	}

	locked, err := fixtures.BuildEncryptedPDF(true)
	if err != nil {
		t.Fatalf("build encrypted pdf: %v", err)
	}
	drmFile := insertBookFile(t, st, root, "locked.pdf", locked)
	_, err = ing.EnsureForMediaFile(context.Background(), drmFile)
	if !errors.Is(err, pdf.ErrDRM) {
		t.Fatalf("drm error = %v, want pdf.ErrDRM", err)
	}
	if _, err := st.GetEpubText(context.Background(), drmFile.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("epub_texts row after DRM refusal: %v", err)
	}
}

// Passage matching over a pdf-sourced canonical text: the dehyphenated,
// running-head-stripped extraction canonicalizes through the same pinned
// Normalize, so a photographed sentence of the printed page lands on its
// offset — the same construction argument the mobi test makes.
func TestPassageMatchingOverPDFText(t *testing.T) {
	st := newTestStore(t)
	root := filepath.Join(t.TempDir(), "books")
	ing, err := NewIngester(st, filepath.Join(t.TempDir(), "epub_text"))
	if err != nil {
		t.Fatalf("ingester: %v", err)
	}
	file := insertBookFile(t, st, root, "book.pdf", fixtures.BuildProsePDF())
	et, err := ing.EnsureForMediaFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	matcher := passage.New(func(ctx context.Context, id string) (string, error) {
		data, err := os.ReadFile(ing.TextPath(id))
		if err != nil {
			return "", err
		}
		return string(data), nil
	})

	// The query as the paper copy reads — including the hyphen the printed
	// page breaks on ("extraordi-nary"), which the parser must have
	// rejoined before Normalize ever ran.
	res, err := matcher.Find(context.Background(), et.ID,
		`The FIRST paragraph opens the book with plain words in sentences that a reader can follow, and nothing here is EXTRAORDI-nary until the hyphen joins.`)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	text, err := ing.ReadText(context.Background(), et, 0, et.CharCount)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := text[res.Match.CharOffset:res.Match.CharEnd]
	// The sentence from the first paragraph — rejoined "extraordinary"
	// included, the property this test exists for.
	for _, want := range []string{
		"the first paragraph opens the book with plain words",
		"and nothing here is extraordinary until the hyphen joins",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("matched span = %q (offset %d), missing %q", got, res.Match.CharOffset, want)
		}
	}
}
