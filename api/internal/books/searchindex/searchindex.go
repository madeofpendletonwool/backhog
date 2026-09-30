// Package searchindex walks the library's primary texts and lands each one
// in the FTS table the library-wide search reads (MAD-470). It is the
// backfill pattern the games arena's series enrichment set: bounded per
// run, idempotent — the ingester's ensure path does the real deciding — and
// safe to re-run at any time.
//
// The walk rotates: its position is a cursor in app_settings, advanced past
// failures as readily as successes, because a file that can never parse
// (DRM, a corrupt download) must not wedge the head of the queue and starve
// every book behind it. A full lap resets the cursor, so a later boot picks
// up books that attached in the meantime.
package searchindex

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"

	booktext "github.com/collinpendleton/backhog/api/internal/books"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// StartupCap bounds the work done automatically per boot. Parsing is the
// expensive half, so this is deliberately modest: a large never-parsed
// library is brought in over successive boots rather than in one startup
// burst, while re-indexing already-parsed books (the common case after an
// upgrade) is fast enough that the cap rarely binds.
const StartupCap = 100

// cursorKey is the app_settings row holding the last media file id walked.
const cursorKey = "text_index_cursor"

// Runner serialises index walks.
type Runner struct {
	store   *store.Store
	ing     *booktext.Ingester
	running atomic.Bool
}

// NewRunner builds a walker over a store and the canonical-text directory
// the ingester companions live in. The ingester is its own instance — the
// two share nothing but the directory and the database, and neither holds
// state the other needs.
func NewRunner(st *store.Store, textDir string) (*Runner, error) {
	ing, err := booktext.NewIngester(st, textDir)
	if err != nil {
		return nil, err
	}
	return &Runner{store: st, ing: ing}, nil
}

// Running reports whether a walk is in progress.
func (r *Runner) Running() bool { return r.running.Load() }

// Run performs one bounded walk. Files that fail to parse are logged and
// left for a later lap; the walk itself only stops early on a cancelled
// context.
func (r *Runner) Run(ctx context.Context, max int) (int, error) {
	if !r.running.CompareAndSwap(false, true) {
		return 0, nil
	}
	defer r.running.Store(false)

	cursor, err := r.store.AppSettingInt(ctx, cursorKey)
	if err != nil {
		return 0, err
	}
	files, err := r.store.BookTextIndexCandidates(ctx, booktext.ParserVersion, cursor, max)
	if err != nil {
		return 0, err
	}

	walked := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return walked, err
		}
		if _, err := r.ing.EnsureForMediaFile(ctx, f); err != nil {
			// DRM, corrupt, unreadable: the book stays searchable-never,
			// which the search answers by silence rather than by error.
			// The cursor advances anyway — see the package comment.
			slog.Warn("text index walk", "file", f.ID, "path", f.Path, "error", err)
		}
		walked++
	}
	if len(files) > 0 {
		next := files[len(files)-1].ID
		if len(files) < max {
			// The lap is exhausted; start the next one from the beginning.
			next = 0
		}
		if err := r.store.SetAppSetting(ctx, cursorKey, strconv.FormatInt(next, 10)); err != nil {
			return walked, err
		}
	}
	return walked, nil
}
