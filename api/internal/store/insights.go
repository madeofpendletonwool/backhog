package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Minimum signal before a bucket stat is worth calling a superlative: five
// owned games for a genre or release year, three unplayed games on a platform.
// Below these the "winner" is just noise.
const (
	minBucketOwned     = 5
	minPlatformBacklog = 3
)

// Insights builds the "Your Gaming Problem" dashboard: a headline summary plus
// the superlative stats. The headline reuses the debt report's hours and pace
// math so the two pages can never disagree.
func (s *Store) Insights(ctx context.Context, userID string) (models.Insights, error) {
	debt, err := s.Debt(ctx, userID)
	if err != nil {
		return models.Insights{}, err
	}
	stats, err := s.Stats(ctx, userID)
	if err != nil {
		return models.Insights{}, err
	}

	headline := models.InsightsHeadline{
		// A wishlist is a shopping list, not an owned game.
		GamesOwned:     stats.Total - stats.Wishlist,
		UnplayedGames:  stats.Backlog + stats.Playing,
		HoursRemaining: debt.TotalHours,
	}
	if debt.Projection.CurrentPace != nil {
		years := round1(debt.Projection.CurrentPace.Weeks / 52)
		headline.YearsAtCurrentRate = &years
	}

	insights := models.Insights{Headline: headline, Superlatives: []models.Superlative{}}

	for _, load := range []func() (*models.Superlative, error){
		func() (*models.Superlative, error) { return s.oldestUntouched(ctx, userID) },
		func() (*models.Superlative, error) { return s.longestUnplayed(ctx, userID) },
		func() (*models.Superlative, error) { return s.neglectedGenre(ctx, userID) },
		func() (*models.Superlative, error) { return s.worstPlatform(ctx, userID) },
		func() (*models.Superlative, error) { return s.neglectedYear(ctx, userID) },
	} {
		sup, err := load()
		if err != nil {
			return models.Insights{}, err
		}
		if sup != nil {
			insights.Superlatives = append(insights.Superlatives, *sup)
		}
	}
	return insights, nil
}

// untouched selects backlog entries with no logged play sessions — games that
// entered the library and were never touched again. Game-scoped: time-to-beat
// debt is a games-only idea.
const untouchedWhere = `e.user_id = ? AND e.media_type = 'game' AND e.status = 'backlog'
	AND NOT EXISTS (SELECT 1 FROM play_sessions ps WHERE ps.entry_id = e.id)`

// oldestUntouched finds the untouched entry that has been sitting in the
// library the longest.
func (s *Store) oldestUntouched(ctx context.Context, userID string) (*models.Superlative, error) {
	var entryID string
	var gameID int64
	var addedOn string
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.game_id, date(e.created_at)
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE `+untouchedWhere+`
		ORDER BY e.created_at ASC
		LIMIT 1`, userID).
		Scan(&entryID, &gameID, &addedOn)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	game, err := s.gameForInsight(ctx, gameID)
	if err != nil {
		return nil, err
	}
	year := addedOn
	if len(year) >= 4 {
		year = year[:4]
	}
	return &models.Superlative{
		Kind: models.SuperlativeOldestUntouched,
		Payload: models.SuperlativePayload{
			Game:    game,
			EntryID: entryID,
			AddedOn: addedOn,
		},
		Label: fmt.Sprintf("Purchased %s · 0 hours logged", year),
	}, nil
}

// longestUnplayed finds the untouched entry with the largest remaining
// time-to-beat: the single biggest chunk of unplayed debt.
func (s *Store) longestUnplayed(ctx context.Context, userID string) (*models.Superlative, error) {
	var entryID string
	var gameID int64
	var seconds sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.game_id, g.time_to_beat_main
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE `+untouchedWhere+`
		ORDER BY g.time_to_beat_main DESC
		LIMIT 1`, userID).
		Scan(&entryID, &gameID, &seconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	game, err := s.gameForInsight(ctx, gameID)
	if err != nil {
		return nil, err
	}
	payload := models.SuperlativePayload{Game: game, EntryID: entryID}
	label := "Unknown length"
	if seconds.Valid {
		hours := round1(seconds.Float64 / 3600)
		payload.Hours = &hours
		label = hoursLabel(hours) + " still to play"
	}
	return &models.Superlative{
		Kind:    models.SuperlativeLongestUnplayed,
		Payload: payload,
		Label:   label,
	}, nil
}

// gameForInsight loads one hydrated game for a game-backed superlative, via
// the batched loader so no query here is ever N+1.
func (s *Store) gameForInsight(ctx context.Context, gameID int64) (*models.Game, error) {
	games, err := s.gamesByID(ctx, []int64{gameID})
	if err != nil {
		return nil, err
	}
	game, ok := games[gameID]
	if !ok {
		return nil, ErrNotFound
	}
	return &game, nil
}

// neglectedGenre finds the genre with the worst played ratio among genres the
// user owns at least minBucketOwned games of. A fully-played genre is not a
// problem, so it is excluded.
func (s *Store) neglectedGenre(ctx context.Context, userID string) (*models.Superlative, error) {
	var name string
	var owned, played int
	err := s.db.QueryRowContext(ctx, `
		SELECT gn.name, COUNT(*), COALESCE(SUM(e.status = 'played'), 0)
		FROM library_entries e
		JOIN game_genres gg ON gg.game_id = e.game_id
		JOIN genres gn ON gn.id = gg.genre_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status != 'wishlist'
		GROUP BY gn.id
		HAVING COUNT(*) >= ? AND SUM(e.status = 'played') < COUNT(*)
		ORDER BY SUM(e.status = 'played') * 1.0 / COUNT(*) ASC, COUNT(*) DESC
		LIMIT 1`, userID, minBucketOwned).
		Scan(&name, &owned, &played)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &models.Superlative{
		Kind: models.SuperlativeNeglectedGenre,
		Payload: models.SuperlativePayload{
			Name: name, Owned: owned, Played: played,
		},
		Label: fmt.Sprintf("%s — %d games / %d played", name, owned, played),
	}, nil
}

// worstPlatform finds the platform carrying the most unplayed hours. Games
// count towards every platform they ship on, so overlapping platforms share
// their backlog — that is the honest version of "where the backlog lives".
func (s *Store) worstPlatform(ctx context.Context, userID string) (*models.Superlative, error) {
	var name string
	var count int
	var seconds float64
	err := s.db.QueryRowContext(ctx, `
		SELECT p.name, COUNT(*), COALESCE(SUM(g.time_to_beat_main), 0)
		FROM library_entries e
		JOIN game_platforms gp ON gp.game_id = e.game_id
		JOIN platforms p ON p.id = gp.platform_id
		JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status IN ('backlog','playing')
		GROUP BY p.id
		HAVING COUNT(*) >= ?
		ORDER BY SUM(g.time_to_beat_main) DESC, COUNT(*) DESC
		LIMIT 1`, userID, minPlatformBacklog).
		Scan(&name, &count, &seconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	hours := round1(seconds / 3600)
	return &models.Superlative{
		Kind: models.SuperlativeWorstPlatform,
		Payload: models.SuperlativePayload{
			Name: name, BacklogGames: count, BacklogHours: hours,
		},
		Label: fmt.Sprintf("%s — %d games · %s owed", name, count, hoursLabel(hours)),
	}, nil
}

// neglectedYear is the bonus stat: the release year the user owns the most
// games from and has played the least of. Same shape as neglectedGenre.
func (s *Store) neglectedYear(ctx context.Context, userID string) (*models.Superlative, error) {
	var year string
	var owned, played int
	err := s.db.QueryRowContext(ctx, `
		SELECT strftime('%Y', g.first_release_date, 'unixepoch'), COUNT(*),
		       COALESCE(SUM(e.status = 'played'), 0)
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ? AND e.media_type = 'game' AND e.status != 'wishlist'
		  AND g.first_release_date IS NOT NULL
		GROUP BY 1
		HAVING COUNT(*) >= ? AND SUM(e.status = 'played') < COUNT(*)
		ORDER BY SUM(e.status = 'played') * 1.0 / COUNT(*) ASC, COUNT(*) DESC, 1 ASC
		LIMIT 1`, userID, minBucketOwned).
		Scan(&year, &owned, &played)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	yearNum, err := strconv.Atoi(year)
	if err != nil {
		return nil, err
	}
	return &models.Superlative{
		Kind: models.SuperlativeNeglectedYear,
		Payload: models.SuperlativePayload{
			Year: &yearNum, Owned: owned, Played: played,
		},
		Label: fmt.Sprintf("%d — %d games / %d played", yearNum, owned, played),
	}, nil
}

// hoursLabel renders hours compactly for labels: one decimal under ten hours,
// whole hours above.
func hoursLabel(hours float64) string {
	if hours < 10 {
		return fmt.Sprintf("%.1fh", hours)
	}
	return fmt.Sprintf("%dh", int(math.Round(hours)))
}
