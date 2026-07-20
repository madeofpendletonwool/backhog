package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	igdbBaseURL    = "https://api.igdb.com/v4"
	twitchTokenURL = "https://id.twitch.tv/oauth2/token"
	// IGDB serves covers from a CDN with fixed size presets.
	coverTemplate = "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/%s.jpg"
	// Fields we request for every game lookup.
	gameFields = "fields name,slug,summary,cover.image_id,genres.id,genres.name," +
		"platforms.id,platforms.name,first_release_date,rating,total_rating,total_rating_count;"
)

// IGDB is a Provider backed by IGDB, authenticated through Twitch.
type IGDB struct {
	clientID string
	secret   string
	http     *http.Client
	limiter  *rate.Limiter

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewIGDB constructs an IGDB client. IGDB allows 4 requests per second; we stay
// just under that with a small burst for the parallel time-to-beat lookup.
func NewIGDB(clientID, secret string) *IGDB {
	return &IGDB{
		clientID: clientID,
		secret:   secret,
		http:     &http.Client{Timeout: 15 * time.Second},
		limiter:  rate.NewLimiter(rate.Limit(3), 2),
	}
}

// Search returns games matching a free-text query.
//
// IGDB's `search` operator is deliberately *not* the primary strategy here. It
// ranks purely on name similarity, so short titles dominate: searching "hollow"
// returns three obscure games called "Hollow" plus "Hollow Jump" and "Hollow
// Halls" before it reaches Hollow Knight. It also matches whole tokens only, so
// a trailing partial word ("hollow kni") scores nothing at all.
//
// A substring match ordered by how many people have rated each game gets both
// cases right, and is what you want for a game picker: type a few characters,
// see the games you've plausibly heard of. `search` stays as a fallback for
// when substring finds nothing — reordered words, or a typo.
func (c *IGDB) Search(ctx context.Context, query string, limit int) ([]Game, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	// version_parent = null drops the "Game of the Year Edition" duplicates that
	// otherwise crowd out the base game.
	primary := fmt.Sprintf(
		`where name ~ *%q* & version_parent = null; %s sort total_rating_count desc; limit %d;`,
		escapeQuotes(query), gameFields, limit)

	games, err := c.queryGames(ctx, primary, false)
	if err != nil {
		return nil, err
	}
	if len(games) > 0 {
		rankByRelevanceAndPopularity(games, query)
		return games, nil
	}

	games, err = c.queryGames(ctx,
		fmt.Sprintf("search %q; %s limit %d;", escapeQuotes(query), gameFields, limit), false)
	if err != nil {
		return nil, err
	}
	rankByRelevanceAndPopularity(games, query)
	return games, nil
}

// GetByID returns a single game by its IGDB id, including time-to-beat.
func (c *IGDB) GetByID(ctx context.Context, id int64) (Game, error) {
	body := fmt.Sprintf("where id = %d; %s limit 1;", id, gameFields)
	games, err := c.queryGames(ctx, body, true)
	if err != nil {
		return Game{}, err
	}
	if len(games) == 0 {
		return Game{}, fmt.Errorf("igdb: game %d not found", id)
	}
	return games[0], nil
}

// escapeQuotes keeps a user's quote characters from terminating the APICalypse
// string literal. %q handles Go-side escaping; this guards the input first.
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, "")
}

// rankByRelevanceAndPopularity re-orders search hits in place.
//
// IGDB's relevance is driven by name similarity alone, so searching "hollow"
// buries Hollow Knight under a handful of obscure games literally called
// "Hollow". Blending in how many people have rated a game fixes that without
// throwing relevance away: popularity is worth at most a few places, so a
// genuinely better name match still wins.
func rankByRelevanceAndPopularity(games []Game, query string) {
	query = strings.ToLower(strings.TrimSpace(query))

	score := make(map[int64]float64, len(games))
	for index, game := range games {
		name := strings.ToLower(game.Name)

		// Lower is better: start from the position IGDB gave it.
		s := float64(index)

		// log10 keeps a 50,000-rating blockbuster from completely outranking
		// relevance the way a raw count would.
		s -= 2.0 * math.Log10(float64(game.RatingCount)+1)

		// An exact title, or one that starts with what was typed, is almost
		// always what the user meant.
		switch {
		case name == query:
			s -= 6
		case strings.HasPrefix(name, query):
			s -= 3
		}

		score[game.ID] = s
	}

	sort.SliceStable(games, func(i, j int) bool {
		return score[games[i].ID] < score[games[j].ID]
	})
}

type igdbGame struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Summary string `json:"summary"`
	Cover   struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Genres           []Ref    `json:"genres"`
	Platforms        []Ref    `json:"platforms"`
	FirstReleaseDate *int64   `json:"first_release_date"`
	Rating           *float64 `json:"rating"`
	TotalRating      *float64 `json:"total_rating"`
	TotalRatingCount int      `json:"total_rating_count"`
}

// queryGames runs an APICalypse game query. withTimeToBeat controls the extra
// round trip for playtime data: search skips it to stay fast and well under the
// rate limit, and it is filled in when a game is actually added.
func (c *IGDB) queryGames(ctx context.Context, body string, withTimeToBeat bool) ([]Game, error) {
	raw, err := c.post(ctx, "/games", body)
	if err != nil {
		return nil, err
	}

	var parsed []igdbGame
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("igdb: decode games: %w", err)
	}

	games := make([]Game, 0, len(parsed))
	ids := make([]int64, 0, len(parsed))
	for _, p := range parsed {
		g := Game{
			ID:               p.ID,
			Name:             p.Name,
			Slug:             p.Slug,
			Summary:          p.Summary,
			FirstReleaseDate: p.FirstReleaseDate,
			RatingCount:      p.TotalRatingCount,
			Genres:           p.Genres,
			Platforms:        p.Platforms,
		}
		// Prefer total_rating: it blends critic and user scores, so it is
		// populated for far more games than `rating` alone.
		if p.TotalRating != nil {
			g.Rating = p.TotalRating
		} else {
			g.Rating = p.Rating
		}
		if p.Cover.ImageID != "" {
			g.CoverURL = fmt.Sprintf(coverTemplate, p.Cover.ImageID)
		}
		if encoded, err := json.Marshal(p); err == nil {
			g.Raw = encoded
		}
		games = append(games, g)
		ids = append(ids, p.ID)
	}

	// Time-to-beat lives on a separate endpoint and cannot be expanded inline.
	// It is a nice-to-have, so a failure here degrades rather than fails.
	if withTimeToBeat {
		if ttb, err := c.timesToBeat(ctx, ids); err != nil {
			slog.Warn("igdb: time-to-beat lookup failed", "error", err)
		} else {
			for i := range games {
				if t, ok := ttb[games[i].ID]; ok {
					games[i].TimeToBeatMain = t.main
					games[i].TimeToBeatComplete = t.complete
				}
			}
		}
	}

	return games, nil
}

type beatTimes struct {
	main     *int64
	complete *int64
}

func (c *IGDB) timesToBeat(ctx context.Context, ids []int64) (map[int64]beatTimes, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = fmt.Sprintf("%d", id)
	}
	body := fmt.Sprintf("fields game_id,normally,completely; where game_id = (%s); limit %d;",
		strings.Join(list, ","), len(ids))

	raw, err := c.post(ctx, "/game_time_to_beats", body)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		GameID     int64  `json:"game_id"`
		Normally   *int64 `json:"normally"`
		Completely *int64 `json:"completely"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode time to beat: %w", err)
	}

	out := make(map[int64]beatTimes, len(rows))
	for _, r := range rows {
		out[r.GameID] = beatTimes{main: r.Normally, complete: r.Completely}
	}
	return out, nil
}

// post issues an APICalypse query, refreshing the token once on a 401.
func (c *IGDB) post(ctx context.Context, path, body string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	raw, status, err := c.doPost(ctx, path, body, false)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		// Token was revoked or expired early; force a refresh and retry once.
		raw, status, err = c.doPost(ctx, path, body, true)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("igdb: %s returned %d: %s", path, status, truncate(string(raw), 200))
	}
	return raw, nil
}

func (c *IGDB) doPost(ctx context.Context, path, body string, forceRefresh bool) ([]byte, int, error) {
	token, err := c.accessToken(ctx, forceRefresh)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, igdbBaseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("igdb: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("igdb: read %s: %w", path, err)
	}
	return raw, resp.StatusCode, nil
}

// accessToken returns a cached Twitch app token, fetching a new one when it is
// missing, near expiry, or explicitly invalidated by a 401.
func (c *IGDB) accessToken(ctx context.Context, forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !forceRefresh && c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}

	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.secret},
		"grant_type":    {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("twitch: token request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("twitch: token request returned %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&token); err != nil {
		return "", fmt.Errorf("twitch: decode token: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("twitch: empty access token")
	}

	c.token = token.AccessToken
	// Renew a minute early to avoid racing the expiry on a slow request.
	c.tokenExp = time.Now().Add(time.Duration(token.ExpiresIn)*time.Second - time.Minute)
	slog.Info("igdb: obtained access token", "expires_in", token.ExpiresIn)
	return c.token, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
