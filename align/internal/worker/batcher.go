package worker

import (
	"context"

	"github.com/collinpendleton/backhog/align/internal/api"
)

// batcher streams transcript segments to the API in fixed-size batches
// instead of holding a whole book's transcript in memory and posting it
// at the end. Each POST also refreshes the heartbeat, so a book that is
// producing output can never look stalled.
type batcher struct {
	api   *api.Client
	jobID string
	size  int

	pending []api.Segment
	count   int
}

func (b *batcher) add(ctx context.Context, segments []api.Segment) error {
	b.pending = append(b.pending, segments...)
	for len(b.pending) >= b.size {
		if err := b.send(ctx, b.pending[:b.size]); err != nil {
			return err
		}
		b.pending = b.pending[b.size:]
	}
	return nil
}

func (b *batcher) flush(ctx context.Context) error {
	if len(b.pending) == 0 {
		return nil
	}
	if err := b.send(ctx, b.pending); err != nil {
		return err
	}
	b.pending = nil
	return nil
}

func (b *batcher) send(ctx context.Context, batch []api.Segment) error {
	if err := b.api.Segments(ctx, b.jobID, batch); err != nil {
		return err
	}
	b.count += len(batch)
	return nil
}
