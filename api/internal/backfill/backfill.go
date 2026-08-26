// Package backfill walks the cached games and enriches each one with the full
// IGDB field set, building the series and DLC relations from it. It is
// incremental (least-recently-fetched first), rate-limited by the shared IGDB
// client, and idempotent — every write it makes is an upsert.
package backfill

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

// StartupCap bounds the work done automatically per boot so a large library
// doesn't hammer IGDB for hours; the on-demand trigger uses a larger budget.
const (
	StartupCap = 40
	KickCap    = 500
)

// Result summarises one completed run.
type Result struct {
	Scanned int `json:"scanned"` // games examined
	Failed  int `json:"failed"`  // games that errored and were skipped
}

// Runner serialises backfill runs: Kick refuses to start a second one while
// the first is still walking.
type Runner struct {
	store    *store.Store
	provider metadata.Provider
	running  atomic.Bool
}

func NewRunner(st *store.Store, provider metadata.Provider) *Runner {
	return &Runner{store: st, provider: provider}
}

// Running reports whether a backfill walk is in progress.
func (r *Runner) Running() bool { return r.running.Load() }

// Kick starts an asynchronous run when none is active. Returns whether one was
// started.
func (r *Runner) Kick(ctx context.Context, maxGames int) bool {
	if !r.running.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer r.running.Store(false)
		// Detached from the request: the run outlives the handler's context.
		runCtx := context.Background()
		result, err := r.Run(runCtx, maxGames)
		if err != nil {
			slog.Error("series backfill failed", "error", err)
			return
		}
		slog.Info("series backfill done", "scanned", result.Scanned, "failed", result.Failed)
	}()
	return true
}

// Run performs one bounded, synchronous walk.
func (r *Runner) Run(ctx context.Context, maxGames int) (Result, error) {
	if !r.running.CompareAndSwap(false, true) {
		return Result{}, nil
	}
	defer r.running.Store(false)

	result := Result{}
	ids, err := r.store.SeriesBackfillCandidates(ctx, maxGames)
	if err != nil {
		return result, err
	}

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := r.backfillGame(ctx, id); err != nil {
			// One game failing (deleted upstream, transient error) must not
			// stop the walk; it stays a candidate for the next run.
			slog.Warn("series backfill game", "game_id", id, "error", err)
			result.Failed++
		}
		result.Scanned++
	}
	return result, nil
}

// backfillGame fetches one game's full record, then its DLC and expansions,
// and writes the whole cluster through the store.
func (r *Runner) backfillGame(ctx context.Context, id int64) error {
	game, err := r.provider.GetByID(ctx, id)
	if err != nil {
		return err
	}

	children := []metadata.Game{}
	if game.Extras != nil {
		ids := make([]int64, 0, len(game.Extras.DLCs)+len(game.Extras.Expansions))
		for _, d := range game.Extras.DLCs {
			ids = append(ids, d.ID)
		}
		for _, d := range game.Extras.Expansions {
			ids = append(ids, d.ID)
		}
		if len(ids) > 0 {
			fetched, err := r.provider.GetManyByIDs(ctx, ids)
			if err != nil {
				// The parent is still worth storing without its children;
				// the DLC links land on the next walk.
				slog.Warn("series backfill children", "game_id", id, "error", err)
			} else {
				children = fetched
			}
		}
	}

	return r.store.ApplySeriesData(ctx, game, children)
}
