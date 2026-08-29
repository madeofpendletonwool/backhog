// Package achievements holds the achievement catalogue: the code-defined list
// of things worth celebrating about backlog progress. The guiding principle is
// to gamify making progress through the backlog — finishing, rescuing, and
// clearing — never playing games themselves. No points for hours logged.
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
	// archaeologistWindow is how long a game must have been owned at finish.
	archaeologistWindow = 5 * 365 * 24 * time.Hour
	// abandonmentWindow is how long a dropped game must have been owned.
	// Matches the season's rescue threshold (owned ≥ 1 year).
	abandonmentWindow = 365 * 24 * time.Hour
	// speedrunMinutes caps logged playtime at completion for Speedrun.
	speedrunMinutes = 5 * 60
	// longHaulSeconds is the time-to-beat that qualifies a game as Long Haul.
	longHaulSeconds = 50 * 3600
)

// Entry is the snapshot of the triggering library entry, plus the user-level
// aggregates the predicates need, all read inside the store's transaction.
type Entry struct {
	ID        string
	Status    string // status as of the event
	CreatedAt time.Time
	// At is the moment the predicate evaluates against: the finish or drop
	// timestamp when there is one, otherwise now.
	At                 time.Time
	LoggedMinutes      int    // total logged playtime at evaluation time
	TimeToBeatMain     *int64 // seconds, nil when unknown
	TimeToBeatComplete *int64 // seconds, nil when unknown
	// PlayedCount is how many games the user has finished including this one.
	PlayedCount int
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
	// SeriesIDs lists the series (franchise or collection) the game belongs to.
	SeriesIDs []string
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

// TimeSnapshot carries the wall-clock aggregates behind a lazy predicate:
// achievements like "30 days without adding a game" have no mutation event
// to hook, so they evaluate against these on gallery loads.
type TimeSnapshot struct {
	Now time.Time
	// LastAcquiredAt is MAX(library_entries.created_at) — when the user last
	// added any entry, wishlist included. Zero time when they own nothing,
	// in which case time predicates should not fire.
	LastAcquiredAt time.Time
}

// Definition couples a catalogue entry with its unlock predicate.
type Definition struct {
	Achievement models.Achievement
	Predicate   func(Event) bool
	// TimePredicate is the lazy, mutation-free counterpart: evaluated on
	// gallery loads against wall-clock aggregates. Nil for achievements
	// that unlock off events.
	TimePredicate func(TimeSnapshot) bool
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
			ID:          "empty_the_closet",
			Title:       "Empty the Closet",
			Description: "Reach zero unplayed games after hoarding 10+.",
			Icon:        "wooden-door",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return (e.finished() || e.dropped()) &&
				e.Entry.UnplayedCount == 0 &&
				e.Entry.PeakUnplayedCount >= emptyTheClosetPeakUnowned
		},
	},
	{
		Achievement: models.Achievement{
			ID:          "backlog_negative",
			Title:       "Backlog Negative",
			Description: "Finish more games in a year than you add.",
			Icon:        "minus",
			Tier:        models.TierGold,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.YearFinishes > e.Entry.YearAdditions
		},
	},
	{
		Achievement: models.Achievement{
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
			ID:          "long_haul",
			Title:       "Long Haul",
			Description: "Finish a game that takes 50+ hours to beat.",
			Icon:        "mountain",
			Tier:        models.TierSilver,
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.TimeToBeatMain != nil &&
				*e.Entry.TimeToBeatMain >= longHaulSeconds
		},
	},
	{
		Achievement: models.Achievement{
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

// unplayedReduction is how far the unplayed backlog sits below its all-time
// peak — finishes and drops both count as shrinking it, honesty included.
func unplayedReduction(e Entry) int {
	return e.PeakUnplayedCount - e.UnplayedCount
}

// Placeholders a locked hidden achievement is served with. The identity is
// the reward for unlocking; only the tier leaks, so the gallery can still
// group it.
const (
	MaskedTitle       = "???"
	MaskedDescription = "Hidden achievement"
	MaskedIcon        = "lock"
)

// Present returns the achievement as it should be served for the given lock
// state: hidden achievements mask their identity while locked and reveal the
// real data on unlock, so the reveal lives in the unlock toast and gallery.
func Present(a models.Achievement, locked bool) models.Achievement {
	if !a.Hidden || !locked {
		return a
	}
	a.Title = MaskedTitle
	a.Description = MaskedDescription
	a.Icon = MaskedIcon
	return a
}
