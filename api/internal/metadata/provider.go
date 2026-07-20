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
	Raw         []byte
}

type Ref struct {
	ID   int64
	Name string
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
