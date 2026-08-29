package store

import (
	"context"
	"fmt"

	"github.com/collinpendleton/backhog/api/internal/metadata"
)

// SyncPlatformMeta re-applies the curated classification to platform rows
// already in the shared cache. Idempotent; runs at startup so rows written
// before the catalog existed (or by an older binary) get healed. Rows the
// catalog does not know are left NULL/empty and degrade at read time.
func (s *Store) SyncPlatformMeta(ctx context.Context) error {
	for id, meta := range metadata.PlatformCatalog {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE platforms
			SET generation = ?, family = ?, manufacturer = ?, handheld = ?
			WHERE id = ?`,
			meta.Generation, meta.Family, meta.Manufacturer, meta.Handheld, id); err != nil {
			return fmt.Errorf("sync platform meta %d: %w", id, err)
		}
	}
	return nil
}
