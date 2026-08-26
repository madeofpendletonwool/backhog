package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// Play-order tuning constants. IGDB ratings are on a 0-100 scale.
const (
	// recommendedFloor is the rating a member needs to count as part of the
	// "recommended" core path; everything below it sorts after the core.
	recommendedFloor = 70.0
	// goodOnesThreshold is the rating a member needs to appear at all when the
	// journey is filtered to "just the good ones".
	goodOnesThreshold = 75.0
)

// SeriesBackfillCandidates returns cached games whose series-relevant metadata
// is missing or stale, least-recently-fetched first.
func (s *Store) SeriesBackfillCandidates(ctx context.Context, limit int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM games
		WHERE extras_json IS NULL OR extras_json = ''
		   OR fetched_at < datetime('now', '-30 days')
		ORDER BY fetched_at ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ApplySeriesData writes one backfill step: the DLC/expansion children first
// (so the parent's relations point at real rows), then the parent itself, then
// the children into the parent's series. Re-running with the same data leaves
// the same state — every write is an upsert.
func (s *Store) ApplySeriesData(ctx context.Context, parent metadata.Game, children []metadata.Game) error {
	for _, child := range children {
		if err := s.UpsertGame(ctx, child, ""); err != nil {
			return err
		}
	}
	if err := s.UpsertGame(ctx, parent, ""); err != nil {
		return err
	}

	if parent.Series == nil || parent.Extras == nil || len(children) == 0 {
		return nil
	}

	seriesID, err := s.mainSeriesOfGame(ctx, parent.ID)
	if err != nil {
		return err
	}
	if seriesID == "" {
		return nil
	}

	kindOf := map[int64]string{}
	for _, d := range parent.Extras.DLCs {
		kindOf[d.ID] = "dlc"
	}
	for _, d := range parent.Extras.Expansions {
		kindOf[d.ID] = "expansion"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, child := range children {
		kind, ok := kindOf[child.ID]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO series_games (series_id, game_id, kind) VALUES (?, ?, ?)
			ON CONFLICT (series_id, game_id) DO UPDATE SET kind = excluded.kind`,
			seriesID, child.ID, kind); err != nil {
			return err
		}
	}
	if err := recomputeReleaseOrder(ctx, tx, seriesID); err != nil {
		return err
	}
	return tx.Commit()
}

// mainSeriesOfGame resolves the series a game itself belongs to, preferring
// its own membership over a DLC-inherited one.
func (s *Store) mainSeriesOfGame(ctx context.Context, gameID int64) (string, error) {
	var seriesID string
	err := s.db.QueryRowContext(ctx, `
		SELECT series_id FROM series_games WHERE game_id = ?
		ORDER BY (kind = 'game') DESC, series_id LIMIT 1`, gameID).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return seriesID, err
}

// syncGameSeries writes the relational series rows for one game upsert: the
// series row itself (creating or merging as needed), the game's membership,
// and its DLC/expansion relations. Runs inside the upsert's transaction.
func syncGameSeries(ctx context.Context, tx *sql.Tx, gameID int64, series *metadata.GameSeries, extras *metadata.GameExtras) error {
	if series != nil {
		seriesID, err := upsertSeriesTx(ctx, tx, series)
		if err != nil {
			return err
		}
		if seriesID != "" {
			// The game's own membership never downgrades an existing DLC
			// membership: DLC nesting wins over a direct franchise link.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO series_games (series_id, game_id, kind) VALUES (?, ?, 'game')
				ON CONFLICT (series_id, game_id) DO NOTHING`, seriesID, gameID); err != nil {
				return err
			}
			if err := recomputeReleaseOrder(ctx, tx, seriesID); err != nil {
				return err
			}
		}
	}

	if extras == nil {
		return nil
	}
	for _, dlc := range extras.DLCs {
		if err := upsertGameDLC(ctx, tx, gameID, dlc.ID, "dlc"); err != nil {
			return err
		}
	}
	for _, exp := range extras.Expansions {
		if err := upsertGameDLC(ctx, tx, gameID, exp.ID, "expansion"); err != nil {
			return err
		}
	}
	return nil
}

func upsertGameDLC(ctx context.Context, tx *sql.Tx, parentID, childID int64, kind string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO game_dlc (parent_game_id, game_id, kind) VALUES (?, ?, ?)
		ON CONFLICT (parent_game_id, game_id) DO UPDATE SET kind = excluded.kind`,
		parentID, childID, kind)
	return err
}

// upsertSeriesTx resolves the series row for a game's franchise/collection,
// creating it when unknown. When the two identifiers point at two existing
// rows (a franchise series and a collection series discovered independently),
// they are merged into one: the second row's memberships and user settings
// move across before it is deleted.
func upsertSeriesTx(ctx context.Context, tx *sql.Tx, series *metadata.GameSeries) (string, error) {
	var franchiseID, collectionID *int64
	name, slug := "", ""
	if series.Franchise != nil {
		id := series.Franchise.ID
		franchiseID = &id
		name, slug = series.Franchise.Name, series.Franchise.Slug
	}
	if series.Collection != nil {
		id := series.Collection.ID
		collectionID = &id
		if name == "" {
			name, slug = series.Collection.Name, series.Collection.Slug
		}
	}
	if name == "" {
		return "", nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM series
		WHERE igdb_franchise_id = ? OR igdb_collection_id = ?`,
		franchiseID, collectionID)
	if err != nil {
		return "", err
	}
	var matched []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		matched = append(matched, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(matched) == 0 {
		id := newID()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO series (id, igdb_collection_id, igdb_franchise_id, name, slug)
			VALUES (?, ?, ?, ?, ?)`,
			id, collectionID, franchiseID, name, slug); err != nil {
			return "", err
		}
		return id, nil
	}

	target := matched[0]
	if _, err := tx.ExecContext(ctx, `
		UPDATE series SET
			igdb_franchise_id = COALESCE(igdb_franchise_id, ?),
			igdb_collection_id = COALESCE(igdb_collection_id, ?),
			name = CASE WHEN name = '' THEN ? ELSE name END
		WHERE id = ?`,
		franchiseID, collectionID, name, target); err != nil {
		return "", err
	}

	for _, other := range matched[1:] {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO series_games (series_id, game_id, kind, release_order)
			SELECT ?, game_id, kind, release_order FROM series_games WHERE series_id = ?
			ON CONFLICT (series_id, game_id) DO NOTHING`, target, other); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_series (user_id, series_id, play_order, created_at, updated_at)
			SELECT user_id, ?, play_order, created_at, updated_at FROM user_series WHERE series_id = ?
			ON CONFLICT (user_id, series_id) DO NOTHING`, target, other); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_series_order (user_id, series_id, game_id, position)
			SELECT user_id, ?, game_id, position FROM user_series_order WHERE series_id = ?
			ON CONFLICT (user_id, series_id, game_id) DO NOTHING`, target, other); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM series WHERE id = ?`, other); err != nil {
			return "", err
		}
	}
	return target, nil
}

// recomputeReleaseOrder rewrites each member's rank within a series by IGDB
// first release date, so the stored order survives independent of the query.
func recomputeReleaseOrder(ctx context.Context, tx *sql.Tx, seriesID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT sg.game_id FROM series_games sg
		JOIN games g ON g.id = sg.game_id
		WHERE sg.series_id = ?
		ORDER BY g.first_release_date ASC NULLS LAST, g.name COLLATE NOCASE ASC`, seriesID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE series_games SET release_order = ? WHERE series_id = ? AND game_id = ?`,
			i+1, seriesID, id); err != nil {
			return err
		}
	}
	return nil
}

// seriesRow is the sliver of a series member every ordering mode needs. The
// full detail hydration hangs off it.
type seriesRow struct {
	gameID   int64
	kind     string
	status   string // "" when the user has no entry for the game
	entryID  string
	parentID int64 // owning game for DLC members, 0 for base games
	position *float64
	rating   *float64
	date     *int64
	ttb      *int64
	name     string
	logged   int
}

// SeriesForGame lists the series a cached game belongs to, for the detail chip.
func (s *Store) SeriesForGame(ctx context.Context, gameID int64) ([]models.Series, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.igdb_collection_id, s.igdb_franchise_id, s.name, COALESCE(s.slug, '')
		FROM series s JOIN series_games sg ON sg.series_id = s.id
		WHERE sg.game_id = ?
		ORDER BY s.name COLLATE NOCASE`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Series{}
	for rows.Next() {
		var sr models.Series
		if err := rows.Scan(&sr.ID, &sr.IGDBCollectionID, &sr.IGDBFranchiseID, &sr.Name, &sr.Slug); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// SeriesIndex lists every series the user owns at least two games in, with the
// journey rolled up.
func (s *Store) SeriesIndex(ctx context.Context, userID string) ([]models.SeriesSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id FROM series s
		WHERE (SELECT COUNT(*) FROM series_games sg
		       JOIN library_entries e ON e.game_id = sg.game_id
		       WHERE sg.series_id = s.id AND e.user_id = ?
		         AND e.status NOT IN ('wishlist','ignored')) >= 2
		ORDER BY s.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.SeriesSummary, 0, len(ids))
	for _, id := range ids {
		summary, err := s.seriesRollup(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

// seriesRollup computes one index card: series fields, counts, remaining
// hours, and the next game in the user's chosen play order.
func (s *Store) seriesRollup(ctx context.Context, userID, seriesID string) (models.SeriesSummary, error) {
	var summary models.SeriesSummary
	series, order, err := s.seriesHeader(ctx, userID, seriesID)
	if err != nil {
		return summary, err
	}
	summary.Series = series

	rows, err := s.seriesRows(ctx, userID, seriesID)
	if err != nil {
		return summary, err
	}

	for _, r := range rows {
		if r.status == "" || r.status == models.StatusWishlist || r.status == models.StatusIgnored {
			continue
		}
		summary.OwnedCount++
		if r.status == models.StatusPlayed {
			summary.PlayedCount++
		}
		summary.RemainingHours += remainingHours(r.ttb, r.status, r.logged)
	}
	summary.RemainingHours = round1(summary.RemainingHours)
	if summary.OwnedCount > 0 {
		summary.Completion = round1(float64(summary.PlayedCount) / float64(summary.OwnedCount) * 100)
	}

	sortSeriesRows(rows, order)
	if next := nextUp(rows, order); next != nil {
		summary.NextGame = next
	}
	return summary, nil
}

// remainingHours converts one owned member's time-to-beat into the hours
// still owed: the full estimate for a backlog game, the estimate minus logged
// time for one in progress. Anything already finished owes nothing.
func remainingHours(ttbSeconds *int64, status string, loggedMinutes int) float64 {
	if status != models.StatusBacklog && status != models.StatusPlaying {
		return 0
	}
	if ttbSeconds == nil || *ttbSeconds <= 0 {
		return 0
	}
	remaining := float64(*ttbSeconds)
	if status == models.StatusPlaying {
		remaining -= float64(loggedMinutes) * 60
	}
	if remaining <= 0 {
		return 0
	}
	return remaining / 3600
}

// nextUp returns the first unplayed owned member in the resolved order — the
// "next up: <game>" of the index card. "Just the good ones" skips members
// below its rating floor.
func nextUp(rows []seriesRow, order string) *models.NamedRef {
	for _, r := range rows {
		if r.status != models.StatusBacklog && r.status != models.StatusPlaying {
			continue
		}
		if order == models.PlayOrderGoodOnes && (r.rating == nil || *r.rating < goodOnesThreshold) {
			continue
		}
		return &models.NamedRef{ID: r.gameID, Name: r.name}
	}
	return nil
}

// SeriesDetail returns the full series view with members in the user's
// resolved play order.
func (s *Store) SeriesDetail(ctx context.Context, userID, seriesID string) (models.SeriesDetail, error) {
	var detail models.SeriesDetail
	series, order, err := s.seriesHeader(ctx, userID, seriesID)
	if err != nil {
		return detail, err
	}
	detail.Series = series
	detail.PlayOrder = order

	rows, err := s.seriesRows(ctx, userID, seriesID)
	if err != nil {
		return detail, err
	}

	gameIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		gameIDs = append(gameIDs, r.gameID)
	}
	games, err := s.gamesByID(ctx, gameIDs)
	if err != nil {
		return detail, err
	}

	sortSeriesRows(rows, order)
	detail.Members = make([]models.SeriesMember, 0, len(rows))
	for _, r := range rows {
		member := models.SeriesMember{
			Kind:          r.kind,
			Status:        "unowned",
			EntryID:       r.entryID,
			Position:      r.position,
			LoggedMinutes: r.logged,
		}
		if r.status != "" {
			member.Status = r.status
		}
		if g, ok := games[r.gameID]; ok {
			member.Game = g
		} else {
			member.Game = models.Game{ID: r.gameID, Name: r.name}
		}
		detail.Members = append(detail.Members, member)

		if r.status == "" || r.status == models.StatusWishlist || r.status == models.StatusIgnored {
			continue
		}
		detail.OwnedCount++
		if r.kind != "game" && (r.status == models.StatusBacklog || r.status == models.StatusPlaying) {
			ttb := member.Game.TimeToBeatMain
			if ttb != nil && *ttb > 0 {
				detail.DLCHours += float64(*ttb) / 3600
			}
		}
		if r.status == models.StatusPlayed {
			detail.PlayedCount++
		}
		detail.RemainingHours += remainingHours(member.Game.TimeToBeatMain, r.status, r.logged)
	}
	detail.RemainingHours = round1(detail.RemainingHours)
	detail.DLCHours = round1(detail.DLCHours)
	if detail.OwnedCount > 0 {
		detail.Completion = round1(float64(detail.PlayedCount) / float64(detail.OwnedCount) * 100)
	}
	return detail, nil
}

// seriesHeader loads the series row and the user's play order for it.
func (s *Store) seriesHeader(ctx context.Context, userID, seriesID string) (models.Series, string, error) {
	var series models.Series
	var slug string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, igdb_collection_id, igdb_franchise_id, name, COALESCE(slug,'')
		FROM series WHERE id = ?`, seriesID).
		Scan(&series.ID, &series.IGDBCollectionID, &series.IGDBFranchiseID, &series.Name, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		return series, "", ErrNotFound
	}
	if err != nil {
		return series, "", err
	}
	series.Slug = slug

	order := models.PlayOrderRelease
	err = s.db.QueryRowContext(ctx,
		`SELECT play_order FROM user_series WHERE user_id = ? AND series_id = ?`,
		userID, seriesID).Scan(&order)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return series, "", err
	}
	return series, order, nil
}

// seriesRows loads the ordering-relevant columns for every member, joined to
// the user's library state, custom positions, and DLC parentage.
func (s *Store) seriesRows(ctx context.Context, userID, seriesID string) ([]seriesRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sg.game_id, sg.kind,
		       COALESCE(e.status, ''), COALESCE(e.id, ''),
		       COALESCE(d.parent_game_id, 0), uso.position,
		       g.igdb_rating, g.first_release_date, g.name, g.time_to_beat_main,
		       COALESCE((SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0)
		FROM series_games sg
		JOIN games g ON g.id = sg.game_id
		LEFT JOIN library_entries e ON e.game_id = sg.game_id AND e.user_id = ?
		LEFT JOIN (SELECT game_id, MIN(parent_game_id) AS parent_game_id
		           FROM game_dlc GROUP BY game_id) d ON d.game_id = sg.game_id
		LEFT JOIN user_series_order uso
		       ON uso.user_id = ? AND uso.series_id = sg.series_id AND uso.game_id = sg.game_id
		WHERE sg.series_id = ?`, userID, userID, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []seriesRow{}
	for rows.Next() {
		var r seriesRow
		if err := rows.Scan(&r.gameID, &r.kind, &r.status, &r.entryID, &r.parentID,
			&r.position, &r.rating, &r.date, &r.name, &r.ttb, &r.logged); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// sortSeriesRows orders a journey according to the play-order mode:
//
//   - release:      IGDB first release date, oldest first.
//   - chronological: each game immediately followed by its own DLC and
//     expansions, so a playthrough finishes a game's add-ons
//     before moving on.
//   - recommended:  the well-regarded core (IGDB rating ≥ 70) in release
//     order first, the rest after.
//   - custom:       the user's dragged order; members never positioned land
//     at the end by release date.
//   - good_ones:    release order — the rating filter is applied on top.
func sortSeriesRows(rows []seriesRow, order string) {
	byDate := func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.date != nil && b.date != nil && *a.date != *b.date {
			return *a.date < *b.date
		}
		if (a.date == nil) != (b.date == nil) {
			return a.date != nil
		}
		return a.name < b.name
	}

	switch order {
	case models.PlayOrderCustom:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			switch {
			case a.position != nil && b.position != nil:
				if *a.position != *b.position {
					return *a.position < *b.position
				}
				return byDate(i, j)
			case a.position != nil:
				return true
			case b.position != nil:
				return false
			}
			return byDate(i, j)
		})

	case models.PlayOrderRecommended:
		core := func(r seriesRow) bool { return r.rating != nil && *r.rating >= recommendedFloor }
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if core(a) != core(b) {
				return core(a)
			}
			return byDate(i, j)
		})

	case models.PlayOrderChronological:
		inSeries := make(map[int64]bool, len(rows))
		for _, r := range rows {
			inSeries[r.gameID] = true
		}
		// A member nests under its parent only when the parent is also a
		// member; otherwise it behaves as a base game.
		nested := make(map[int64][]seriesRow)
		var bases []seriesRow
		for _, r := range rows {
			if r.parentID != 0 && r.parentID != r.gameID && inSeries[r.parentID] {
				nested[r.parentID] = append(nested[r.parentID], r)
			} else {
				bases = append(bases, r)
			}
		}
		for _, children := range nested {
			sort.SliceStable(children, func(i, j int) bool { return byDate(i, j) })
		}
		sort.SliceStable(bases, func(i, j int) bool { return byDate(i, j) })

		ordered := make([]seriesRow, 0, len(rows))
		for _, base := range bases {
			ordered = append(ordered, base)
			ordered = append(ordered, nested[base.gameID]...)
		}
		copy(rows, ordered)

	default: // release, good_ones
		sort.SliceStable(rows, func(i, j int) bool { return byDate(i, j) })
	}
}

// SetSeriesPlayOrder stores a user's play-order choice for a series. Switching
// to custom with no stored positions seeds them from release order, so the
// first drag starts from a sensible list.
func (s *Store) SetSeriesPlayOrder(ctx context.Context, userID, seriesID, order string) error {
	if !models.ValidPlayOrder(order) {
		return fmt.Errorf("invalid play order %q", order)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM series WHERE id = ?`, seriesID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_series (user_id, series_id, play_order) VALUES (?, ?, ?)
		ON CONFLICT (user_id, series_id) DO UPDATE SET
			play_order = excluded.play_order, updated_at = CURRENT_TIMESTAMP`,
		userID, seriesID, order); err != nil {
		return err
	}

	if order == models.PlayOrderCustom {
		var positioned int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM user_series_order WHERE user_id = ? AND series_id = ?`,
			userID, seriesID).Scan(&positioned); err != nil {
			return err
		}
		if positioned == 0 {
			if err := seedCustomOrder(ctx, tx, userID, seriesID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func seedCustomOrder(ctx context.Context, tx *sql.Tx, userID, seriesID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT sg.game_id FROM series_games sg
		JOIN games g ON g.id = sg.game_id
		WHERE sg.series_id = ?
		ORDER BY g.first_release_date ASC NULLS LAST, g.name COLLATE NOCASE ASC`, seriesID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_series_order (user_id, series_id, game_id, position) VALUES (?, ?, ?, ?)
			ON CONFLICT (user_id, series_id, game_id) DO NOTHING`,
			userID, seriesID, id, float64(i+1)*positionGap); err != nil {
			return err
		}
	}
	return nil
}

// MoveSeriesGame repositions a game within a custom journey between the games
// identified by beforeID and afterID (0 meaning an end of the list). Same
// fractional-index scheme as the play queue: one row per move, with a
// renormalise when positions converge.
func (s *Store) MoveSeriesGame(ctx context.Context, userID, seriesID string, gameID, beforeID, afterID int64) error {
	err := s.moveSeriesGameOnce(ctx, userID, seriesID, gameID, beforeID, afterID)
	if errors.Is(err, errNeedsRenormalize) {
		if err := s.renormalizeSeriesOrder(ctx, userID, seriesID); err != nil {
			return err
		}
		return s.moveSeriesGameOnce(ctx, userID, seriesID, gameID, beforeID, afterID)
	}
	return err
}

func (s *Store) moveSeriesGameOnce(ctx context.Context, userID, seriesID string, gameID, beforeID, afterID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM series_games WHERE series_id = ? AND game_id = ?`, seriesID, gameID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	before, err := seriesPositionOf(ctx, tx, userID, seriesID, beforeID)
	if err != nil {
		return err
	}
	after, err := seriesPositionOf(ctx, tx, userID, seriesID, afterID)
	if err != nil {
		return err
	}

	pos, err := midpoint(before, after)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_series_order (user_id, series_id, game_id, position) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, series_id, game_id) DO UPDATE SET position = excluded.position`,
		userID, seriesID, gameID, pos); err != nil {
		return err
	}
	return tx.Commit()
}

func seriesPositionOf(ctx context.Context, tx *sql.Tx, userID, seriesID string, gameID int64) (*float64, error) {
	if gameID == 0 {
		return nil, nil
	}
	var pos sql.NullFloat64
	err := tx.QueryRowContext(ctx,
		`SELECT position FROM user_series_order WHERE user_id = ? AND series_id = ? AND game_id = ?`,
		userID, seriesID, gameID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !pos.Valid {
		return nil, nil
	}
	return &pos.Float64, nil
}

// renormalizeSeriesOrder rewrites the user's custom positions at even spacing.
// Members that never had a position are appended at the end, in release order.
func (s *Store) renormalizeSeriesOrder(ctx context.Context, userID, seriesID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT sg.game_id, uso.position FROM series_games sg
		JOIN games g ON g.id = sg.game_id
		LEFT JOIN user_series_order uso
		       ON uso.user_id = ? AND uso.series_id = sg.series_id AND uso.game_id = sg.game_id
		WHERE sg.series_id = ?
		ORDER BY uso.position ASC NULLS LAST, g.first_release_date ASC NULLS LAST, g.name COLLATE NOCASE ASC`,
		userID, seriesID)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		pos *float64
	}
	var ordered []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.pos); err != nil {
			rows.Close()
			return err
		}
		ordered = append(ordered, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i, r := range ordered {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_series_order (user_id, series_id, game_id, position) VALUES (?, ?, ?, ?)
			ON CONFLICT (user_id, series_id, game_id) DO UPDATE SET position = excluded.position`,
			userID, seriesID, r.id, float64(i+1)*positionGap); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DLCHours sums the unplayed add-on debt: time-to-beat of DLC and expansions
// attached to games the user still owes (backlog or in progress). Nil when no
// DLC links are known for the user's library, so the debt report can keep the
// row empty rather than showing a misleading zero.
func (s *Store) DLCHours(ctx context.Context, userID string) (*float64, error) {
	var seconds float64
	var known int
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(g.time_to_beat_main), 0), COUNT(g.id)
		FROM game_dlc gd
		JOIN library_entries e ON e.game_id = gd.parent_game_id
		JOIN games g ON g.id = gd.game_id
		WHERE e.user_id = ? AND e.status IN ('backlog','playing')`, userID).
		Scan(&seconds, &known)
	if err != nil {
		return nil, err
	}
	if known == 0 {
		return nil, nil
	}
	hours := round1(seconds / 3600)
	return &hours, nil
}
