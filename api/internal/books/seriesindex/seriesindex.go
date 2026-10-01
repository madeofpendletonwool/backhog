// Package seriesindex walks the library naming book series from the Calibre
// sidecars the media scan already parses (MAD-469). It is the backfill
// pattern the search index set: bounded per run, idempotent — it only ever
// fills an empty series_name, so first evidence wins — and safe to re-run
// at any time.
//
// The walk has two arms. The catalogue arm resolves each sidecar that
// carries an exact identity (an Open Library work key or an ISBN) straight
// to a book row. The file-shape arm resolves the books the catalogue could
// not name through their attached files' placement — the stem-matched or
// single-book-directory sidecar, the same rules the matcher itself uses
// when proposing a book — rotating over book ids on a cursor so a shelf
// whose sidecars never arrive cannot wedge the head of the queue.
package seriesindex

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"sync/atomic"

	"github.com/collinpendleton/backhog/api/internal/media"
	"github.com/collinpendleton/backhog/api/internal/models"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// StartupCap bounds the catalogue-unnamed books a boot names automatically.
// The catalogue arm is cheap and runs whole; this cap is the file-shape
// arm's per-boot budget, sized like the search index's: a large library
// comes in over successive boots.
const StartupCap = 100

// cursorKey is the app_settings row holding the last book id the file-shape
// arm walked past. Empty means the next lap starts from the beginning.
const cursorKey = "book_series_cursor"

// Runner serialises series walks.
type Runner struct {
	store   *store.Store
	running atomic.Bool
}

// NewRunner builds a walker over a store.
func NewRunner(st *store.Store) *Runner {
	return &Runner{store: st}
}

// Running reports whether a walk is in progress.
func (r *Runner) Running() bool { return r.running.Load() }

// Run performs one bounded walk, returning how many books it named.
func (r *Runner) Run(ctx context.Context, max int) (int, error) {
	if !r.running.CompareAndSwap(false, true) {
		return 0, nil
	}
	defer r.running.Store(false)

	sidecars, err := r.store.ListMediaSidecars(ctx)
	if err != nil {
		return 0, err
	}
	named, err := r.catalogueArm(ctx, sidecars)
	if err != nil {
		return named, err
	}
	fileNamed, err := r.fileArm(ctx, sidecars, max)
	return named + fileNamed, err
}

// catalogueArm writes every sidecar whose exact identity (work key, ISBN)
// resolves to a catalogued book.
func (r *Runner) catalogueArm(ctx context.Context, sidecars []models.MediaSidecar) (int, error) {
	named := 0
	for _, car := range sidecars {
		if car.Series == "" || (car.WorkKey == "" && car.ISBN == "") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return named, err
		}
		var bookID string
		if car.WorkKey != "" {
			id, err := r.store.BookIDForWorkKey(ctx, car.WorkKey)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return named, err
			}
			bookID = id
		}
		if bookID == "" && car.ISBN != "" {
			id, err := r.store.BookIDForISBN(ctx, car.ISBN)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return named, err
			}
			bookID = id
		}
		if bookID == "" {
			continue
		}
		wrote, err := writeEvidence(ctx, r.store, bookID, car)
		if err != nil {
			return named, err
		}
		if wrote {
			named++
		}
	}
	return named, nil
}

// fileArm names the books their sidecars cannot identify by catalogue key,
// through the placement of their attached files.
func (r *Runner) fileArm(ctx context.Context, sidecars []models.MediaSidecar, max int) (int, error) {
	if max <= 0 {
		max = 50
	}
	cursor, err := r.store.AppSetting(ctx, cursorKey)
	if err != nil {
		return 0, err
	}
	books, err := r.store.BooksWithoutSeries(ctx, cursor, max)
	if err != nil {
		return 0, err
	}

	named := 0
	for _, b := range books {
		if err := ctx.Err(); err != nil {
			return named, err
		}
		files, err := r.store.MediaFilesForBook(ctx, b.ID)
		if err != nil {
			slog.WarnContext(ctx, "series walk", "book", b.ID, "error", err)
			continue
		}
		wrote, err := NameBookFromFiles(ctx, r.store, b.ID, files, sidecars)
		if err != nil {
			slog.WarnContext(ctx, "series walk", "book", b.ID, "error", err)
			continue
		}
		if wrote {
			named++
		}
	}

	if len(books) > 0 {
		next := ""
		if len(books) == max {
			next = books[len(books)-1].ID
		}
		if err := r.store.SetAppSetting(ctx, cursorKey, next); err != nil {
			return named, err
		}
	}
	return named, nil
}

// NameBookFromFiles resolves one book's series from its attached files and
// the sidecars on file, writing the verdict when there is one. The bool
// reports whether a series was written.
func NameBookFromFiles(ctx context.Context, st *store.Store, bookID string, files []models.MediaFile, sidecars []models.MediaSidecar) (bool, error) {
	if len(files) == 0 {
		return false, nil
	}
	var dirs [][2]string
	seen := map[[2]string]bool{}
	for _, f := range files {
		pair := [2]string{f.Root, media.FoldDir(path.Dir(f.Path))}
		if !seen[pair] {
			seen[pair] = true
			dirs = append(dirs, pair)
		}
	}
	dirFiles, err := st.AttachedFilesInDirs(ctx, dirs)
	if err != nil {
		return false, err
	}
	evidence, ok := media.SeriesEvidenceForFiles(files, dirFiles, sidecars)
	if !ok {
		return false, nil
	}
	return st.SetBookSeriesIfEmpty(ctx, bookID, evidence.Name, seriesNumber(evidence))
}

// NameAttachedBook is the attach flow's immediate write: the files just
// attached to a book, resolved now rather than at the next boot's walk. It
// tries the exact identities first — a sidecar carrying this very book's
// work key or ISBN is the strongest evidence there is — then the files'
// placement. Errors are the caller's to log, never to fail the attach with.
func NameAttachedBook(ctx context.Context, st *store.Store, bookID string, files []models.MediaFile) error {
	sidecars, err := st.ListMediaSidecars(ctx)
	if err != nil {
		return err
	}
	for _, car := range sidecars {
		if car.Series == "" {
			continue
		}
		for _, key := range []string{car.WorkKey, car.ISBN} {
			if key == "" {
				continue
			}
			id, err := resolveIdentity(ctx, st, car, key)
			if err != nil || id == "" || id != bookID {
				continue
			}
			_, werr := writeEvidence(ctx, st, bookID, car)
			return werr
		}
	}
	_, err = NameBookFromFiles(ctx, st, bookID, files, sidecars)
	return err
}

// resolveIdentity resolves one catalogue key to a book id, treating
// "not catalogued" as empty rather than an error.
func resolveIdentity(ctx context.Context, st *store.Store, car models.MediaSidecar, key string) (string, error) {
	if key == car.WorkKey {
		id, err := st.BookIDForWorkKey(ctx, key)
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return id, err
	}
	id, err := st.BookIDForISBN(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	return id, err
}

// writeEvidence stores one sidecar's series claim on a book, first
// evidence winning as everywhere else.
func writeEvidence(ctx context.Context, st *store.Store, bookID string, car models.MediaSidecar) (bool, error) {
	return st.SetBookSeriesIfEmpty(ctx, bookID, car.Series, seriesNumberOf(car))
}

func seriesNumber(e media.SeriesEvidence) *float64 {
	if !e.HasNumber {
		return nil
	}
	return &e.Number
}

// seriesNumberOf parses a sidecar's rank the way the evidence helper does.
func seriesNumberOf(car models.MediaSidecar) *float64 {
	e := evidenceOf(car)
	return seriesNumber(e)
}

// evidenceOf adapts the media package's private parse for the catalogue
// arms, which hold raw sidecars rather than resolved evidence.
func evidenceOf(car models.MediaSidecar) media.SeriesEvidence {
	return media.ParseSeriesEvidence(car.Series, car.SeriesIndex)
}
