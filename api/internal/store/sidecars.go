package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// ReplaceMediaSidecars swaps one root's parsed .opf inventory in a single
// transaction, exactly like ReplaceMediaSkipped: a sidecar describes what is
// on disk right now, nothing is attached to it, and the scan just walked the
// whole root.
func (s *Store) ReplaceMediaSidecars(ctx context.Context, root string, cars []models.MediaSidecar) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM media_sidecars WHERE root = ?`, root); err != nil {
		return err
	}
	const batchSize = 128
	for start := 0; start < len(cars); start += batchSize {
		batch := cars[start:min(start+batchSize, len(cars))]
		var sb strings.Builder
		args := make([]any, 0, len(batch)*10)
		sb.WriteString(`INSERT INTO media_sidecars ` +
			`(root, path, title, author, series, series_index, language, isbn, work_key, seen_at) VALUES `)
		for i, c := range batch {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`(?,?,?,?,?,?,?,?,?,?)`)
			args = append(args, c.Root, c.Path, c.Title, c.Author, c.Series,
				c.SeriesIndex, c.Language, c.ISBN, c.WorkKey, c.SeenAt)
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("insert media sidecars: %w", err)
		}
	}
	return tx.Commit()
}

// ListMediaSidecars returns every parsed sidecar, ordered by root and path.
// The matcher loads them all in this one query and keys them by directory
// itself: candidate grouping is the only thing that knows which directory a
// book lives in, and the ordering here is what makes "pick one when a
// directory holds several" deterministic.
func (s *Store) ListMediaSidecars(ctx context.Context) ([]models.MediaSidecar, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, root, path, title, author, series, series_index, language, isbn, work_key, seen_at
		FROM media_sidecars ORDER BY root, path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MediaSidecar{}
	for rows.Next() {
		var c models.MediaSidecar
		if err := rows.Scan(&c.ID, &c.Root, &c.Path, &c.Title, &c.Author, &c.Series,
			&c.SeriesIndex, &c.Language, &c.ISBN, &c.WorkKey, &c.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
