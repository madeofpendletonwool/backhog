package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// UpsertGame writes provider metadata into the shared game cache, replacing the
// game's genre and platform links. Runs in one transaction so a game is never
// visible with a half-written set of relations.
func (s *Store) UpsertGame(ctx context.Context, g metadata.Game, accentHex string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Extras are only present on a detail fetch. On a lean search/import upsert
	// they're empty, and the COALESCE below preserves any extras a previous
	// detail fetch already stored — searching a game must not wipe its metadata.
	var extrasJSON string
	if g.Extras != nil {
		if encoded, mErr := json.Marshal(g.Extras); mErr == nil {
			extrasJSON = string(encoded)
		}
	}

	// Keep an existing accent if this call did not compute one (e.g. the cover
	// was already cached and re-sampling was skipped).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO games (id, name, slug, summary, cover_url, accent_hex,
		                   first_release_date, igdb_rating, time_to_beat_main,
		                   time_to_beat_complete, raw_json, extras_json, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name                  = excluded.name,
			slug                  = excluded.slug,
			summary               = excluded.summary,
			cover_url             = excluded.cover_url,
			accent_hex            = COALESCE(NULLIF(excluded.accent_hex, ''), games.accent_hex),
			first_release_date    = excluded.first_release_date,
			igdb_rating           = excluded.igdb_rating,
			time_to_beat_main     = excluded.time_to_beat_main,
			time_to_beat_complete = excluded.time_to_beat_complete,
			raw_json              = excluded.raw_json,
			extras_json           = COALESCE(NULLIF(excluded.extras_json, ''), games.extras_json),
			fetched_at            = CURRENT_TIMESTAMP`,
		g.ID, g.Name, g.Slug, g.Summary, g.CoverURL, accentHex,
		g.FirstReleaseDate, g.Rating, g.TimeToBeatMain, g.TimeToBeatComplete, string(g.Raw), extrasJSON)
	if err != nil {
		return err
	}

	if err := replaceRefs(ctx, tx, "genres", "game_genres", "genre_id", g.ID, g.Genres); err != nil {
		return err
	}
	if err := replaceRefs(ctx, tx, "platforms", "game_platforms", "platform_id", g.ID, g.Platforms); err != nil {
		return err
	}

	return tx.Commit()
}

// replaceRefs upserts the lookup rows (genres/platforms) and rewrites the join
// table for one game.
func replaceRefs(ctx context.Context, tx *sql.Tx, lookupTable, joinTable, joinCol string, gameID int64, refs []metadata.Ref) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM `+joinTable+` WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	for _, ref := range refs {
		if ref.ID == 0 || ref.Name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO `+lookupTable+` (id, name) VALUES (?, ?)
			 ON CONFLICT(id) DO UPDATE SET name = excluded.name`, ref.ID, ref.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO `+joinTable+` (game_id, `+joinCol+`) VALUES (?, ?)
			 ON CONFLICT DO NOTHING`, gameID, ref.ID); err != nil {
			return err
		}
	}
	return nil
}

// GetGame returns a cached game with its genres and platforms.
func (s *Store) GetGame(ctx context.Context, id int64) (models.Game, error) {
	games, err := s.gamesByID(ctx, []int64{id})
	if err != nil {
		return models.Game{}, err
	}
	g, ok := games[id]
	if !ok {
		return models.Game{}, ErrNotFound
	}
	return g, nil
}

// CoverURLFor returns the upstream cover URL for a game, used to lazily
// re-download a cover that is missing from disk.
func (s *Store) CoverURLFor(ctx context.Context, id int64) (string, error) {
	var url sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cover_url FROM games WHERE id = ?`, id).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return url.String, err
}

// SetAccent records a sampled accent colour for a game.
func (s *Store) SetAccent(ctx context.Context, id int64, accentHex string) error {
	if accentHex == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE games SET accent_hex = ? WHERE id = ?`, accentHex, id)
	return err
}

// GamesByIDs loads a set of cached games keyed by id.
func (s *Store) GamesByIDs(ctx context.Context, ids []int64) (map[int64]models.Game, error) {
	return s.gamesByID(ctx, ids)
}

// OwnedGameIDs reports which of the given games are already in a user's
// library, so search results can be marked as added.
func (s *Store) OwnedGameIDs(ctx context.Context, userID string, ids []int64) (map[int64]bool, error) {
	owned := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return owned, nil
	}
	placeholders, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id FROM library_entries WHERE user_id = ? AND game_id IN (`+placeholders+`)`,
		append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owned[id] = true
	}
	return owned, rows.Err()
}

// gamesByID loads games and their relations in three queries rather than N+1.
func (s *Store) gamesByID(ctx context.Context, ids []int64) (map[int64]models.Game, error) {
	out := make(map[int64]models.Game, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	placeholders, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(slug,''), COALESCE(summary,''), COALESCE(cover_url,''),
		       COALESCE(accent_hex,''), first_release_date, igdb_rating,
		       time_to_beat_main, time_to_beat_complete, COALESCE(extras_json,'')
		FROM games WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var g models.Game
		var extras string
		if err := rows.Scan(&g.ID, &g.Name, &g.Slug, &g.Summary, &g.CoverURL, &g.AccentHex,
			&g.FirstReleaseDate, &g.IGDBRating, &g.TimeToBeatMain, &g.TimeToBeatComplete, &extras); err != nil {
			return nil, err
		}
		// Served verbatim to the client. Left nil (JSON null) when unfetched, so
		// the detail page knows to backfill it.
		if extras != "" {
			g.Extras = json.RawMessage(extras)
		}
		g.Genres = []models.NamedRef{}
		g.Platforms = []models.NamedRef{}
		out[g.ID] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.attachRefs(ctx, out, ids, "game_genres", "genres", "genre_id", true); err != nil {
		return nil, err
	}
	if err := s.attachRefs(ctx, out, ids, "game_platforms", "platforms", "platform_id", false); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) attachRefs(ctx context.Context, games map[int64]models.Game, ids []int64,
	joinTable, lookupTable, joinCol string, isGenre bool) error {

	placeholders, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, `
		SELECT j.game_id, l.id, l.name
		FROM `+joinTable+` j JOIN `+lookupTable+` l ON l.id = j.`+joinCol+`
		WHERE j.game_id IN (`+placeholders+`)
		ORDER BY l.name`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var gameID int64
		var ref models.NamedRef
		if err := rows.Scan(&gameID, &ref.ID, &ref.Name); err != nil {
			return err
		}
		g, ok := games[gameID]
		if !ok {
			continue
		}
		if isGenre {
			g.Genres = append(g.Genres, ref)
		} else {
			g.Platforms = append(g.Platforms, ref)
		}
		games[gameID] = g
	}
	return rows.Err()
}

// inClause builds "?,?,?" and the matching args for an IN filter.
func inClause(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
