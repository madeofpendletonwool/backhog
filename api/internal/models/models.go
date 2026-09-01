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

// Media types for a library entry. Books are the second media type; the books
// subject table itself arrives in a later stage.
const (
	MediaGame = "game"
	MediaBook = "book"
)

// AllMediaTypes lists every tracked media type, in display order.
var AllMediaTypes = []string{MediaGame, MediaBook}

// ValidMediaType reports whether s is a tracked media type.
func ValidMediaType(s string) bool {
	switch s {
	case MediaGame, MediaBook:
		return true
	}
	return false
}

// Media file kinds for the scanned Books arena library. Audio covers mp3,
// m4a and m4b; epub is text. Anything else (.aax, .aaxc, DRM-wrapped epub,
// ...) is unsupported and never inventoried.
const (
	MediaFileAudio = "audio"
	MediaFileEpub  = "epub"
)

// ValidMediaFileKind reports whether s is a tracked media file kind.
func ValidMediaFileKind(s string) bool {
	switch s {
	case MediaFileAudio, MediaFileEpub:
		return true
	}
	return false
}

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

// Platform is an IGDB platform with its curated classification. Family
// degrades to "other" and Manufacturer to "Other" for platforms the catalog
// does not classify; Generation is null for platforms that do not map to a
// home-console generation (PC, mobile) or that are unclassified.
type Platform struct {
	NamedRef
	Manufacturer string `json:"manufacturer"`
	Family       string `json:"family"`
	Generation   *int   `json:"generation"`
	Handheld     bool   `json:"handheld"`
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
	Platforms          []Platform `json:"platforms"`
	// Extras is the rich, display-only IGDB metadata, stored and served as an
	// opaque JSON document (see metadata.GameExtras for its shape). A null value
	// means it hasn't been fetched yet, which the detail handler uses to trigger
	// a lazy refresh.
	Extras json.RawMessage `json:"extras"`
}

// Book is the shared, Open Library-sourced work record: "The Hobbit", not any
// particular printing of it.
type Book struct {
	ID               string   `json:"id"` // Open Library work key, e.g. OL12345W
	Title            string   `json:"title"`
	Authors          []string `json:"authors"`
	Description      string   `json:"description"`
	CoverURL         string   `json:"cover_url"`
	AccentHex        string   `json:"accent_hex"`
	FirstPublishYear *int     `json:"first_publish_year"`
	Subjects         []string `json:"subjects"`
	// Editions is the printings cache for this work, loaded on a detail read.
	Editions []BookEdition `json:"editions,omitempty"`
}

// BookEdition is one printing of a work. Page maps for physical copies key
// off the edition, never the work.
type BookEdition struct {
	ID            string `json:"id"` // Open Library edition key, e.g. OL12345M
	BookID        string `json:"book_id"`
	ISBN10        string `json:"isbn10"`
	ISBN13        string `json:"isbn13"`
	Publisher     string `json:"publisher"`
	PublishedYear *int   `json:"published_year"`
	PageCount     *int   `json:"page_count"`
	Binding       string `json:"binding"`
	Language      string `json:"language"`
	CoverURL      string `json:"cover_url"`
}

// Entry is one item in one user's library. Media type says which subject is
// embedded: Game for 'game' entries, Book for 'book' ones. Exactly one of the
// two is set — the other is omitted from the payload rather than serialised
// as an empty object.
type Entry struct {
	ID            string     `json:"id"`
	MediaType     string     `json:"media_type"`
	Game          *Game      `json:"game,omitempty"`
	Book          *Book      `json:"book,omitempty"`
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

// BookStats summarises a user's book library for the books library strip.
// Deliberately lean: the reading dashboard (pages read, reading pace) is a
// later stage — this is only what the library page's header needs.
type BookStats struct {
	Total    int     `json:"total"`
	Backlog  int     `json:"backlog"`
	Reading  int     `json:"reading"`
	Read     int     `json:"read"`
	Dropped  int     `json:"dropped"`
	Ignored  int     `json:"ignored"`
	Wishlist int     `json:"wishlist"`
	// Completion mirrors the games definition: read / owned, with wishlist
	// and ignored excluded from the denominator.
	Completion float64 `json:"completion"`
}

// BookFacets carries the book library's filter rail: the authors, subjects
// and languages present on the user's shelves, plus the statuses actually in
// use. Only values that would match anything are returned.
type BookFacets struct {
	Authors   []string `json:"authors"`
	Subjects  []string `json:"subjects"`
	Languages []string `json:"languages"`
	Statuses  []string `json:"statuses"`
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

// Project kinds: what the target is measured against.
const (
	// ProjectChecklist is a curated member list; every member done = complete.
	ProjectChecklist = "checklist"
	// ProjectCountGoal is "finish N games"; the whole library counts.
	ProjectCountGoal = "count_goal"
	// ProjectRuleGoal is defined by a smart-list RuleSet; matching entries
	// count, and the target is either target_count or every match.
	ProjectRuleGoal = "rule_goal"
)

// AllProjectKinds lists every project kind, in display order.
var AllProjectKinds = []string{ProjectChecklist, ProjectCountGoal, ProjectRuleGoal}

// ValidProjectKind reports whether k is a known project kind.
func ValidProjectKind(k string) bool {
	for _, kind := range AllProjectKinds {
		if kind == k {
			return true
		}
	}
	return false
}

// ProjectProgress is the computed state of a project's target, never stored:
// how much of the target is done, in both games and estimated hours.
type ProjectProgress struct {
	// TargetCount is what "done" means: checklist members, count_goal
	// target_count, or rule_goal target_count (falling back to the full match
	// pool when no explicit target was set).
	TargetCount int `json:"target_count"`
	// CompletedCount is how many entries currently count as done. May exceed
	// TargetCount for count goals set below the library's played total.
	CompletedCount int     `json:"completed_count"`
	EstHoursTotal  float64 `json:"est_hours_total"`
	EstHoursDone   float64 `json:"est_hours_done"`
	// EstHoursRemaining is the estimated playtime left in the target set,
	// floored at zero.
	EstHoursRemaining float64 `json:"est_hours_remaining"`
	// Percent is CompletedCount / TargetCount, capped at 100.
	Percent float64 `json:"percent"`
}

// Project is a temporary objective. Lists answer "what exists"; projects
// answer "what am I trying to accomplish", and they end.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	TargetCount *int      `json:"target_count,omitempty"`
	Rules       *RuleSet  `json:"rules,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// CompletedAt is stamped when the target is met (automatically on the next
	// read) or manually when the user closes the project. Null = in progress.
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Progress    ProjectProgress `json:"progress"`
}

// ProjectItem is one member of a project view: an entry plus its manual
// done override. Done is null when completion is derived from the entry's
// status; it is only ever set for checklist members.
type ProjectItem struct {
	Entry Entry `json:"entry"`
	Done  *bool `json:"done"`
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

// TonightPick is one category's answer to "what should I play tonight?". The
// reason is built server-side so the API stays the source of truth for why a
// game was suggested.
type TonightPick struct {
	Entry  Entry   `json:"entry"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// TonightPicksResult is the four-category answer to a time budget. Any category
// is null when the library has nothing to offer it.
type TonightPicksResult struct {
	Continue *TonightPick `json:"continue"`
	ShortWin *TonightPick `json:"short_win"`
	Wildcard *TonightPick `json:"wildcard"`
	Rescue   *TonightPick `json:"rescue"`
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

// Kinds of superlative on the "Your Gaming Problem" dashboard.
const (
	SuperlativeOldestUntouched = "oldest_untouched"
	SuperlativeLongestUnplayed = "longest_unplayed"
	SuperlativeNeglectedGenre  = "neglected_genre"
	SuperlativeWorstPlatform   = "worst_platform"
	SuperlativeNeglectedYear   = "neglected_year"
)

// InsightsHeadline is the top row of the dashboard: the size of the problem.
type InsightsHeadline struct {
	GamesOwned     int     `json:"games_owned"`
	UnplayedGames  int     `json:"unplayed_games"`
	HoursRemaining float64 `json:"hours_remaining"`
	// YearsAtCurrentRate is how long the remaining hours take at the current
	// 90-day pace. Null when there is no pace to project from.
	YearsAtCurrentRate *float64 `json:"years_at_current_rate"`
}

// SuperlativePayload carries the numbers behind one superlative. Which fields
// are set depends on the kind: game-backed stats fill Game/EntryID/AddedOn/
// Hours, bucket stats (genre / platform / release year) fill the counts.
type SuperlativePayload struct {
	Game    *Game  `json:"game,omitempty"`
	EntryID string `json:"entry_id,omitempty"`
	// AddedOn is the date the game entered the library (YYYY-MM-DD).
	AddedOn string `json:"added_on,omitempty"`
	// Hours is the playtime figure the superlative turns on.
	Hours *float64 `json:"hours,omitempty"`

	// Name is the genre or platform name.
	Name string `json:"name,omitempty"`
	// Owned / Played count games for a bucket stat; a wishlist is not owned.
	Owned  int `json:"owned,omitempty"`
	Played int `json:"played,omitempty"`
	// BacklogGames / BacklogHours size a platform's unplayed debt.
	BacklogGames int     `json:"backlog_games,omitempty"`
	BacklogHours float64 `json:"backlog_hours,omitempty"`
	// Year is the release year the release-year stat is about.
	Year *int `json:"year,omitempty"`
}

// Superlative is one ridiculous stat. Label is pre-rendered so the copy lives
// in one place; the payload carries the raw numbers for clients that want them.
type Superlative struct {
	Kind    string             `json:"kind"`
	Payload SuperlativePayload `json:"payload"`
	Label   string             `json:"label"`
}

// Insights is the "Your Gaming Problem" dashboard payload.
type Insights struct {
	Headline     InsightsHeadline `json:"headline"`
	Superlatives []Superlative    `json:"superlatives"`
}

// Achievement tiers, in ascending order of prestige.
const (
	TierBronze    = "bronze"
	TierSilver    = "silver"
	TierGold      = "gold"
	TierLegendary = "legendary"
)

// AllTiers lists every achievement tier, in display order.
var AllTiers = []string{TierBronze, TierSilver, TierGold, TierLegendary}

// ValidTier reports whether t is a known achievement tier.
func ValidTier(t string) bool {
	for _, tier := range AllTiers {
		if tier == t {
			return true
		}
	}
	return false
}

// Achievement is one catalogue entry, defined in code (internal/achievements).
type Achievement struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Icon is a code key the client maps to an actual icon glyph.
	Icon string `json:"icon"`
	// Tier is the achievement's rarity band; one of the Tier constants.
	Tier string `json:"tier"`
	// Hidden keeps an achievement's identity masked while locked — the
	// reveal lives in the unlock toast and the gallery.
	Hidden bool `json:"hidden"`
	// Egg marks an easter egg: unlockable only by playing with the app
	// through the /egg endpoint, never by a predicate. Implies Hidden.
	Egg bool `json:"egg"`
}

// AchievementStatus is an achievement plus the user's unlock state: locked
// (UnlockedAt nil, no entry) or unlocked with the triggering game attached.
type AchievementStatus struct {
	Achievement
	UnlockedAt *time.Time `json:"unlocked_at,omitempty"`
	Entry      *Entry     `json:"entry,omitempty"`
}

// Season is the per-calendar-year "Backlog Challenge" rollup. It is derived
// on demand — no table — from finishes, sessions, series completion, and
// ownership ages.
type Season struct {
	Year              int     `json:"year"`
	GamesCompleted    int     `json:"games_completed"`
	HoursPlayed       float64 `json:"hours_played"`
	FranchisesCleared int     `json:"franchises_cleared"`
	// Rescues counts games finished after sitting owned for a year or more.
	Rescues int `json:"rescues"`
}

// MediaFile is one file the scanner found under a configured media root.
// The library lives on the NAS, mounted read-only — Backhog points at these
// files, it never owns them. Path is relative to Root, slash-separated.
type MediaFile struct {
	ID        int64  `json:"id"`
	Root      string `json:"root"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"size_bytes"`
	// Mtime is unix nanoseconds, kept exactly as the filesystem reported it so
	// (size, mtime) change detection has no format round-trips.
	Mtime int64 `json:"mtime"`
	// SHA256 is computed lazily by the attach flow, not by the scanner.
	SHA256 *string `json:"sha256,omitempty"`
	// DurationSeconds is audio-only and NULL when the tags did not yield it.
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	// ContainerMetadata is the embedded ID3/MP4 tag set as a JSON object
	// (see internal/media), or NULL when the file carries none.
	ContainerMetadata json.RawMessage `json:"container_metadata,omitempty"`
	// BookID is the attached book. Plain TEXT with no FK by design: the books
	// table arrives in migration 00011 and the FK is added once it is
	// guaranteed present. NULL means not attached yet.
	BookID    *string   `json:"book_id,omitempty"`
	ScannedAt time.Time `json:"scanned_at"`
	// MissingAt is set when the path disappeared from its root; the row is
	// kept so the BookID association survives a temporarily-unmounted NAS.
	MissingAt *time.Time `json:"missing_at,omitempty"`
}

// EpubText is the canonical-text index row for one parsed EPUB media file.
// The canonical text itself is a companion file ({EPUB_TEXT_DIR}/{id}.txt),
// not a column: novels are multi-megabyte strings and only the pointer and
// its facts live here. All offsets in the Books arena are byte offsets into
// that text, and ParserVersion pins the normalizer that produced it.
type EpubText struct {
	ID               string    `json:"id"`
	MediaFileID      int64     `json:"media_file_id"`
	CharCount        int       `json:"char_count"`
	WordCount        int       `json:"word_count"`
	NormalizedSHA256 string    `json:"normalized_sha256"`
	ParsedAt         time.Time `json:"parsed_at"`
	ParserVersion    string    `json:"parser_version"`
}

// EpubChapter is one spine document of a canonical text, in reading order.
// [CharStart, CharEnd) partitions [0, CharCount) with no gaps or overlaps;
// empty (image-only) documents have CharStart == CharEnd.
type EpubChapter struct {
	ID         string `json:"id"`
	EpubTextID string `json:"epub_text_id"`
	SpineIndex int    `json:"spine_index"`
	Href       string `json:"href"`
	Title      string `json:"title"`
	CharStart  int    `json:"char_start"`
	CharEnd    int    `json:"char_end"`
	// Depth is the TOC nesting of the entry targeting this document.
	Depth int `json:"depth"`
}
