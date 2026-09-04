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
// m4a, m4b and opus; epub is text. Anything else (.aax, .aaxc, .mobi,
// DRM-wrapped epub, ...) is not inventoried — it is recorded in media_skipped
// with a reason instead.
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
	Total    int `json:"total"`
	Backlog  int `json:"backlog"`
	Reading  int `json:"reading"`
	Read     int `json:"read"`
	Dropped  int `json:"dropped"`
	Ignored  int `json:"ignored"`
	Wishlist int `json:"wishlist"`
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
	// MediaScope is the arena this project lives in ('game' or 'book') — it
	// decides which half of the library feeds the project's progress.
	MediaScope  string    `json:"media_scope"`
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

// Kinds of superlative on the "Your Reading Problem" dashboard. They mirror
// the gaming ones without pretending to be the same stat: a book has an
// author and a subject where a game has a genre and a platform, and a book
// is the only one of the two you can start over from page one three times.
const (
	BookSuperlativeOldestUnopened   = "oldest_unopened"
	BookSuperlativeLongestUnread    = "longest_unread"
	BookSuperlativeUnreadAuthor     = "unread_author"
	BookSuperlativeNeglectedSubject = "neglected_subject"
	BookSuperlativeRestarted        = "restarted"
)

// ReadingPace is how fast you actually get through a book, measured from
// reading_sessions rather than assumed. PagesPerHour is always populated so
// the "years at your pace" number is never magic; Measured says whether it
// came from your own sessions or from the fallback default.
type ReadingPace struct {
	PagesPerHour float64 `json:"pages_per_hour"`
	// CharsPerHour is the raw measurement PagesPerHour is derived from,
	// exposed so the page conversion is auditable rather than hidden.
	CharsPerHour float64 `json:"chars_per_hour"`
	// CharsPerPage is the constant that converts one into the other.
	CharsPerPage int  `json:"chars_per_page"`
	Measured     bool `json:"measured"`
	// SessionHours is how much instrumented reading the measurement rests on;
	// below the minimum it stays 0 and Measured is false.
	SessionHours float64 `json:"session_hours"`
	// HoursPerWeek90d / HoursPerWeekAll mirror Pace: how much time you
	// actually spend reading, from the same sessions. Null means no data.
	HoursPerWeek90d *float64 `json:"hours_per_week_90d"`
	HoursPerWeekAll *float64 `json:"hours_per_week_all"`
}

// ReadingDebt is the books counterpart of DebtReport: how much unread book
// you own, in pages and in hours, and when it clears at your real pace.
type ReadingDebt struct {
	BooksOwned  int `json:"books_owned"`
	UnreadBooks int `json:"unread_books"`
	// PagesOwed counts every page you have not read yet: the whole book for
	// something in the backlog, the unread remainder for something started.
	PagesOwed float64 `json:"pages_owed"`
	// HoursOwed is PageHours + AudioHours.
	HoursOwed float64 `json:"hours_owed"`
	// PageHours is the part estimated from page counts and your reading pace;
	// AudioHours is the part measured from real attached audiobook durations,
	// which is the honest number wherever a book has audio files.
	PageHours  float64 `json:"page_hours"`
	AudioHours float64 `json:"audio_hours"`
	// AudioBooks is how many unread books were sized from audio rather than
	// pages; UnsizedBooks is how many contribute nothing because neither a
	// page count nor an audiobook is known.
	AudioBooks   int `json:"audio_books"`
	UnsizedBooks int `json:"unsized_books"`
	// ShortBooksHours is the quick-wins slice: unread books under
	// shortBookPages long.
	ShortBooksHours float64        `json:"short_books_hours"`
	Pace            ReadingPace    `json:"pace"`
	Projection      DebtProjection `json:"projection"`
}

// ReadingHeadline is the top row of the reading dashboard: the size of the
// problem, in the two units a book is measured in.
type ReadingHeadline struct {
	BooksOwned  int     `json:"books_owned"`
	UnreadBooks int     `json:"unread_books"`
	PagesOwed   float64 `json:"pages_owed"`
	HoursOwed   float64 `json:"hours_owed"`
	// YearsAtCurrentRate is how long HoursOwed takes at your trailing 90-day
	// reading rate. Null when you have not logged enough to have one.
	YearsAtCurrentRate *float64 `json:"years_at_current_rate"`
}

// BookSuperlativePayload carries the numbers behind one book superlative.
// Which fields are set depends on the kind: book-backed stats fill
// Book/EntryID/AddedOn/Pages/Hours, bucket stats (author, subject) fill the
// counts.
type BookSuperlativePayload struct {
	Book    *Book  `json:"book,omitempty"`
	EntryID string `json:"entry_id,omitempty"`
	// AddedOn is the date the book entered the library (YYYY-MM-DD).
	AddedOn string `json:"added_on,omitempty"`
	// Pages / Hours size the book the superlative turns on.
	Pages *int     `json:"pages,omitempty"`
	Hours *float64 `json:"hours,omitempty"`

	// Name is the author or subject a bucket stat is about.
	Name string `json:"name,omitempty"`
	// Owned / Read count books for a bucket stat; a wishlist is not owned.
	Owned int `json:"owned,omitempty"`
	Read  int `json:"read,omitempty"`
	// Starts is how many separate times a book was picked up again.
	Starts int `json:"starts,omitempty"`
}

// BookSuperlative is one uncomfortable reading stat, the mirror of
// Superlative. Label is pre-rendered so the copy lives in one place.
type BookSuperlative struct {
	Kind    string                 `json:"kind"`
	Payload BookSuperlativePayload `json:"payload"`
	Label   string                 `json:"label"`
}

// ReadingInsights is the "Your Reading Problem" dashboard payload. Pace is
// hoisted out of the debt report and shown explicitly, so the projected years
// are legible rather than magic.
type ReadingInsights struct {
	Headline     ReadingHeadline   `json:"headline"`
	Pace         ReadingPace       `json:"pace"`
	Superlatives []BookSuperlative `json:"superlatives"`
}

// ReadingPick is one category's answer to "what should I read?", the books
// mirror of TonightPick. The reason is built server-side so the API stays the
// source of truth for why a book was suggested.
type ReadingPick struct {
	Entry  Entry   `json:"entry"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// ReadingPicksResult is the four-category answer to a time budget. Any
// category is null when the shelf has nothing to offer it.
type ReadingPicksResult struct {
	Continue *ReadingPick `json:"continue"`
	ShortWin *ReadingPick `json:"short_win"`
	Wildcard *ReadingPick `json:"wildcard"`
	Rescue   *ReadingPick `json:"rescue"`
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

// Achievement domains: which arena an achievement belongs to. 'game' and
// 'book' scope a definition to one library; 'any' marks achievements that
// are about the app itself (the eggs) and can unlock from either.
const (
	DomainGame = "game"
	DomainBook = "book"
	DomainAny  = "any"
)

// ValidDomain reports whether d is a known achievement domain.
func ValidDomain(d string) bool {
	switch d {
	case DomainGame, DomainBook, DomainAny:
		return true
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
	// Domain is the arena the achievement belongs to; one of the Domain
	// constants. Predicates only ever fire against their own domain's
	// events — a book finish never moves a game ladder, or vice versa.
	Domain string `json:"domain"`
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

// ReadingSeason is the books arena's per-calendar-year rollup — the mirror
// of Season, derived on demand the same way: books finished, pages read,
// hours listened, authors fully cleared, and rescues of long-owned books.
type ReadingSeason struct {
	Year          int     `json:"year"`
	BooksFinished int     `json:"books_finished"`
	PagesRead     int     `json:"pages_read"`
	HoursListened float64 `json:"hours_listened"`
	// AuthorsCleared counts authors whose owned books were all finished,
	// the year the last one closed.
	AuthorsCleared int `json:"authors_cleared"`
	// Rescues counts books finished after sitting owned for a year or more.
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
	// MetaVersion is the version of the metadata extractor that produced
	// ContainerMetadata. The scanner's (size, mtime) fast path also requires
	// this to match, so bumping the extractor re-reads every file's metadata
	// exactly once instead of leaving old rows behind forever.
	// Not served: the readers that build a MediaFile do not select it, and
	// it means nothing to a client.
	MetaVersion int `json:"-"`
	// BookID is the attached book. Plain TEXT with no FK by design: the books
	// table arrives in migration 00011 and the FK is added once it is
	// guaranteed present. NULL means not attached yet.
	BookID    *string   `json:"book_id,omitempty"`
	ScannedAt time.Time `json:"scanned_at"`
	// MissingAt is set when the path disappeared from its root; the row is
	// kept so the BookID association survives a temporarily-unmounted NAS.
	MissingAt *time.Time `json:"missing_at,omitempty"`
}

// Reasons a file was skipped by the scanner. DRM formats are refused, not
// worked around — this tool is DRM-free by decision. The other two reasons
// exist so that "not inventoried" stops meaning "we have no idea what this
// is": a Kindle file is a format we chose not to parse, and an .opf is not a
// book at all.
const (
	MediaSkipUnsupported = "unsupported_extension"
	MediaSkipDRM         = "drm_epub"
	// MediaSkipFormatUnhandled is a recognised ebook format this tool does
	// not parse (.mobi, .azw, .azw3). Not a DRM refusal and not an unknown
	// extension: reading Kindle formats means a PalmDOC/HUFF-CDIC/KF8 parser,
	// which belongs in its own library, not bolted into the scanner.
	MediaSkipFormatUnhandled = "format_unhandled"
	// MediaSkipSidecar is a metadata sidecar (.opf) — the answer key next to
	// the books rather than a missing one. It is parsed into media_sidecars
	// and fed to the matcher; the skip row exists only so the file is
	// accounted for rather than silently vanishing.
	MediaSkipSidecar = "sidecar_metadata"
)

// MediaSkipped is one file the scanner refused to inventory: an unsupported
// extension (.aax, cover art, ...) or a DRM-wrapped EPUB. Kept in its own
// table so the attach UI can show *why* half a library is missing instead of
// letting the user assume the scan is broken. Rows are replaced per root on
// each scan.
type MediaSkipped struct {
	ID        int64  `json:"id"`
	Root      string `json:"root"`
	Path      string `json:"path"`
	Ext       string `json:"ext"`
	Reason    string `json:"reason"`
	SizeBytes int64  `json:"size_bytes"`
	// Mtime is unix nanoseconds, as reported by the filesystem.
	Mtime  int64     `json:"mtime"`
	SeenAt time.Time `json:"seen_at"`
}

// MediaSidecar is the metadata block of one .opf file found next to a
// library's books — a Calibre "metadata.opf", or a "Title - Author.opf"
// beside a single file. It is not a book and never gets a media_files row;
// it is authoritative evidence about the directory it sits in, which is why
// the matcher prefers it over embedded tags, directory layout and filenames.
//
// Rows are replaced per root on each scan, exactly like MediaSkipped: the
// sidecar is a property of what is on disk right now, never user state.
type MediaSidecar struct {
	ID   int64  `json:"id"`
	Root string `json:"root"`
	Path string `json:"path"`
	// Title and Author are the dc:title / dc:creator the file declares.
	Title  string `json:"title"`
	Author string `json:"author"`
	// Series and SeriesIndex come from Calibre's meta pair when present.
	Series      string `json:"series,omitempty"`
	SeriesIndex string `json:"series_index,omitempty"`
	Language    string `json:"language,omitempty"`
	// ISBN is normalized and validated at parse time, so a non-empty value
	// is safe to hand straight to an ISBN lookup.
	ISBN string `json:"isbn,omitempty"`
	// WorkKey is an Open Library work key (OL12345W) when the sidecar
	// carries one — an exact identity, not a search term.
	WorkKey string    `json:"work_key,omitempty"`
	SeenAt  time.Time `json:"seen_at"`
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

// Sources for a stored reading position: what produced the canonical
// character offset. A later alignment uses this to tell an offset it can
// trust exactly (the reader knew where it was) from one that was
// back-computed or fuzzy-matched.
const (
	// PositionSourceRead is the EPUB reader: the offset is exact.
	PositionSourceRead = "read"
	// PositionSourceListen is the audiobook player: the offset was derived
	// from a timestamp through the alignment map.
	PositionSourceListen = "listen"
	// PositionSourceScan is a camera OCR page scan matched into the text.
	PositionSourceScan = "scan"
	// PositionSourceManual is the user typing a page or dragging a slider.
	PositionSourceManual = "manual"
)

// ValidPositionSource reports whether s is a tracked position source.
func ValidPositionSource(s string) bool {
	switch s {
	case PositionSourceRead, PositionSourceListen, PositionSourceScan, PositionSourceManual:
		return true
	}
	return false
}

// Modes a book is consumed in. An hour read and an hour listened are both an
// hour owed, but the dashboard wants to tell them apart.
const (
	ReadingModeRead   = "read"
	ReadingModeListen = "listen"
)

// ValidReadingMode reports whether s is a tracked consumption mode.
func ValidReadingMode(s string) bool {
	switch s {
	case ReadingModeRead, ReadingModeListen:
		return true
	}
	return false
}

// BookProgress is a library entry's stored reading position. CharOffset is
// the only truth: the audio timestamp and printed page in an API response are
// derived from it on read, so they cannot drift apart.
//
// RawAudioSeconds/RawAudioFileID are the fallback for a book with no
// alignment yet — a listening position that genuinely cannot be expressed as
// a character offset. They are track-relative (seconds within that file), not
// global, so re-measuring or re-ordering the timeline cannot move them.
type BookProgress struct {
	EntryID          string   `json:"entry_id"`
	CharOffset       int      `json:"char_offset"`
	CharOffsetSource string   `json:"char_offset_source"`
	RawAudioSeconds  *float64 `json:"raw_audio_seconds,omitempty"`
	RawAudioFileID   *int64   `json:"raw_audio_file_id,omitempty"`
	// PercentComplete is 0–100 against the canonical text's length, or
	// against the audiobook's total duration for a book with no EPUB.
	PercentComplete float64   `json:"percent_complete"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ReadingSession is one stretch of reading or listening, the books-arena
// mirror of Session. Unlike a play session — typed in after the fact against
// a whole day — it is instrumented, so it carries real endpoints and how far
// the position actually moved.
type ReadingSession struct {
	ID            string    `json:"id"`
	EntryID       string    `json:"entry_id"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Mode          string    `json:"mode"`
	CharsAdvanced int       `json:"chars_advanced"`
	Seconds       int       `json:"seconds"`
	CreatedAt     time.Time `json:"created_at"`
}

// Sources for a recorded page anchor: how the (page, char offset) pair
// was produced. An OCR scan matched through the passage matcher carries
// the matcher's confidence; a manual anchor is a reader saying "page 40
// starts here", which is exact by declaration.
const (
	// PageAnchorSourceOCR is a camera scan matched into the text.
	PageAnchorSourceOCR = "ocr"
	// PageAnchorSourceManual is a reader-typed page position.
	PageAnchorSourceManual = "manual"
)

// ValidPageAnchorSource reports whether s is a tracked anchor source.
func ValidPageAnchorSource(s string) bool {
	switch s {
	case PageAnchorSourceOCR, PageAnchorSourceManual:
		return true
	}
	return false
}

const (
	// CopyAcquisitionOwned is a printing the user bought or otherwise
	// keeps: nothing about holding it expires.
	CopyAcquisitionOwned = "owned"
	// CopyAcquisitionBorrowed is a library copy: held, not owned, with
	// an optional return deadline. Format is how the book was consumed,
	// not whether it's owned — a borrowed copy still counts as paper.
	CopyAcquisitionBorrowed = "borrowed"
)

// ValidCopyAcquisition reports whether s is a tracked acquisition kind.
func ValidCopyAcquisition(s string) bool {
	switch s {
	case CopyAcquisitionOwned, CopyAcquisitionBorrowed:
		return true
	}
	return false
}

// PhysicalCopy is one printing of a book the user actually holds, owned
// or borrowed from the library: the thing a camera scan reads off of.
// Page numbers belong to the printing, so anchors hang off the copy and
// never off the work — a user with two printings of the same book has
// two copies with two independent page maps.
type PhysicalCopy struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	EntryID   string `json:"entry_id"`
	EditionID string `json:"edition_id"`
	Notes     string `json:"notes"`
	// Acquisition is how the printing was acquired. A returned borrowed
	// copy is still a row — the page map survives the return — and
	// ReturnedAt is what says whether it is in hand.
	Acquisition string `json:"acquisition"`
	// DueAt is the library return deadline, when known. Display-only.
	DueAt *time.Time `json:"due_at"`
	// ReturnedAt is nil while the copy is in hand. Return stamps it;
	// a re-checkout of the same printing clears it and sets a new DueAt.
	ReturnedAt *time.Time `json:"returned_at"`
	// AnchorCount is computed on list, so the copy UI can say how much of
	// a page map exists without fetching it.
	AnchorCount int `json:"anchor_count"`
	// DrivesPages reports whether this is the copy the position endpoints
	// read: only the printing the entry itself is anchored to feeds them.
	// A reader who holds two printings has two maps and one of them is
	// what "page 214" means, and the UI has to be able to say which.
	DrivesPages bool      `json:"drives_pages"`
	CreatedAt   time.Time `json:"created_at"`
}

// PageAnchor ties one printed page of one physical copy to the canonical
// character offset where that page's text begins. Re-scanning a page
// overwrites the anchor (last write wins) instead of stacking a
// conflicting one, so a bad scan corrects rather than accumulates.
type PageAnchor struct {
	PhysicalCopyID string    `json:"-"`
	PrintedPage    int       `json:"printed_page"`
	CharOffset     int       `json:"char_offset"`
	Source         string    `json:"source"`
	Confidence     float64   `json:"confidence"`
	CreatedAt      time.Time `json:"created_at"`
}

// PageMapSeed is the coarse page map a printing has before anybody has
// scanned anything: the printing's own page count against the length of
// the canonical text. It is not a measurement — nobody has looked at the
// paper — but "this printing has 412 pages and the text is 640k
// characters" already places every offset to within a chapter, which is
// enough to be useful on day one and improves with every real anchor
// scanned over the top of it.
//
// It exists only for a printing the reader actually registered a copy
// of: page numbers of a printing nobody owns are page numbers of
// nothing, and inventing them would put a page readout on every book in
// the library.
type PageMapSeed struct {
	PageCount int
	CharCount int
}

// Usable reports whether the seed has both halves it needs to span a
// book. A printing whose page count Open Library never recorded, or a
// book whose EPUB has not been parsed, has no seed and simply waits for
// real anchors.
func (s PageMapSeed) Usable() bool { return s.PageCount > 1 && s.CharCount > 0 }

// Alignment job states. The first four are the worker pipeline's live
// positions; the last three are terminal. 'low_confidence' is a *usable*
// alignment whose anchors should be treated skeptically — it exists so a
// marginal book can still hand off between reading and listening instead
// of failing outright.
const (
	AlignmentQueued        = "queued"
	AlignmentClaimed       = "claimed"
	AlignmentTranscribing  = "transcribing"
	AlignmentAligning      = "aligning"
	AlignmentReady         = "ready"
	AlignmentFailed        = "failed"
	AlignmentLowConfidence = "low_confidence"
)

// AlignmentJobActive reports whether state is one the worker is still
// working through (or is about to): the states covered by the one-job-
// per-entry queue invariant.
func AlignmentJobActive(state string) bool {
	switch state {
	case AlignmentQueued, AlignmentClaimed, AlignmentTranscribing, AlignmentAligning:
		return true
	}
	return false
}

// AlignmentJobTerminal reports whether state is one the job can never
// leave: it finished, one way or another.
func AlignmentJobTerminal(state string) bool {
	switch state {
	case AlignmentReady, AlignmentFailed, AlignmentLowConfidence:
		return true
	}
	return false
}

// AlignmentJob is one requested alignment on the queue. It is the unit
// the internal worker API claims, heartbeats and completes; HeartbeatAt
// is the liveness signal a stale claim is judged by.
type AlignmentJob struct {
	ID         string `json:"id"`
	EntryID    string `json:"entry_id"`
	EpubTextID string `json:"epub_text_id"`
	// AudioTimelineHash pins the ordered (file, duration) sequence the
	// job was enqueued against, so a re-attached audiobook is detectable
	// against the alignment it produced.
	AudioTimelineHash string     `json:"audio_timeline_hash"`
	State             string     `json:"state"`
	Progress          float64    `json:"progress"`
	StageDetail       string     `json:"stage_detail"`
	Error             *string    `json:"error,omitempty"`
	Attempts          int        `json:"attempts"`
	ClaimedBy         *string    `json:"claimed_by,omitempty"`
	ClaimedAt         *time.Time `json:"claimed_at,omitempty"`
	HeartbeatAt       *time.Time `json:"heartbeat_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Alignment is a finished (or streamed-so-far) alignment: the summary row
// whose anchors the position translator interpolates over. While a worker
// streams batches it shares the claiming job's id, with its final state,
// coverage and confidence set by the complete call.
type Alignment struct {
	ID             string    `json:"id"`
	EntryID        string    `json:"entry_id"`
	EpubTextID     string    `json:"epub_text_id"`
	State          string    `json:"state"`
	Coverage       float64   `json:"coverage"`
	MeanConfidence float64   `json:"mean_confidence"`
	Model          string    `json:"model"`
	CreatedAt      time.Time `json:"created_at"`
}

// AlignmentAnchor ties one canonical character offset to one point on
// the audiobook's GLOBAL timeline — the same single notion of "where in
// the tape" the player and stored positions use, never a per-track
// offset. Confidence is the aligner's own belief in the pair, in [0,1].
type AlignmentAnchor struct {
	AlignmentID  string  `json:"-"`
	CharOffset   int     `json:"char_offset"`
	AudioSeconds float64 `json:"audio_seconds"`
	Confidence   float64 `json:"confidence"`
}

// TranscriptSegment is one stretch of raw transcription output, kept for
// debugging a bad alignment. Never read on a hot path.
type TranscriptSegment struct {
	AlignmentID string  `json:"-"`
	AudioStart  float64 `json:"audio_start"`
	AudioEnd    float64 `json:"audio_end"`
	Text        string  `json:"text"`
}
