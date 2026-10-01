package seriesindex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// seed plants the pieces the arms read: books, their attached files, and
// the Calibre sidecars the media scan would have parsed.
type seed struct {
	db  *store.Store
	ctx context.Context
}

func newSeed(t *testing.T) *seed {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "series_index.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &seed{db: store.New(database), ctx: t.Context()}
}

func (s *seed) book(t *testing.T, id string) {
	t.Helper()
	if _, err := s.db.DB().Exec(
		`INSERT INTO books (id, title) VALUES (?, ?)`, id, id); err != nil {
		t.Fatalf("book %s: %v", id, err)
	}
}

func (s *seed) file(t *testing.T, path, bookID string) {
	t.Helper()
	if _, err := s.db.DB().Exec(`
		INSERT INTO media_files (root, path, kind, size_bytes, mtime, book_id, scanned_at)
		VALUES ('/nas', ?, 'epub', 1, 0, ?, CURRENT_TIMESTAMP)`, path, bookID); err != nil {
		t.Fatalf("file %s: %v", path, err)
	}
}

func (s *seed) sidecar(t *testing.T, path string, car models.MediaSidecar) {
	t.Helper()
	// Direct insert: the store's replacer swaps a whole root's inventory,
	// which would drop every earlier sidecar the test planted.
	car.Root = "/nas"
	car.Path = path
	if _, err := s.db.DB().Exec(`
		INSERT INTO media_sidecars (root, path, title, author, series, series_index,
		                            language, isbn, work_key, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		car.Root, car.Path, car.Title, car.Author, car.Series, car.SeriesIndex,
		car.Language, car.ISBN, car.WorkKey); err != nil {
		t.Fatalf("sidecar %s: %v", path, err)
	}
}

func (s *seed) seriesOf(t *testing.T, bookID string) (string, *float64) {
	t.Helper()
	var name string
	var number *float64
	row := s.db.DB().QueryRow(
		`SELECT series_name, series_number FROM books WHERE id = ?`, bookID)
	if err := row.Scan(&name, &number); err != nil {
		t.Fatalf("read %s: %v", bookID, err)
	}
	return name, number
}

func TestWalkerArms(t *testing.T) {
	s := newSeed(t)

	// The catalogue arms: exact identities, no files needed.
	s.book(t, "OLKW1")
	s.book(t, "OLISBN1")
	s.sidecar(t, "Library/Saga 02 - Second.opf", models.MediaSidecar{
		Series: "Saga", SeriesIndex: "2", WorkKey: "OLKW1"})
	s.sidecar(t, "Library/Other/edition.opf", models.MediaSidecar{
		Series: "ISBN Saga", ISBN: "9780575084070"})

	// The stem arm: a sidecar sharing the attached file's stem.
	s.book(t, "OLSTEM")
	s.file(t, "King/Talisman 01 - The Talisman.epub", "OLSTEM")
	s.sidecar(t, "King/Talisman 01 - The Talisman.opf", models.MediaSidecar{
		Series: "Talisman", SeriesIndex: "1"})

	// The single-book directory: Calibre's per-book metadata.opf.
	s.book(t, "OLDIR")
	s.file(t, "HP/Goblet/cover.epub", "OLDIR")
	s.sidecar(t, "HP/Goblet/metadata.opf", models.MediaSidecar{
		Series: "Harry Potter", SeriesIndex: "4"})

	// The ambiguous directory: two books' files share it, so the
	// directory-level sidecar says nothing.
	s.book(t, "OLAMB1")
	s.book(t, "OLAMB2")
	s.file(t, "Flat/first book.epub", "OLAMB1")
	s.file(t, "Flat/second book.epub", "OLAMB2")
	s.sidecar(t, "Flat/metadata.opf", models.MediaSidecar{Series: "Flat Saga"})

	// The ISBN arm needs an edition row to resolve through.
	if _, err := s.db.DB().Exec(`
		INSERT INTO book_editions (id, book_id, isbn13)
		VALUES ('OLISBN1-M', 'OLISBN1', '9780575084070')`); err != nil {
		t.Fatalf("edition: %v", err)
	}

	runner := NewRunner(s.db)
	named, err := runner.Run(s.ctx, 10)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if named != 4 {
		t.Fatalf("named = %d, want 4 (work key, isbn, stem, dir)", named)
	}

	for bookID, want := range map[string]string{
		"OLKW1": "Saga", "OLISBN1": "ISBN Saga", "OLSTEM": "Talisman", "OLDIR": "Harry Potter",
	} {
		if got, _ := s.seriesOf(t, bookID); got != want {
			t.Errorf("%s series = %q, want %q", bookID, got, want)
		}
	}
	if name, _ := s.seriesOf(t, "OLAMB1"); name != "" {
		t.Errorf("ambiguous directory named %q — it must say nothing", name)
	}
	if _, number := s.seriesOf(t, "OLSTEM"); number == nil || *number != 1 {
		t.Errorf("stem rank = %v, want 1", number)
	}

	// Idempotent and first-evidence-wins: a second lap names nothing new,
	// and later sidecars do not overwrite the recorded names.
	if _, err := s.db.DB().Exec(`
		UPDATE media_sidecars SET series = 'Wrong Saga', work_key = 'OLSTEM'
		WHERE root = '/nas' AND path = 'King/Talisman 01 - The Talisman.opf'`); err != nil {
		t.Fatalf("retitle sidecar: %v", err)
	}
	named, err = runner.Run(s.ctx, 10)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if named != 0 {
		t.Fatalf("second run named %d, want 0", named)
	}
	if got, _ := s.seriesOf(t, "OLSTEM"); got != "Talisman" {
		t.Errorf("first evidence lost: %q", got)
	}
}

func TestNameAttachedBook(t *testing.T) {
	s := newSeed(t)
	s.book(t, "OLNEW")
	files := []models.MediaFile{{Root: "/nas", Path: "King/Talisman 02 - Black House.epub"}}
	s.sidecar(t, "King/Talisman 02 - Black House.opf", models.MediaSidecar{
		Series: "Talisman", SeriesIndex: "2"})

	// The attach-time write sees the files before they even have ids: the
	// evidence is their placement, not their rows.
	if err := NameAttachedBook(s.ctx, s.db, "OLNEW", files); err != nil {
		t.Fatalf("name attached: %v", err)
	}
	if got, number := s.seriesOf(t, "OLNEW"); got != "Talisman" || number == nil || *number != 2 {
		t.Fatalf("attached book = %q rank %v, want Talisman 2", got, number)
	}
}
