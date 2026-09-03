package achievements

import (
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// unlockIDsForDomain mirrors the store's evaluation loop: only definitions
// whose domain admits the media type are even asked. It is what keeps the
// book assertions below honest about routing, not just predicates.
func unlockIDsForDomain(mediaType string, e Event) []string {
	var ids []string
	for _, def := range Catalogue {
		if !def.MatchesDomain(mediaType) {
			continue
		}
		if def.Predicate != nil && def.Predicate(e) {
			ids = append(ids, def.Achievement.ID)
		}
	}
	return ids
}

// bookEvent builds a books-arena finish event at a known instant. CreatedAt
// defaults to At — just added — so the ownership-age predicate stays quiet
// unless a case deliberately reaches back.
func bookEvent(kind string, e Entry) Event {
	e.MediaType = models.MediaBook
	if e.Status == "" {
		e.Status = models.StatusPlayed
	}
	if e.At.IsZero() {
		e.At = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = e.At
	}
	return Event{Kind: kind, Entry: e}
}

func TestBookPredicates(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  []string
	}{
		{
			name:  "first finished book opens the ladder",
			event: bookEvent(EventFinished, Entry{PlayedCount: 1}),
			want:  []string{"first_edition"},
		},
		{
			name:  "fifth finish earns shelf improvement",
			event: bookEvent(EventFinished, Entry{PlayedCount: 5}),
			want:  []string{"first_edition", "shelf_improvement"},
		},
		{
			name:  "the hundredth finish fires the whole book ladder",
			event: bookEvent(EventFinished, Entry{PlayedCount: 100}),
			want: []string{
				"first_edition", "shelf_improvement", "well_read",
				"library_card", "branch_library", "the_librarian",
			},
		},
		{
			name: "a book owned exactly the late-fine window pays it",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1,
				CreatedAt:   time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC).Add(-lateFineWindow),
			}),
			want: []string{"first_edition", "late_fine"},
		},
		{
			name: "a four-year-old book pays no fine",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1,
				CreatedAt:   time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC).Add(-4 * 365 * 24 * time.Hour),
			}),
			want: []string{"first_edition"},
		},
		{
			name: "twice-abandoned and finally finished",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1,
				DropHistory: []DropCycle{
					{DroppedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
						ResumedAt: ptrTime(time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC))},
					{DroppedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						ResumedAt: ptrTime(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))},
				},
			}),
			want: []string{"first_edition", "third_times_the_charm"},
		},
		{
			name: "one abandonment is not yet charming",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1,
				DropHistory: []DropCycle{
					{DroppedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						ResumedAt: ptrTime(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))},
				},
			}),
			want: []string{"first_edition"},
		},
		{
			name:  "all three formats closes every which way",
			event: bookEvent(EventFinished, Entry{PlayedCount: 1, FormatCount: 3}),
			want:  []string{"first_edition", "every_which_way"},
		},
		{
			name:  "two of three formats is not the trifecta",
			event: bookEvent(EventFinished, Entry{PlayedCount: 1, FormatCount: 2}),
			want:  []string{"first_edition"},
		},
		{
			name: "the pile shrunk by ten earns tbr trim",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1, PeakUnplayedCount: 30, UnplayedCount: 20,
			}),
			want: []string{"first_edition", "tbr_trim"},
		},
		{
			name: "an honest drop shrinks the pile too",
			event: bookEvent(EventDropped, Entry{
				Status: models.StatusDropped, PeakUnplayedCount: 25, UnplayedCount: 14,
			}),
			want: []string{"tbr_trim"},
		},
		{
			name: "a reduction of nine earns nothing on the shelf ladder",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1, PeakUnplayedCount: 30, UnplayedCount: 21,
			}),
			want: []string{"first_edition"},
		},
		{
			name: "finishing more than buying breaks even",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1, YearFinishes: 4, YearAdditions: 3,
			}),
			want: []string{"first_edition", "breaking_even"},
		},
		{
			name: "finishing as many as buying breaks nothing",
			event: bookEvent(EventFinished, Entry{
				PlayedCount: 1, YearFinishes: 3, YearAdditions: 3,
			}),
			want: []string{"first_edition"},
		},
		{
			name:  "a 601-page book is a doorstop",
			event: bookEvent(EventFinished, Entry{PlayedCount: 1, PageCount: 601}),
			want:  []string{"first_edition", "doorstop"},
		},
		{
			name:  "exactly 600 pages is not over the line",
			event: bookEvent(EventFinished, Entry{PlayedCount: 1, PageCount: 600}),
			want:  []string{"first_edition"},
		},
		{
			name: "dropping a three-year 'read' is the honest dnf",
			event: bookEvent(EventDropped, Entry{
				Status:    models.StatusDropped,
				StartedAt: ptrTime(time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)),
			}),
			want: []string{"honest_dnf"},
		},
		{
			name: "dropping a one-year 'read' owes no apology",
			event: bookEvent(EventDropped, Entry{
				Status:    models.StatusDropped,
				StartedAt: ptrTime(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
			}),
			want: nil,
		},
		{
			name:  "a drop with no known start is not judged",
			event: bookEvent(EventDropped, Entry{Status: models.StatusDropped}),
			want:  nil,
		},
		{
			name: "a game snapshot never tips a shelf ladder",
			event: Event{Kind: EventFinished, Entry: Entry{
				ID: "g1", Status: models.StatusPlayed, MediaType: models.MediaGame,
				CreatedAt:     time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 100, PageCount: 900, FormatCount: 3,
				YearFinishes: 1, YearAdditions: 9,
			}},
			want: []string{
				"first_blood", "cleanup_crew", "spring_cleaning", "deep_clean",
				"hazmat_suit", "the_purge", "instant_gratification", "no_shelf_time",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The last case runs the raw catalogue on purpose: it pins the
			// book predicates' own guards against a game snapshot, which is
			// what keeps unfiltered loops honest.
			var got []string
			if tt.event.Entry.MediaType == models.MediaGame {
				got = unlockIDs(tt.event)
			} else {
				got = unlockIDsForDomain(models.MediaBook, tt.event)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("unlocked %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("unlocked %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestCartographerTimePredicate(t *testing.T) {
	def := byID("cartographer")
	if def == nil || def.TimePredicate == nil {
		t.Fatal("cartographer must be a lazy time predicate")
	}
	snap := TimeSnapshot{Now: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}

	snap.BookPagesMapped = 25
	if !def.TimePredicate(snap) {
		t.Error("25 scanned pages did not draw the map")
	}
	snap.BookPagesMapped = 24
	if def.TimePredicate(snap) {
		t.Error("24 scanned pages should not count as a map")
	}
	// The predicate reads the page map only: a stale game-acquisition date
	// in the same snapshot neither helps nor hurts it.
	snap.LastAcquiredAt = snap.Now.Add(-90 * 24 * time.Hour)
	if def.TimePredicate(snap) {
		t.Error("cartographer should not care about acquisitions")
	}
}

func TestDomainRouting(t *testing.T) {
	byID := func(id string) Definition {
		for _, def := range Catalogue {
			if def.Achievement.ID == id {
				return def
			}
		}
		t.Fatalf("%s missing from the catalogue", id)
		return Definition{}
	}
	if !byID("first_blood").MatchesDomain(models.MediaGame) ||
		byID("first_blood").MatchesDomain(models.MediaBook) {
		t.Error("first_blood must answer games only")
	}
	if !byID("first_edition").MatchesDomain(models.MediaBook) ||
		byID("first_edition").MatchesDomain(models.MediaGame) {
		t.Error("first_edition must answer books only")
	}
	for _, egg := range []string{"hog_watcher", "konami", "queue_shuffler"} {
		if !byID(egg).MatchesDomain(models.MediaGame) || !byID(egg).MatchesDomain(models.MediaBook) {
			t.Errorf("%s is arena-agnostic and must answer both", egg)
		}
	}
	if byID("night_owl").MatchesDomain(models.MediaBook) {
		t.Error("night_owl is about play sessions — games only")
	}
	for _, def := range Catalogue {
		if !models.ValidDomain(def.Achievement.Domain) {
			t.Errorf("%s carries unknown domain %q", def.Achievement.ID, def.Achievement.Domain)
		}
	}
}

// byID finds one catalogue definition by code key.
func byID(id string) *Definition {
	for i := range Catalogue {
		if Catalogue[i].Achievement.ID == id {
			return &Catalogue[i]
		}
	}
	return nil
}
