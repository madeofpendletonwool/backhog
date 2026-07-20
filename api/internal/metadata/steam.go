package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrSteamUnavailable is returned when no Steam API key is configured.
var ErrSteamUnavailable = errors.New("steam import is not configured")

// ErrSteamPrivate is returned when a profile exists but hides its game list.
var ErrSteamPrivate = errors.New("that Steam profile's game details are private")

// Steam reads a user's owned games from the Steam Web API.
//
// The API key is a developer key belonging to this deployment, not to the user:
// one key can query any *public* profile. So the operator sets it once and users
// only supply their own SteamID.
type Steam struct {
	apiKey string
	http   *http.Client
}

func NewSteam(apiKey string) *Steam {
	return &Steam{apiKey: apiKey, http: &http.Client{Timeout: 20 * time.Second}}
}

// Enabled reports whether a key is configured.
func (s *Steam) Enabled() bool { return s != nil && s.apiKey != "" }

// SteamGame is one owned Steam title.
type SteamGame struct {
	AppID int64  `json:"appid"`
	Name  string `json:"name"`
}

var steamID64 = regexp.MustCompile(`^\d{17}$`)

// ResolveID turns whatever the user pasted — a 17-digit SteamID64, a vanity
// name, or a full profile URL — into a SteamID64.
func (s *Steam) ResolveID(ctx context.Context, input string) (string, error) {
	if !s.Enabled() {
		return "", ErrSteamUnavailable
	}

	input = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(input), "/"))

	// Accept a pasted profile URL by taking its last path segment.
	if strings.Contains(input, "steamcommunity.com") {
		parts := strings.Split(input, "/")
		input = parts[len(parts)-1]
	}
	if steamID64.MatchString(input) {
		return input, nil
	}

	endpoint := fmt.Sprintf(
		"https://api.steampowered.com/ISteamUser/ResolveVanityURL/v1/?key=%s&vanityurl=%s",
		url.QueryEscape(s.apiKey), url.QueryEscape(input))

	var payload struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
		} `json:"response"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return "", err
	}
	if payload.Response.Success != 1 || payload.Response.SteamID == "" {
		return "", fmt.Errorf("could not find a Steam profile called %q", input)
	}
	return payload.Response.SteamID, nil
}

// OwnedGames lists the games on a public Steam profile.
func (s *Steam) OwnedGames(ctx context.Context, steamID string) ([]SteamGame, error) {
	if !s.Enabled() {
		return nil, ErrSteamUnavailable
	}

	endpoint := fmt.Sprintf(
		"https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/?key=%s&steamid=%s&include_appinfo=1&include_played_free_games=1",
		url.QueryEscape(s.apiKey), url.QueryEscape(steamID))

	var payload struct {
		Response struct {
			GameCount int         `json:"game_count"`
			Games     []SteamGame `json:"games"`
		} `json:"response"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}

	// Steam answers 200 with an empty object for a private profile rather than
	// an error, so an absent games list is the only signal we get.
	if payload.Response.Games == nil {
		return nil, ErrSteamPrivate
	}
	return payload.Response.Games, nil
}

func (s *Steam) getJSON(ctx context.Context, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("steam request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("steam read: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("steam rejected the API key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam returned %d: %s", resp.StatusCode, truncate(string(body), 160))
	}
	return json.Unmarshal(body, dst)
}
