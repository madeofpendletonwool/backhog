package metadata

import (
	"context"
	"errors"
)

// ErrUnavailable is returned when no metadata provider is configured.
var ErrUnavailable = errors.New("metadata provider is not configured")

// Game is a provider-agnostic metadata record. Times to beat are in seconds.
type Game struct {
	ID                 int64
	Name               string
	Slug               string
	Summary            string
	CoverURL           string
	FirstReleaseDate   *int64
	Rating             *float64
	TimeToBeatMain     *int64
	TimeToBeatComplete *int64
	// Popularity, used only to re-rank search results.
	RatingCount int
	Genres      []Ref
	Platforms   []Ref
	// Extras is the richer, display-only metadata. It is populated only on a
	// full detail lookup (GetByID), not on search or Steam import — those stay
	// lean, and the detail page backfills the rest lazily. Nil means "not fetched
	// yet", which is the signal the store uses to decide whether to refresh.
	Extras *GameExtras
	Raw    []byte
}

type Ref struct {
	ID   int64
	Name string
}

// GameExtras is the rich, display-only IGDB metadata surfaced on the detail
// page. None of it is filtered or sorted on, so the store persists it as one
// JSON document rather than in relational tables.
type GameExtras struct {
	Developer          string        `json:"developer"`
	Publisher          string        `json:"publisher"`
	Storyline          string        `json:"storyline"`
	AggregatedRating   *float64      `json:"aggregated_rating"`
	Category           string        `json:"category"`
	GameModes          []string      `json:"game_modes"`
	PlayerPerspectives []string      `json:"player_perspectives"`
	Themes             []string      `json:"themes"`
	Franchise          string        `json:"franchise"`
	Collection         string        `json:"collection"`
	AlternativeNames   []string      `json:"alternative_names"`
	AgeRatings         []string      `json:"age_ratings"`
	Websites           []GameWebsite `json:"websites"`
	ScreenshotImageIDs []string      `json:"screenshot_image_ids"`
	Videos             []GameVideo   `json:"videos"`
	SimilarGames       []RelatedGame `json:"similar_games"`
	DLCs               []RelatedGame `json:"dlcs"`
	Expansions         []RelatedGame `json:"expansions"`
}

// GameWebsite is an external link with a human-readable kind.
type GameWebsite struct {
	URL      string `json:"url"`
	Category string `json:"category"`
}

// GameVideo is a trailer or gameplay clip, identified by its YouTube id.
type GameVideo struct {
	VideoID string `json:"video_id"`
	Name    string `json:"name"`
}

// RelatedGame is another game referenced by this one (similar / DLC / expansion).
// Only enough is kept to render a thumbnail that links back into search.
type RelatedGame struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	CoverImageID string `json:"cover_image_id"`
}

// Provider fetches game metadata from an upstream catalogue. The interface is
// deliberately narrow so a second source (RAWG, MobyGames) can be added without
// touching the handlers.
type Provider interface {
	Search(ctx context.Context, query string, limit int) ([]Game, error)
	GetByID(ctx context.Context, id int64) (Game, error)
}

// Unconfigured is the Provider used when credentials are absent. The app still
// serves everything already in the local cache; only new lookups fail.
type Unconfigured struct{}

func (Unconfigured) Search(context.Context, string, int) ([]Game, error) {
	return nil, ErrUnavailable
}

func (Unconfigured) GetByID(context.Context, int64) (Game, error) {
	return Game{}, ErrUnavailable
}
