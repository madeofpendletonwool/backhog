package achievements

import (
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

var base = Entry{
	ID:        "e1",
	Status:    models.StatusPlayed,
	CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
}

func i64(v int64) *int64 { return &v }

// unlockIDs runs an event against the catalogue and returns the ids of every
// achievement whose predicate fired.
func unlockIDs(e Event) []string {
	var ids []string
	for _, def := range Catalogue {
		if def.Predicate(e) {
			ids = append(ids, def.Achievement.ID)
		}
	}
	return ids
}

func TestPredicates(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  []string
	}{
		{
			name: "first finished game with no logged time",
			event: Event{Kind: EventFinished, Entry: Entry{
				ID: "e1", Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				PlayedCount: 1,
			}},
			want: []string{"first_blood", "speedrun"},
		},
		{
			name: "fifth finish unlocks cleanup crew",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "finish after five years of ownership",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "archaeologist"},
		},
		{
			name: "four and a half years of ownership is not archaeologist",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "under five hours logged at completion",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2,  1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 299, PlayedCount: 2,
			}},
			want: []string{"first_blood", "speedrun"},
		},
		{
			name: "exactly five hours logged is not speedrun",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 300, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "50h game at the boundary",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 3010, PlayedCount: 3,
				TimeToBeatMain: i64(50 * 3600),
			}},
			want: []string{"first_blood", "long_haul"},
		},
		{
			name: "unknown time to beat never qualifies long haul",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 3010, PlayedCount: 3,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "logged hours cover the completionist estimate",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
				TimeToBeatComplete: i64(100 * 3600),
			}},
			want: []string{"first_blood", "completionist"},
		},
		{
			name: "session against a finished game re-evaluates finish predicates",
			event: Event{Kind: EventSession, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
				TimeToBeatComplete: i64(100 * 3600),
			}},
			want: []string{"first_blood", "completionist"},
		},
		{
			name: "session against a playing game unlocks nothing",
			event: Event{Kind: EventSession, Entry: Entry{
				Status: models.StatusPlaying,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
			}},
			want: nil,
		},
		{
			name: "dropping after a year of ownership",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status: models.StatusDropped,
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: []string{"abandonment_issues"},
		},
		{
			name: "dropping a recent purchase unlocks nothing",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status: models.StatusDropped,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: nil,
		},
		{
			name: "finishing the oldest owned game",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				IsOldestOwned: true,
			}},
			want: []string{"first_blood", "cleanup_crew", "archaeologist", "the_ancient_one"},
		},
		{
			name: "finishing a newer game is not the ancient one",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status: models.StatusPlayed,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				IsOldestOwned: false,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "session against a dropped game can only re-fire drop predicates",
			event: Event{Kind: EventSession, Entry: Entry{
				Status: models.StatusDropped,
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				At:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				PlayedCount: 9,
			}},
			want: []string{"abandonment_issues"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unlockIDs(tt.event)
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

func TestCatalogueIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range Catalogue {
		a := def.Achievement
		if a.ID == "" || a.Title == "" || a.Description == "" || a.Icon == "" {
			t.Errorf("catalogue entry %+v has an empty field", a)
		}
		if seen[a.ID] {
			t.Errorf("duplicate catalogue id %q", a.ID)
		}
		seen[a.ID] = true
		if ByID(a.ID) == nil {
			t.Errorf("ByID(%q) = nil", a.ID)
		}
	}
	if ByID("does-not-exist") != nil {
		t.Error("ByID for unknown id should be nil")
	}
}
