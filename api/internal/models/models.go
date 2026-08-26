package models

import (
	"encoding/json"
	"time"
)

// Status values for a library entry.
const (
	StatusBacklog = "backlog"
	StatusPlaying = "playing"
	StatusPlayed  = "played"
	StatusDropped = "dropped"
	// StatusIgnored is for games you own and have played but will never "beat" —
	// endless or open-ended titles. Like wishlist, it is excluded from completion
	// so a game you'll never finish doesn't drag your progress down forever. It is
	// kept out of the backlog and the play queue.
	StatusIgnored = "ignored"
	// StatusWishlist is for games you want but do not own yet. It is deliberately
	// excluded from backlog totals and completion: a wishlist is a shopping list,
	// not a debt you owe yourself.
	StatusWishlist = "wishlist"
)

// AllStatuses lists every tracked status, in display order.
var AllStatuses = []string{StatusBacklog, StatusPlaying, StatusPlayed, StatusDropped, StatusIgnored, StatusWishlist}

// ValidStatus reports whether s is a tracked status.
func ValidStatus(s string) bool {
	switch s {
	case StatusBacklog, StatusPlaying, StatusPlayed, StatusDropped, StatusIgnored, StatusWishlist:
		return true
	}
	return false
}

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type NamedRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Game is the shared, IGDB-sourced metadata record. Times to beat are seconds.
type Game struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Slug               string     `json:"slug"`
	Summary            string     `json:"summary"`
	CoverURL           string     `json:"cover_url"`
	AccentHex          string     `json:"accent_hex"`
	FirstReleaseDate   *int64     `json:"first_release_date"`
	IGDBRating         *float64   `json:"igdb_rating"`
	TimeToBeatMain     *int64     `json:"time_to_beat_main"`
	TimeToBeatComplete *int64     `json:"time_to_beat_complete"`
	Genres             []NamedRef `json:"genres"`
	Platforms          []NamedRef `json:"platforms"`
	// Extras is the rich, display-only IGDB metadata, stored and served as an
	// opaque JSON document (see metadata.GameExtras for its shape). A null value
	// means it hasn't been fetched yet, which the detail handler uses to trigger
	// a lazy refresh.
	Extras json.RawMessage `json:"extras"`
}

// Entry is one game in one user's library.
type Entry struct {
	ID            string     `json:"id"`
	Game          Game       `json:"game"`
	Status        string     `json:"status"`
	PlatformID    *int64     `json:"platform_id"`
	UserRating    *int       `json:"user_rating"`
	Notes         string     `json:"notes"`
	QueuePosition *float64   `json:"queue_position"`
	LoggedMinutes int        `json:"logged_minutes"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type List struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Rules       *RuleSet  `json:"rules,omitempty"`
	Count       int       `json:"count"`
	CreatedAt   time.Time `json:"created_at"`
}

// RuleSet is the stored definition of a smart list.
type RuleSet struct {
	Match string `json:"match"` // "all" | "any"
	Rules []Rule `json:"rules"`
	Sort  *Sort  `json:"sort,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type Rule struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type Sort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

// Stats summarises a user's library for the dashboard strip.
type Stats struct {
	Total        int     `json:"total"`
	Backlog      int     `json:"backlog"`
	Playing      int     `json:"playing"`
	Played       int     `json:"played"`
	Dropped      int     `json:"dropped"`
	Ignored      int     `json:"ignored"`
	Wishlist     int     `json:"wishlist"`
	BacklogHours float64 `json:"backlog_hours"`
	PlayedHours  float64 `json:"played_hours"`
	// LoggedHours is time you actually recorded playing, as opposed to the
	// crowd-sourced estimates the other two are built from.
	LoggedHours float64 `json:"logged_hours"`
	Completion  float64 `json:"completion"`
}

// Pace is how fast the user is actually playing, from logged sessions. Null
// means there is no data to compute a figure from, not "zero hours".
type Pace struct {
	// HoursPerWeek90d is the trailing 90-day average.
	HoursPerWeek90d *float64 `json:"hours_per_week_90d"`
	// HoursPerWeekAll spans first logged session to now.
	HoursPerWeekAll *float64 `json:"hours_per_week_all"`
}

// ClearanceScenario projects when the backlog debt is fully played at one
// fixed hours/week rate.
type ClearanceScenario struct {
	HoursPerWeek float64 `json:"hours_per_week"`
	Weeks        float64 `json:"weeks"`
	// ClearBy is the estimated clearance date (2027-03-05). Null means the
	// backlog never clears at this pace.
	ClearBy *string `json:"clear_by"`
}

// DebtProjection bundles the current-pace estimate with fixed-rate scenarios.
type DebtProjection struct {
	CurrentPace *ClearanceScenario  `json:"current_pace"`
	Scenarios   []ClearanceScenario `json:"scenarios"`
}

// DebtReport is the backlog-debt summary: unplayed hours split by where they
// sit, actual play pace, and clearance projections.
type DebtReport struct {
	TotalHours       float64 `json:"total_hours"`
	MainBacklogHours float64 `json:"main_backlog_hours"`
	StartedHours     float64 `json:"started_hours"`
	ShortGamesHours  float64 `json:"short_games_hours"`
	// WishlistHours is null by design: a wishlist is a shopping list, not debt
	// you owe yourself. The field stays in the shape so later work can fill it.
	WishlistHours *float64 `json:"wishlist_hours"`
	// DLCHours is the unplayed add-on debt: DLC and expansions linked to games
	// still in the backlog or being played. Null until any DLC links are known.
	DLCHours   *float64       `json:"dlc_hours"`
	Pace       Pace           `json:"pace"`
	Projection DebtProjection `json:"projection"`
}

// Play orders for a series journey.
const (
	PlayOrderRelease       = "release"
	PlayOrderChronological = "chronological"
	PlayOrderRecommended   = "recommended"
	PlayOrderCustom        = "custom"
	PlayOrderGoodOnes      = "good_ones"
)

// AllPlayOrders lists every play order, in display order.
var AllPlayOrders = []string{PlayOrderRelease, PlayOrderChronological, PlayOrderRecommended, PlayOrderCustom, PlayOrderGoodOnes}

// ValidPlayOrder reports whether s is a known play order.
func ValidPlayOrder(s string) bool {
	for _, o := range AllPlayOrders {
		if o == s {
			return true
		}
	}
	return false
}

// Series is a franchise or collection from IGDB ("Mass Effect"), shared across
// users like games are.
type Series struct {
	ID               string `json:"id"`
	IGDBCollectionID *int64 `json:"igdb_collection_id"`
	IGDBFranchiseID  *int64 `json:"igdb_franchise_id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
}

// SeriesSummary is one row of the series index: a series the user owns at
// least two games in, with its journey rolled up.
type SeriesSummary struct {
	Series
	OwnedCount     int       `json:"owned_count"`
	PlayedCount    int       `json:"played_count"`
	Completion     float64   `json:"completion"`
	RemainingHours float64   `json:"remaining_hours"`
	NextGame       *NamedRef `json:"next_game"`
}

// SeriesMember is one game in a series, with the requesting user's state
// attached. Status is "unowned" when the user has no library entry for it.
type SeriesMember struct {
	Game          Game     `json:"game"`
	Kind          string   `json:"kind"` // game | dlc | expansion
	Status        string   `json:"status"`
	EntryID       string   `json:"entry_id,omitempty"`
	Position      *float64 `json:"position,omitempty"` // set when custom-ordered
	LoggedMinutes int      `json:"logged_minutes"`
}

// SeriesDetail is the full series view: settings plus the ordered journey.
type SeriesDetail struct {
	Series
	PlayOrder      string         `json:"play_order"`
	Members        []SeriesMember `json:"members"`
	OwnedCount     int            `json:"owned_count"`
	PlayedCount    int            `json:"played_count"`
	Completion     float64        `json:"completion"`
	RemainingHours float64        `json:"remaining_hours"`
	// DLCHours is the unplayed owned DLC/expansion hours inside this series.
	DLCHours float64 `json:"dlc_hours"`
}

// Session is one manually logged stretch of play.
type Session struct {
	ID        string    `json:"id"`
	EntryID   string    `json:"entry_id"`
	PlayedOn  string    `json:"played_on"`
	Minutes   int       `json:"minutes"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}
