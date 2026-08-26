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
)

// Thresholds the predicates turn on.
const (
	// cleanupCrewCount is how many finished games earn Cleanup Crew.
	cleanupCrewCount = 5
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
	ID                 string
	Status             string // status as of the event
	CreatedAt          time.Time
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

// Definition couples a catalogue entry with its unlock predicate.
type Definition struct {
	Achievement models.Achievement
	Predicate   func(Event) bool
}

// Catalogue is every achievement, in display order.
var Catalogue = []Definition{
	{
		Achievement: models.Achievement{
			ID:          "first_blood",
			Title:       "First Blood",
			Description: "Finish your first backlog game.",
			Icon:        "droplet",
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
		},
		Predicate: func(e Event) bool {
			return e.finished() && e.Entry.PlayedCount >= cleanupCrewCount
		},
	},
	{
		Achievement: models.Achievement{
			ID:          "archaeologist",
			Title:       "Archaeologist",
			Description: "Finish a game you owned for 5+ years.",
			Icon:        "shovel",
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
