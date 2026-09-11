package worker

import (
	"context"

	"github.com/collinpendleton/backhog/ocr/internal/api"
)

// batcher streams per-page results to the API in fixed-size batches
// instead of holding a whole comic in memory and posting it at the end.
// Each POST also refreshes the heartbeat, so a pass that is producing
// output can never look stalled.
type batcher struct {
	api   *api.Client
	jobID string
	size  int

	pending []api.Page
	ocrVer  string
}

func newBatcher(client *api.Client, jobID string, size int) *batcher {
	return &batcher{api: client, jobID: jobID, size: size, ocrVer: ocrVersion}
}

func (b *batcher) add(ctx context.Context, pages []api.Page) error {
	b.pending = append(b.pending, pages...)
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

func (b *batcher) send(ctx context.Context, batch []api.Page) error {
	return b.api.Pages(ctx, b.jobID, b.ocrVer, batch)
}
