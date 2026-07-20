package store

import (
	"context"
	"errors"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// PickFilter narrows what "pick something for me" is allowed to choose from.
type PickFilter struct {
	// MaxHours caps time-to-beat. Games with no known playtime are excluded when
	// this is set, since an unknown length can't satisfy "something short".
	MaxHours  float64
	MinRating float64
	GenreID   *int64
	PlatformID *int64
}

// PickRandom returns one random backlog game matching the filter.
//
// Ordering by RANDOM() is fine here: it's a single row from one user's backlog,
// which is hundreds of rows at most, not a table scan worth optimising.
func (s *Store) PickRandom(ctx context.Context, userID string, f PickFilter) (models.Entry, error) {
	query := entrySelect + ` WHERE e.user_id = ? AND e.status = 'backlog'`
	args := []any{userID}

	if f.MaxHours > 0 {
		query += ` AND g.time_to_beat_main IS NOT NULL AND g.time_to_beat_main <= ?`
		args = append(args, f.MaxHours*3600)
	}
	if f.MinRating > 0 {
		query += ` AND g.igdb_rating IS NOT NULL AND g.igdb_rating >= ?`
		args = append(args, f.MinRating)
	}
	if f.GenreID != nil {
		query += ` AND EXISTS (SELECT 1 FROM game_genres gg WHERE gg.game_id = e.game_id AND gg.genre_id = ?)`
		args = append(args, *f.GenreID)
	}
	if f.PlatformID != nil {
		query += ` AND EXISTS (SELECT 1 FROM game_platforms gp WHERE gp.game_id = e.game_id AND gp.platform_id = ?)`
		args = append(args, *f.PlatformID)
	}

	query += ` ORDER BY RANDOM() LIMIT 1`

	entries, err := s.queryEntries(ctx, query, args...)
	if err != nil {
		return models.Entry{}, err
	}
	if len(entries) == 0 {
		return models.Entry{}, ErrNotFound
	}
	return entries[0], nil
}

// ErrNoCandidates is returned when the backlog has nothing matching a pick.
var ErrNoCandidates = errors.New("no games match")
