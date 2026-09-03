// Package achievements holds the achievement catalogue: the code-defined list
// of things worth celebrating about backlog progress. The guiding principle is
// to gamify making progress through the backlog — finishing, rescuing, and
// clearing — never playing games themselves. No points for hours logged.
//
// The catalogue spans both arenas: game definitions and book definitions live
// side by side, each tagged with its domain, and a definition only ever
// evaluates against its own domain's events. Both domains hang off the same
// spine — library_entries and the status history — so a book finish feeds the
// same shape of predicate a game finish does, with the aggregates assembled
// per domain by the store.
//
// Predicates are pure functions over an Event snapshot so they can be tested
// without a database; the store assembles the snapshot inside its transaction
// and inserts unlocks idempotently.
package achievements

import (
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Event kinds: what store mutation triggered the evaluation.
const (
	// EventFinished fires when an entry's status transitions to played.
	EventFinished = "finished"
	// EventDropped fires when an entry's status transitions to dropped.
	EventDropped = "dropped"
	// EventSession fires when playtime is logged. It re-evaluates a finished
	// or dropped game, because the final session is often logged after the
	// status flip (completionist, speedrun).
	EventSession = "session"
	// EventResumed fires when a dropped game comes back: a transition from
	// dropped to playing or backlog. Unlocks the comeback predicates.
	EventResumed = "resumed"
)

// Thresholds the predicates turn on.
const (
	// cleanupCrewCount is how many finished games earn Cleanup Crew.
	cleanupCrewCount = 5
	// The volume ladder: finishes at large round numbers.
	springCleaningCount = 10
	deepCleanCount      = 25
	hazmatSuitCount     = 50
	thePurgeCount       = 100
	// oneDownUnplayed is how many games must still be unplayed when a
	// finish lands for One Down.
	oneDownUnplayed = 10
	// The backlog-reduction ladder: how far the unplayed count must sit
	// below its all-time peak.
	makingADentReduction      = 10
	dentistAppointmentDrop    = 25
	massExtinctionReduction   = 50
	emptyTheClosetPeakUnowned = 10
	// The ownership-age ladder: how long a game must have been owned at
	// finish. Archaeologist (5 years) is the silver rung of it.
	dustyRelicWindow       = 3 * 365 * 24 * time.Hour
	archaeologistWindow    = 5 * 365 * 24 * time.Hour
	lostCivilizationWindow = 7 * 365 * 24 * time.Hour
	ancientArtifactWindow  = 10 * 365 * 24 * time.Hour
	// timeCapsuleWindow is how much older than your finish a game's
	// original release must be.
	timeCapsuleWindow = 10 * 365 * 24 * time.Hour
	// oldHardwareBeforeYear: Old Hardware counts games originally released
	// before this calendar year.
	oldHardwareBeforeYear = 2000
	// fossilRecordOldest is how many of the oldest owned games count for
	// The Fossil Record.
	fossilRecordOldest = 3
	// The acquisition-speed windows: how soon after adding a game it must
	// be finished.
	instantGratificationWindow = 30 * 24 * time.Hour
	noShelfTimeWindow          = 7 * 24 * time.Hour
	// abandonmentWindow is how long a dropped game must have been owned.
	// Matches the season's rescue threshold (owned ≥ 1 year).
	abandonmentWindow = 365 * 24 * time.Hour
	// The drop celebrations: the logged effort that makes a fold honest,
	// the share of the main estimate that makes it cutting losses, and the
	// drop-count ladder.
	knowWhenToFoldMinutes = 5 * 60
	cutYourLossesDivisor  = 10
	buyersRemorseWindow   = 7 * 24 * time.Hour
	wasntYouDropCount     = 5
	theReaperDropCount    = 10
	// The comeback windows: how long a game must stay dropped before the
	// return counts. Half a 365-day year for six months, matching the
	// whole-year windows around it.
	secondChanceWindow   = 182 * 24 * time.Hour
	againstAllOddsWindow = 365 * 24 * time.Hour
	phoenixWindow        = 2 * 365 * 24 * time.Hour
	// speedrunMinutes caps logged playtime at completion for Speedrun.
	speedrunMinutes = 5 * 60
	// LongHaulSeconds is the time-to-beat that qualifies a game as Long
	// Haul and counts a finish toward The Commitment. Exported because
	// the store's snapshot counts the same ladder in SQL.
	LongHaulSeconds = 50 * 3600
	// ultraMarathonSeconds is the time-to-beat that qualifies a game as
	// Ultra Marathon.
	ultraMarathonSeconds = 100 * 3600
	// theCommitmentGames is how many 50h+ finishes The Commitment demands.
	theCommitmentGames = 3
	// The calendar ladder: finishes bucketed by calendar month, the
	// consecutive-month streak, the full-year sweep, and the summer window.
	hatTrickMonth        = 3
	cleanupMonthCount    = 5
	backlogMachineStreak = 3
	perfectSeasonMonths  = 12
	summerCleanupCount   = 5
	// The no-acquisition windows: how long the user must go without
	// adding a non-wishlist game.
	restraintWindow  = 30 * 24 * time.Hour
	disciplineWindow = 90 * 24 * time.Hour
	// The series ladder: finishes from one series, how many owned
	// entries make a series a saga, same-year finishes for a marathon,
	// and the smallest series worth calling a completed set.
	trilogyGames        = 3
	franchiseModeGames  = 5
	sagaEntries         = 5
	marathonSeriesGames = 3
	fullSetMinOwned     = 2
	// The diversity ladder: distinct genres in one calendar year and
	// distinct platforms finished on.
	samplerGenres      = 5
	worldTourPlatforms = 5
	// RetroactiveYears is how long a platform must sit without a logged
	// session for a finish on it to count as coming out of retirement.
	// Exported because the store's snapshot evaluates the lookback
	// against session dates, in SQL and in the backfill replay.
	RetroactiveYears = 5
	// The platform-mastery sets: how many console generations, Nintendo
	// consoles, and handheld generations the finishes must span, and the
	// sizes of the curated hardware sets. Cross-checked against the
	// platform catalog by the tests.
	generationGapGenerations     = 5
	nintendoTimeMachineConsoles  = 5
	bigNConsoles                 = 7
	gameBoySystems               = 3
	handheldHistorianGenerations = 4
	pilgrimConsoles              = 5
	xboxGenerations              = 4
	// The book ladder: finishes at the same large round numbers the games
	// ladder uses.
	firstEditionCount  = 1
	shelfImprovement   = 5
	wellReadCount      = 10
	libraryCardCount   = 25
	branchLibraryCount = 50
	theLibrarianCount  = 100
	// lateFineWindow is how long a book must have been owned at finish —
	// the single rung the books arena asks of the ownership-age ladder.
	lateFineWindow = 5 * 365 * 24 * time.Hour
	// thirdTimeCharmAbandons is how many earlier start-and-abandon cycles a
	// book needs before finally finishing it counts as charming.
	thirdTimeCharmAbandons = 2
	// everyWhichWayFormats is the full set of formats one book must be
	// finished in: paper, ebook, and audio.
	everyWhichWayFormats = 3
	// The unread-pile ladder: how far the unread count must sit below its
	// all-time peak. Mirror of the games dent ladder.
	tbrTrimReduction  = 10
	shelfControlDrop  = 25
	sparkJoyReduction = 50
	// doorstopPages is the length a finished book must clear.
	doorstopPages = 600
	// honestDNFWindow is how long a book must have sat 'reading' before
	// dropping it counts as honest instead of overdue.
	honestDNFWindow = 2 * 365 * 24 * time.Hour
	// cartographerPages is how many pages of one physical copy a user must
	// map by scanning before the map counts as drawn.
	cartographerPages = 25
)

// Entry is the snapshot of the triggering library entry, plus the user-level
// aggregates the predicates need, all read inside the store's transaction.
// Every aggregate is scoped to the snapshot's own domain — PlayedCount counts
// games on a game snapshot and books on a book one — so a definition reads
// the same fields whichever arena fired the event.
type Entry struct {
	ID     string
	Status string // status as of the event
	// MediaType is the domain this snapshot belongs to: models.MediaGame or
	// models.MediaBook. It is what routes the event to the matching half
	// of the catalogue.
	MediaType string
	CreatedAt time.Time
	// At is the moment the predicate evaluates against: the finish or drop
	// timestamp when there is one, otherwise now.
	At                 time.Time
	LoggedMinutes      int    // total logged playtime at evaluation time
	TimeToBeatMain     *int64 // seconds, nil when unknown
	TimeToBeatComplete *int64 // seconds, nil when unknown
	// PlayedCount is how many entries the user has finished including this
	// one — games for a game snapshot, books for a book one.
	PlayedCount int
	// StartedAt is when the entry most recently entered 'playing'; nil when
	// never started. The books arena's honest-DNF predicate measures the
	// "in progress" stretch against it.
	StartedAt *time.Time
	// PageCount is the book's effective length in printed pages — derived
	// from the canonical text when one is attached, otherwise the
	// printing's own count. 0 when unknown. Book-domain only.
	PageCount int
	// FormatCount is how many of paper, ebook, and audio the user holds the
	// finished work in. Book-domain only.
	FormatCount int
	// IsOldestOwned reports whether this entry is the oldest game the user
	// owns and still means to finish (wishlist and ignored excluded, so an
	// endless game doesn't block the achievement forever).
	IsOldestOwned bool

	// DroppedAt is when the entry was last dropped before this event, from
	// the status history. Nil when it has never been dropped, or when the
	// drop predates the history table and finished_at has since been wiped.
	DroppedAt *time.Time
	// DroppedCount is how many games the user has ever dropped: entries
	// currently dropped, plus drop events in the history that were followed
	// by a resume. Pre-feature drops only count while still dropped.
	DroppedCount int
	// UnplayedCount is how many owned games are still unplayed
	// (backlog + playing) at evaluation time.
	UnplayedCount int
	// PeakUnplayedCount is the highest the unplayed count has ever been
	// up to At — the all-time peak a reduction is measured against.
	PeakUnplayedCount int
	// YearFinishes is how many games were finished in At's calendar year,
	// including this one.
	YearFinishes int
	// YearAdditions is how many non-wishlist games were added in At's
	// calendar year, up to At.
	YearAdditions int
	// CreatedAtRank is the entry's position by created_at among the owned,
	// finishable entries (oldest = 1). Generalizes IsOldestOwned to "oldest
	// N" predicates; ties share a rank.
	CreatedAtRank int
	// SeriesIDs lists the series (franchise or collection) the game
	// belongs to as a full game. DLC and expansion memberships are
	// excluded, so they never count toward the series predicates.
	SeriesIDs []string
	// SeriesStandings rolls up the user's standing in each of those
	// series at evaluation time — owned members, finishes, and what
	// remains — read inside the store's transaction.
	SeriesStandings map[string]SeriesStanding
	// PrevFinishSharesSeries reports whether the user's previous finish
	// — the closest one before this by (finish stamp, id), the same
	// order the backfill replays in — was a game from one of the same
	// series. Back to Back turns on it.
	PrevFinishSharesSeries bool
	// PlatformID is the platform the user chose to play this entry on; nil
	// when unset.
	PlatformID *int64
	// FirstReleaseDate is the game's original release date (unix seconds);
	// nil when IGDB doesn't know it.
	FirstReleaseDate *int64
	// FinishYear / FinishMonth identify the calendar month of At — the month
	// a month-scoped predicate (hat trick, season opener) buckets the finish
	// into. Zero when At is the zero time.
	FinishYear  int
	FinishMonth int
	// MonthFinishes is how many games were finished in At's calendar
	// month, including this one.
	MonthFinishes int
	// FinishStreak is the run of consecutive calendar months with at
	// least one finish, ending with At's month.
	FinishStreak int
	// YearMonthsFinished is how many distinct months of At's calendar
	// year have seen a finish, At's own included.
	YearMonthsFinished int
	// SummerFinishes is how many games were finished in the June–August
	// stretch of At's year, including this one; only meaningful when the
	// finish itself lands in summer.
	SummerFinishes int
	// LongHaulFinishes is how many 50h+ games the user has finished,
	// including this one.
	LongHaulFinishes int
	// WasQueueTop reports whether the entry sat at the top of the play
	// queue at the moment it was finished — captured before the
	// finishing update clears its position. Live finishes only: the
	// queue's historical order is not replayable, so the backfill never
	// sets it.
	WasQueueTop bool
	// DropHistory lists the entry's drop arcs from its status history,
	// oldest first. ResumedAt is nil while the drop is still open (or was
	// closed by finishing the game directly, which no resume row records).
	// Drops that predate the history table appear only through the
	// resume-time fallback the store passes in. Comeback predicates read it.
	DropHistory []DropCycle

	// YearGenres is how many distinct genres the user's finishes in At's
	// calendar year cover, this finish included — Sampler's variety count.
	YearGenres int
	// DistinctPlatforms is how many distinct platforms the user has
	// finished games on, this finish included. The "where you actually
	// played it" signal: entry.platform_id.
	DistinctPlatforms int
	// PlatformDormant reports whether the entry's platform has seen no
	// logged session in the RetroactiveYears up to and including the
	// finish. Nil when no platform is set, in which case Retroactive
	// cannot fire. Sessions only exist from Backhog usage, so a platform
	// never touched here reads as dormant.
	PlatformDormant *bool
	// DistinctGenerations is how many distinct console generations the
	// user's finishes span. Platforms without a real generation (PC,
	// unknown hardware) never count.
	DistinctGenerations int
	// NintendoConsoles is how many distinct Nintendo home consoles the
	// finishes cover — Switch hybrids included, handhelds excluded.
	NintendoConsoles int
	// BigNConsoles is how many of The Big N's seven curated consoles
	// carry a finish.
	BigNConsoles int
	// GameBoySystems is how many of the three Game Boy systems carry a
	// finish.
	GameBoySystems int
	// HandheldGenerations is how many distinct generations of handhelds
	// the finishes span.
	HandheldGenerations int
	// PilgrimConsoles is how many of the five curated PlayStation home
	// consoles carry a finish.
	PilgrimConsoles int
	// XboxGenerations is how many distinct Xbox generations the finishes
	// span.
	XboxGenerations int
}

// DropCycle is one drop-and-return arc: when the entry was dropped and, if
// it came back, when.
type DropCycle struct {
	DroppedAt time.Time
	ResumedAt *time.Time
}

// SeriesStanding is the user's position in one series of the finished game
// at the evaluation moment, this finish included.
type SeriesStanding struct {
	// Owned is how many members the user owns: non-wishlist,
	// non-ignored, kind 'game' — the same bar the series index uses.
	Owned int
	// Played is how many of those are finished, this one included.
	Played int
	// Unplayed is how many still sit backlog or playing. Dropped
	// members are neither played nor unplayed — they owe nothing.
	Unplayed int
	// YearPlayed is how many of the series' finishes landed in the
	// event's calendar year, this one included.
	YearPlayed int
}

// Event is one evaluation trigger.
type Event struct {
	Kind  string
	Entry Entry
}

// finished reports whether this event counts as finishing a game: a direct
// status flip, or a session logged against an already-finished game (the
// final-session-after-flip flow).
func (e Event) finished() bool {
	return e.Kind == EventFinished ||
		(e.Kind == EventSession && e.Entry.Status == models.StatusPlayed)
}

// dropped mirrors finished for drops.
func (e Event) dropped() bool {
	return e.Kind == EventDropped ||
		(e.Kind == EventSession && e.Entry.Status == models.StatusDropped)
}

// resumed reports whether this event is a dropped game coming back.
func (e Event) resumed() bool {
	return e.Kind == EventResumed
}

// book reports whether the event's snapshot belongs to the books arena.
// Book predicates guard on it: the two arenas share field names for their
// aggregates, and the store routes by domain anyway, so a game snapshot —
// or an unscoped test fixture — can never tip a shelf ladder.
func (e Event) book() bool {
	return e.Entry.MediaType == models.MediaBook
}

// TimeSnapshot carries the wall-clock aggregates behind a lazy predicate:
// achievements like "30 days without adding a game" have no mutation event
// to hook, so they evaluate against these on gallery loads. Each field is
// scoped to its own domain — the games arena measures acquisition recency,
// the books arena measures how much of a physical page map exists.
type TimeSnapshot struct {
	Now time.Time
	// LastAcquiredAt is MAX(library_entries.created_at) over non-wishlist
	// game entries — when the user last actually added a game to their
	// library rather than its wishlist. Zero time when they own nothing, in
	// which case time predicates should not fire.
	LastAcquiredAt time.Time
	// BookPagesMapped is the largest number of pages the user has mapped by
	// scanning a single physical copy. Zero when no copy carries a scanned
	// page yet.
	BookPagesMapped int
}

// Definition couples a catalogue entry with its unlock predicate. The
// domain lives on the embedded Achievement — one value serves both the
// API payload and the routing rule below.
type Definition struct {
	Achievement models.Achievement
	// Predicate is the event-hooked unlock test; nil for achievements
	// that only unlock through a lazy time predicate or an egg endpoint.
	Predicate func(Event) bool
	// TimePredicate is the lazy, mutation-free counterpart: evaluated on
	// gallery loads against wall-clock aggregates. Nil for achievements
	// that unlock off events.
	TimePredicate func(TimeSnapshot) bool
}

// MatchesDomain reports whether the definition applies to an event from the
// given media type. Arena-agnostic definitions (the eggs) match anything;
// the rest only ever see their own arena, which is what keeps a book finish
// from moving a game ladder and vice versa.
func (d Definition) MatchesDomain(mediaType string) bool {
	return d.Achievement.Domain == models.DomainAny || d.Achievement.Domain == mediaType
}

// HasTimePredicates reports whether any catalogue entry has a lazy time
// predicate, so callers can skip the wall-clock read when none do.
func HasTimePredicates() bool {
	for _, def := range Catalogue {
		if def.TimePredicate != nil {
			return true
		}
	}
	return false
}

// Catalogue is every achievement, in display order. Tiers follow the rubric:
// first steps bronze, multi-game grinds and long-wait feats silver, 25-scale
// grinds gold, 100-scale and absurd feats legendary.
var Catalogue = []Definition{
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "first_blood",
			Title:       "First Blood",
			Description: "Finish your first backlog game.",
			Icon:        "droplet",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= 1
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "cleanup_crew",
			Title:       "Cleanup Crew",
			Description: "Finish 5 backlog games.",
			Icon:        "brush",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= cleanupCrewCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "spring_cleaning",
			Title:       "Spring Cleaning",
			Description: "Finish 10 backlog games.",
			Icon:        "broom",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= springCleaningCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "deep_clean",
			Title:       "Deep Clean",
			Description: "Finish 25 backlog games.",
			Icon:        "bubbles",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= deepCleanCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "hazmat_suit",
			Title:       "Hazmat Suit",
			Description: "Finish 50 backlog games.",
			Icon:        "gas-mask",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= hazmatSuitCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "the_purge",
			Title:       "The Purge",
			Description: "Finish 100 backlog games.",
			Icon:        "small-fire",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= thePurgeCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "one_down",
			Title:       "One Down",
			Description: "Finish a game while 10+ sit unplayed.",
			Icon:        "chevrons-down",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.UnplayedCount >= oneDownUnplayed
		},
	},
	{
		Achievement: models.Achievement{
			Domain: models.DomainGame,
			ID:     "next_up",
			Title:  "Next!",
			Description: "Finish the game sitting at the top of your queue " +
				"(marked finished straight from the queue).",
			Icon: "chevrons-up",
			Tier: models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.WasQueueTop
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "making_a_dent",
			Title:       "Making a Dent",
			Description: "Shrink your unplayed backlog by 10 from its peak.",
			Icon:        "hammer-drop",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= makingADentReduction
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "dentist_appointment",
			Title:       "Dentist Appointment",
			Description: "Shrink your unplayed backlog by 25 from its peak.",
			Icon:        "tooth",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= dentistAppointmentDrop
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "mass_extinction",
			Title:       "Mass Extinction",
			Description: "Shrink your unplayed backlog by 50 from its peak.",
			Icon:        "meteor-impact",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= massExtinctionReduction
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "empty_the_closet",
			Title:       "Empty the Closet",
			Description: "Reach zero unplayed games after hoarding 10+.",
			Icon:        "wooden-door",
			Tier:        models.TierGold,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return (e.finished() || e.dropped()) &&
				e.Entry.UnplayedCount == 0 &&
				e.Entry.PeakUnplayedCount >= emptyTheClosetPeakUnowned
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "backlog_negative",
			Title:       "Backlog Negative",
			Description: "Finish more games in a year than you add.",
			Icon:        "minus",
			Tier:        models.TierGold,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.YearFinishes > e.Entry.YearAdditions
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "hat_trick",
			Title:       "Hat Trick",
			Description: "Finish 3 games in one calendar month.",
			Icon:        "star",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.MonthFinishes >= hatTrickMonth
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "cleanup_month",
			Title:       "Cleanup Month",
			Description: "Finish 5 games in one calendar month.",
			Icon:        "calendar",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.MonthFinishes >= cleanupMonthCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "backlog_machine",
			Title:       "Backlog Machine",
			Description: "Finish at least one game in 3 consecutive months.",
			Icon:        "layers",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.FinishStreak >= backlogMachineStreak
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "perfect_season",
			Title:       "Perfect Season",
			Description: "Finish at least one game every month of a calendar year.",
			Icon:        "check-circle",
			Tier:        models.TierLegendary,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.YearMonthsFinished >= perfectSeasonMonths
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "season_opener",
			Title:       "Season Opener",
			Description: "Finish a backlog game in January.",
			Icon:        "play",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.FinishMonth == int(time.January)
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "strong_finish",
			Title:       "Strong Finish",
			Description: "Finish a backlog game in December.",
			Icon:        "check",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.FinishMonth == int(time.December)
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "summer_cleanup",
			Title:       "Summer Cleanup",
			Description: "Finish 5 games in a single summer (June–August).",
			Icon:        "zap",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && IsSummer(e.Entry.FinishMonth) &&
				e.Entry.SummerFinishes >= summerCleanupCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "restraint",
			Title:       "Restraint",
			Description: "Go 30 days without adding a single game.",
			Icon:        "clock",
			Tier:        models.TierSilver,
		},
		TimePredicate: func(ts TimeSnapshot) bool {
			return ts.Now.Sub(ts.LastAcquiredAt) >= restraintWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "discipline",
			Title:       "Discipline",
			Description: "Go 90 days without adding a single game.",
			Icon:        "gauge",
			Tier:        models.TierGold,
		},
		TimePredicate: func(ts TimeSnapshot) bool {
			return ts.Now.Sub(ts.LastAcquiredAt) >= disciplineWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "dusty_relic",
			Title:       "Dusty Relic",
			Description: "Finish a game you've owned for 3+ years (owned = since you added it to Backhog).",
			Icon:        "amphora",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) >= dustyRelicWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "archaeologist",
			Title:       "Archaeologist",
			Description: "Finish a game you owned for 5+ years.",
			Icon:        "shovel",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) >= archaeologistWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "lost_civilization",
			Title:       "Lost Civilization",
			Description: "Finish a game you've owned for 7+ years.",
			Icon:        "mayan-pyramid",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) >= lostCivilizationWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "ancient_artifact",
			Title:       "Ancient Artifact",
			Description: "Finish a game you've owned for 10+ years.",
			Icon:        "stone-tablet",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) >= ancientArtifactWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "time_capsule",
			Title:       "Time Capsule",
			Description: "Finish a game released 10+ years before you finished it.",
			Icon:        "time-trap",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.FirstReleaseDate != nil &&
				e.Entry.At.Sub(time.Unix(*e.Entry.FirstReleaseDate, 0)) >= timeCapsuleWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "old_hardware",
			Title:       "Old Hardware, New Victory",
			Description: "Finish a game originally released before 2000.",
			Icon:        "vintage-robot",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.FirstReleaseDate != nil &&
				time.Unix(*e.Entry.FirstReleaseDate, 0).Year() < oldHardwareBeforeYear
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "speedrun",
			Title:       "Speedrun",
			Description: "Finish a game with under 5 hours logged.",
			Icon:        "timer",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.LoggedMinutes < speedrunMinutes
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "instant_gratification",
			Title:       "Instant Gratification",
			Description: "Finish a game within 30 days of adding it.",
			Icon:        "lightning-storm",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) <= instantGratificationWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "no_shelf_time",
			Title:       "No Shelf Time",
			Description: "Finish a game within 7 days of adding it.",
			Icon:        "bookshelf",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && ownedFor(e.Entry) <= noShelfTimeWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "long_haul",
			Title:       "Long Haul",
			Description: "Finish a game that takes 50+ hours to beat.",
			Icon:        "mountain",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.TimeToBeatMain != nil &&
				*e.Entry.TimeToBeatMain >= LongHaulSeconds
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "ultra_marathon",
			Title:       "Ultra Marathon",
			Description: "Finish a game that takes 100+ hours to beat.",
			Icon:        "joystick",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.TimeToBeatMain != nil &&
				*e.Entry.TimeToBeatMain >= ultraMarathonSeconds
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "the_commitment",
			Title:       "The Commitment",
			Description: "Finish 3 games that take 50+ hours to beat.",
			Icon:        "list-checks",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.LongHaulFinishes >= theCommitmentGames
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "completionist",
			Title:       "Completionist",
			Description: "Finish a game having logged its full completion time.",
			Icon:        "target",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.TimeToBeatComplete != nil &&
				int64(e.Entry.LoggedMinutes)*60 >= *e.Entry.TimeToBeatComplete
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "abandonment_issues",
			Title:       "Abandonment Issues",
			Description: "Drop a game you've owned for a year or more. Honesty counts.",
			Icon:        "door-open",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && ownedFor(e.Entry) >= abandonmentWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "know_when_to_fold",
			Title:       "Know When to Fold 'Em",
			Description: "Drop a game after 5+ hours of honest effort.",
			Icon:        "hand",
			Tier:        models.TierSilver,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && e.Entry.LoggedMinutes >= knowWhenToFoldMinutes
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "cut_your_losses",
			Title:       "Cut Your Losses",
			Description: "Drop a game with less than 10% of its main story logged.",
			Icon:        "swords",
			Tier:        models.TierBronze,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && e.Entry.TimeToBeatMain != nil &&
				int64(e.Entry.LoggedMinutes)*60*cutYourLossesDivisor < *e.Entry.TimeToBeatMain
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "buyers_remorse",
			Title:       "Buyer's Remorse",
			Description: "Drop a game within 7 days of adding it.",
			Icon:        "gift",
			Tier:        models.TierBronze,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && ownedFor(e.Entry) <= buyersRemorseWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "wasnt_you_it_was_me",
			Title:       "It Wasn't You, It Was Me",
			Description: "Drop 5 games. It's not them, it's you.",
			Icon:        "x-circle",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && e.Entry.DroppedCount >= wasntYouDropCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "the_reaper",
			Title:       "The Reaper",
			Description: "Drop 10 games.",
			Icon:        "ban",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.dropped() && e.Entry.DroppedCount >= theReaperDropCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "resurrection",
			Title:       "Resurrection",
			Description: "Bring a dropped game back into the rotation.",
			Icon:        "refresh",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.resumed()
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "second_chance",
			Title:       "Second Chance",
			Description: "Resume a game 6+ months after dropping it.",
			Icon:        "history",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.resumed() && longestReturnGap(e.Entry) >= secondChanceWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "never_give_up",
			Title:       "Never Give Up",
			Description: "Finish a game you previously dropped.",
			Icon:        "flag",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && len(e.Entry.DropHistory) > 0
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "against_all_odds",
			Title:       "Against All Odds",
			Description: "Resume a game after a year away and finish it.",
			Icon:        "dices",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && longestReturnGap(e.Entry) >= againstAllOddsWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "phoenix",
			Title:       "Phoenix",
			Description: "Return to a game 2+ years after dropping it and finish it.",
			Icon:        "sparkles",
			Tier:        models.TierLegendary,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && longestReturnGap(e.Entry) >= phoenixWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "the_ancient_one",
			Title:       "The Ancient One",
			Description: "Finish the oldest game you own.",
			Icon:        "hourglass",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.IsOldestOwned
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "fossil_record",
			Title:       "The Fossil Record",
			Description: "Finish one of the 3 oldest games you own.",
			Icon:        "fossil",
			Tier:        models.TierGold,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.CreatedAtRank > 0 &&
				e.Entry.CreatedAtRank <= fossilRecordOldest
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "trilogy",
			Title:       "Trilogy",
			Description: "Finish 3 games from the same series.",
			Icon:        "book-pile",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.Played >= trilogyGames
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "back_to_back",
			Title:       "Back to Back",
			Description: "Finish two games of the same series in a row, with nothing finished between them.",
			Icon:        "linked-rings",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PrevFinishSharesSeries
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "saga",
			Title:       "Saga",
			Description: "Finish a game from a series you own 5+ entries of.",
			Icon:        "scroll-unfurled",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.Owned >= sagaEntries
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "franchise_mode",
			Title:       "Franchise Mode",
			Description: "Finish 5 games from the same series.",
			Icon:        "imperial-crown",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.Played >= franchiseModeGames
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "marathon_series",
			Title:       "Marathon Series",
			Description: "Finish 3 games of one series within a calendar year.",
			Icon:        "sprint",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.YearPlayed >= marathonSeriesGames
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "closing_the_loop",
			Title:       "Closing the Loop",
			Description: "Finish the last unplayed game in a series you own — dropped entries don't count against you.",
			Icon:        "cycle",
			Tier:        models.TierGold,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.Owned >= fullSetMinOwned && s.Unplayed == 0
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "full_set",
			Title:       "The Full Set",
			Description: "Finish every game in a series you own (2+ entries, all played).",
			Icon:        "full-folder",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.finished() && anySeries(e.Entry, func(s SeriesStanding) bool {
				return s.Owned >= fullSetMinOwned && s.Played == s.Owned
			})
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "sampler",
			Title:       "Sampler",
			Description: "Finish games from 5 different genres in one calendar year.",
			Icon:        "pizza-slice",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.YearGenres >= samplerGenres
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "world_tour",
			Title:       "World Tour",
			Description: "Finish games on 5 different platforms.",
			Icon:        "compass",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.DistinctPlatforms >= worldTourPlatforms
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "retroactive",
			Title:       "Retroactive",
			Description: "Finish a game on a platform with no logged session in the last 5 years.",
			Icon:        "dust-cloud",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlatformDormant != nil && *e.Entry.PlatformDormant
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "generation_gap",
			Title:       "Generation Gap",
			Description: "Finish games on 5 different console generations.",
			Icon:        "family-tree",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.DistinctGenerations >= generationGapGenerations
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "nintendo_time_machine",
			Title:       "Nintendo Time Machine",
			Description: "Finish games on 5 different Nintendo consoles.",
			Icon:        "cuckoo-clock",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.NintendoConsoles >= nintendoTimeMachineConsoles
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "the_big_n",
			Title:       "The Big N",
			Description: "Finish a game on NES, SNES, N64, GameCube, Wii, Wii U, and Switch.",
			Icon:        "mushroom-gills",
			Tier:        models.TierLegendary,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.BigNConsoles >= bigNConsoles
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "game_boy_generation",
			Title:       "Game Boy Generation",
			Description: "Finish a game on Game Boy, Game Boy Color, and Game Boy Advance.",
			Icon:        "gamepad",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.GameBoySystems >= gameBoySystems
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "handheld_historian",
			Title:       "Handheld Historian",
			Description: "Finish games on 4 generations of handhelds.",
			Icon:        "knapsack",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.HandheldGenerations >= handheldHistorianGenerations
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "playstation_pilgrim",
			Title:       "PlayStation Pilgrim",
			Description: "Finish a game on PS1, PS2, PS3, PS4, and PS5.",
			Icon:        "footprint",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PilgrimConsoles >= pilgrimConsoles
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "green_across_the_ages",
			Title:       "Green Across the Ages",
			Description: "Finish games on 4 generations of Xbox.",
			Icon:        "clover",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.XboxGenerations >= xboxGenerations
		},
	},
	// The books arena: the same principle as the games catalogue — progress
	// through the pile, never hours in it — spoken in the same dry voice,
	// about the shelf instead of the backlog. Every aggregate a book
	// predicate reads is book-scoped by the store, so a finish here never
	// moves a game ladder and a game finish never moves these.
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "first_edition",
			Title:       "First Edition",
			Description: "Finish your first book.",
			Icon:        "book-pile",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= firstEditionCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "shelf_improvement",
			Title:       "Shelf Improvement",
			Description: "Finish 5 books.",
			Icon:        "bookshelf",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= shelfImprovement
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "well_read",
			Title:       "Well Read",
			Description: "Finish 10 books.",
			Icon:        "layers",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= wellReadCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "library_card",
			Title:       "Library Card",
			Description: "Finish 25 books.",
			Icon:        "scroll-unfurled",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= libraryCardCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "branch_library",
			Title:       "Branch Library",
			Description: "Finish 50 books.",
			Icon:        "building",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= branchLibraryCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "the_librarian",
			Title:       "The Librarian",
			Description: "Finish 100 books. The shelf refills; that's the deal.",
			Icon:        "imperial-crown",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PlayedCount >= theLibrarianCount
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "late_fine",
			Title:       "Late Fine",
			Description: "Finish a book you've owned for 5+ years.",
			Icon:        "time-trap",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && ownedFor(e.Entry) >= lateFineWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "third_times_the_charm",
			Title:       "Third Time's the Charm",
			Description: "Finish a book you started and abandoned twice before.",
			Icon:        "cycle",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && len(e.Entry.DropHistory) >= thirdTimeCharmAbandons
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "every_which_way",
			Title:       "Every Which Way",
			Description: "Finish one book in all three formats: paper, ebook, and audio.",
			Icon:        "headphones",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.FormatCount >= everyWhichWayFormats
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "tbr_trim",
			Title:       "TBR Trim",
			Description: "Shrink your unread pile by 10 from its peak.",
			Icon:        "chevrons-down",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.book() && (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= tbrTrimReduction
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "shelf_control",
			Title:       "Shelf Control",
			Description: "Shrink your unread pile by 25 from its peak.",
			Icon:        "sliders",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.book() && (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= shelfControlDrop
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "spark_joy",
			Title:       "Spark Joy",
			Description: "Shrink your unread pile by 50 from its peak.",
			Icon:        "sparkles",
			Tier:        models.TierLegendary,
		},
		Predicate: func(e Event) bool {
			return e.book() && (e.finished() || e.dropped()) && unplayedReduction(e.Entry) >= sparkJoyReduction
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "breaking_even",
			Title:       "Breaking Even",
			Description: "Finish more books in a year than you buy.",
			Icon:        "minus",
			Tier:        models.TierGold,
			Hidden:      true,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.YearFinishes > e.Entry.YearAdditions
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "doorstop",
			Title:       "Doorstop",
			Description: "Finish a book over 600 pages long.",
			Icon:        "mountain",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.finished() && e.Entry.PageCount > doorstopPages
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "honest_dnf",
			Title:       "The Honest DNF",
			Description: "Drop a book you've left 'reading' for two years. Honesty counts.",
			Icon:        "door-open",
			Tier:        models.TierBronze,
		},
		Predicate: func(e Event) bool {
			return e.book() && e.dropped() && e.Entry.StartedAt != nil &&
				e.Entry.At.Sub(*e.Entry.StartedAt) >= honestDNFWindow
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainBook,
			ID:          "cartographer",
			Title:       "Cartographer",
			Description: "Map 25 pages of a physical copy by scanning them.",
			Icon:        "camera",
			Tier:        models.TierSilver,
		},
		TimePredicate: func(ts TimeSnapshot) bool {
			return ts.BookPagesMapped >= cartographerPages
		},
	},
	// The easter eggs: unlockable only by playing with the app itself, never
	// by a predicate. Egg implies Hidden — the locked card shows ??? and a
	// tease, and the reveal rides the normal unlock toast. The client fires
	// POST /api/achievements/{id}/egg when it detects the interaction; the
	// endpoint only accepts ids from this club.
	{
		Achievement: models.Achievement{
			Domain:      models.DomainGame,
			ID:          "night_owl",
			Title:       "Do You Even Sleep?",
			Description: "Log a play session between 3 and 5 in the morning.",
			Icon:        "night-sleep",
			Tier:        models.TierBronze,
			Hidden:      true,
			Egg:         true,
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainAny,
			ID:          "hog_watcher",
			Title:       "Hog Watcher",
			Description: "Click the Backhog logo 10 times in a row.",
			Icon:        "eyeball",
			Tier:        models.TierBronze,
			Hidden:      true,
			Egg:         true,
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainAny,
			ID:          "konami",
			Title:       "Old Habits",
			Description: "Enter the Konami code on the achievements page.",
			Icon:        "keyboard",
			Tier:        models.TierSilver,
			Hidden:      true,
			Egg:         true,
		},
	},
	{
		Achievement: models.Achievement{
			Domain:      models.DomainAny,
			ID:          "queue_shuffler",
			Title:       "Chaos Gremlin",
			Description: "Re-queue the same game 5 times in one sitting.",
			Icon:        "whirlwind",
			Tier:        models.TierBronze,
			Hidden:      true,
			Egg:         true,
		},
	},
}

// ByID returns the catalogue entry with that code key, or nil.
func ByID(id string) *models.Achievement {
	for i := range Catalogue {
		if Catalogue[i].Achievement.ID == id {
			return &Catalogue[i].Achievement
		}
	}
	return nil
}

// ownedFor is how long the entry had been in the library at the event.
func ownedFor(e Entry) time.Duration {
	return e.At.Sub(e.CreatedAt)
}

// IsSummer reports whether a calendar month (1–12) falls in the June–August
// window the single-summer predicates bucket on. The store's backfill shares
// it so the bucket can't drift from the predicate.
func IsSummer(month int) bool {
	return month >= 6 && month <= 8
}

// unplayedReduction is how far the unplayed backlog sits below its all-time
// peak — finishes and drops both count as shrinking it, honesty included.
func unplayedReduction(e Entry) int {
	return e.PeakUnplayedCount - e.UnplayedCount
}

// anySeries reports whether any series the finished game belongs to passes
// the test. Standings exist only for kind-'game' memberships, so DLC and
// expansion finishes never clear the bar.
func anySeries(e Entry, test func(SeriesStanding) bool) bool {
	for _, s := range e.SeriesStandings {
		if test(s) {
			return true
		}
	}
	return false
}

// longestReturnGap is the longest span between a drop and the return that
// ended it: the resume when one followed, or the event's own moment when the
// arc is still open — a dropped game finished directly is its own return.
// Zero when the entry has never been dropped.
func longestReturnGap(e Entry) time.Duration {
	var longest time.Duration
	for _, cycle := range e.DropHistory {
		returnAt := e.At
		if cycle.ResumedAt != nil {
			returnAt = *cycle.ResumedAt
		}
		if gap := returnAt.Sub(cycle.DroppedAt); gap > longest {
			longest = gap
		}
	}
	return longest
}

// Placeholders a locked hidden achievement is served with. The identity is
// the reward for unlocking; only the tier leaks, so the gallery can still
// group it. The description placeholder is the fallback for hidden entries
// without custom teasing copy — every curated one has a tease in maskedHints.
const (
	MaskedTitle       = "???"
	MaskedDescription = "Hidden achievement"
	MaskedIcon        = "lock"
)

// maskedHints is the teasing copy a locked hidden card wears instead of a
// description: flavor that hints at the shape of the thing without naming
// it. The tease is part of the fun — write one when hiding an achievement.
var maskedHints = map[string]string{
	"empty_the_closet":  "One day the closet will be empty. You'll know.",
	"backlog_negative":  "Spend less than you bring in. You do the math.",
	"perfect_season":    "A whole year, every month. You'll know it when it's done.",
	"closing_the_loop":  "Some loops are waiting to be closed.",
	"the_big_n":         "Seven of a kind, from one house.",
	"fossil_record":     "The oldest things you own remember.",
	"phoenix":           "Some things come back from the ashes.",
	"buyers_remorse":    "Buyer's... something. You'll know it when you feel it.",
	"know_when_to_fold": "You'll know it when you hold it.",
	"cut_your_losses":   "Sometimes the kindest cut is early.",
	"night_owl":         "You'll know it when you feel it. Especially around 3 AM.",
	"hog_watcher":       "Keep watching the hog.",
	"konami":            "Some codes never die.",
	"queue_shuffler":    "Chaos is a ladder. Or a queue.",
	"breaking_even":     "Arithmetic, but for books. You can do arithmetic.",
}

// MaskedHint returns the teasing copy for a locked hidden achievement, or
// the generic placeholder when it has none.
func MaskedHint(id string) string {
	if hint, ok := maskedHints[id]; ok {
		return hint
	}
	return MaskedDescription
}

// Present returns the achievement as it should be served for the given lock
// state: hidden achievements mask their identity while locked — title, tease,
// and lock icon — and reveal the real data on unlock, so the reveal lives in
// the unlock toast and gallery.
func Present(a models.Achievement, locked bool) models.Achievement {
	if !a.Hidden || !locked {
		return a
	}
	a.Title = MaskedTitle
	a.Description = MaskedHint(a.ID)
	a.Icon = MaskedIcon
	return a
}

// IsEgg reports whether id is an easter-egg achievement — the only ids the
// egg endpoint will unlock.
func IsEgg(id string) bool {
	def := ByID(id)
	return def != nil && def.Egg
}
