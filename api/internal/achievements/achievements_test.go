package achievements

import (
	"encoding/json"
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
				CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:          time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				PlayedCount: 1,
			}},
			want: []string{"first_blood", "speedrun"},
		},
		{
			name: "fifth finish unlocks cleanup crew",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "tenth finish unlocks spring cleaning",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 10,
			}},
			want: []string{"first_blood", "cleanup_crew", "spring_cleaning"},
		},
		{
			name: "hundredth finish fires the whole volume ladder",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 100,
			}},
			want: []string{"first_blood", "cleanup_crew", "spring_cleaning",
				"deep_clean", "hazmat_suit", "the_purge"},
		},
		{
			name: "finish with exactly ten unplayed earns one down",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2, UnplayedCount: 10,
			}},
			want: []string{"first_blood", "one_down"},
		},
		{
			name: "nine unplayed is not one down",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2, UnplayedCount: 9,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "peak shrunk by ten earns making a dent",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PeakUnplayedCount: 20, UnplayedCount: 10,
			}},
			want: []string{"first_blood", "one_down", "making_a_dent"},
		},
		{
			name: "drop counts as backlog reduction too",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:            models.StatusDropped,
				CreatedAt:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:                time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				PeakUnplayedCount: 60, UnplayedCount: 10,
			}},
			want: []string{"making_a_dent", "dentist_appointment", "mass_extinction"},
		},
		{
			name: "reduction of nine earns nothing on the dent ladder",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PeakUnplayedCount: 20, UnplayedCount: 11,
			}},
			want: []string{"first_blood", "one_down"},
		},
		{
			name: "zero unplayed after a ten-plus peak empties the closet",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PeakUnplayedCount: 10, UnplayedCount: 0,
			}},
			want: []string{"first_blood", "making_a_dent", "empty_the_closet"},
		},
		{
			name: "empty library from day one does not empty the closet",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PeakUnplayedCount: 3, UnplayedCount: 0,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "finishing more than added in the year goes negative",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				YearFinishes: 3, YearAdditions: 2,
			}},
			want: []string{"first_blood", "backlog_negative"},
		},
		{
			name: "finishing exactly as many as added is not negative",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				YearFinishes: 2, YearAdditions: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "finish after five years of ownership",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "archaeologist"},
		},
		{
			name: "four and a half years of ownership is not archaeologist",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "under five hours logged at completion",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 299, PlayedCount: 2,
			}},
			want: []string{"first_blood", "speedrun"},
		},
		{
			name: "exactly five hours logged is not speedrun",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 300, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "50h game at the boundary",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 3010, PlayedCount: 3,
				TimeToBeatMain: i64(50 * 3600),
			}},
			want: []string{"first_blood", "long_haul"},
		},
		{
			name: "unknown time to beat never qualifies long haul",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 3010, PlayedCount: 3,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "logged hours cover the completionist estimate",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
				TimeToBeatComplete: i64(100 * 3600),
			}},
			want: []string{"first_blood", "completionist"},
		},
		{
			name: "session against a finished game re-evaluates finish predicates",
			event: Event{Kind: EventSession, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
				TimeToBeatComplete: i64(100 * 3600),
			}},
			want: []string{"first_blood", "completionist"},
		},
		{
			name: "session against a playing game unlocks nothing",
			event: Event{Kind: EventSession, Entry: Entry{
				Status:        models.StatusPlaying,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 6100, PlayedCount: 4,
			}},
			want: nil,
		},
		{
			name: "dropping after a year of ownership",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:    models.StatusDropped,
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: []string{"abandonment_issues"},
		},
		{
			name: "dropping a recent purchase unlocks nothing",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:    models.StatusDropped,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: nil,
		},
		{
			name: "finishing the oldest owned game",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				IsOldestOwned: true,
			}},
			want: []string{"first_blood", "cleanup_crew", "archaeologist", "the_ancient_one"},
		},
		{
			name: "finishing a newer game is not the ancient one",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				IsOldestOwned: false,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "session against a dropped game can only re-fire drop predicates",
			event: Event{Kind: EventSession, Entry: Entry{
				Status:      models.StatusDropped,
				CreatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				At:          time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
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
		if !models.ValidTier(a.Tier) {
			t.Errorf("catalogue entry %q has invalid tier %q", a.ID, a.Tier)
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
	if HasTimePredicates() {
		t.Error("HasTimePredicates should be false while no entry defines one")
	}
}

func TestPresentMasksHiddenWhileLocked(t *testing.T) {
	hidden := models.Achievement{
		ID: "secret", Title: "The Reveal", Description: "Do the thing.",
		Icon: "trophy", Tier: models.TierGold, Hidden: true,
	}
	visible := hidden
	visible.Hidden = false

	tests := []struct {
		name      string
		a         models.Achievement
		locked    bool
		wantTitle string
		wantDesc  string
		wantIcon  string
	}{
		{
			name:      "hidden and locked is masked",
			a:         hidden,
			locked:    true,
			wantTitle: MaskedTitle, wantDesc: MaskedDescription, wantIcon: MaskedIcon,
		},
		{
			name:      "hidden and unlocked reveals",
			a:         hidden,
			locked:    false,
			wantTitle: "The Reveal", wantDesc: "Do the thing.", wantIcon: "trophy",
		},
		{
			name:      "visible and locked is untouched",
			a:         visible,
			locked:    true,
			wantTitle: "The Reveal", wantDesc: "Do the thing.", wantIcon: "trophy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Present(tt.a, tt.locked)
			if got.Title != tt.wantTitle || got.Description != tt.wantDesc || got.Icon != tt.wantIcon {
				t.Errorf("Present() = %q/%q/%q, want %q/%q/%q",
					got.Title, got.Description, got.Icon, tt.wantTitle, tt.wantDesc, tt.wantIcon)
			}
			// The tier survives masking so the gallery can still group.
			if got.Tier != models.TierGold {
				t.Errorf("tier = %q, want it preserved", got.Tier)
			}
			// Masking must not leak into the catalogue definition.
			if hidden.Title != "The Reveal" {
				t.Errorf("Present mutated its input: title = %q", hidden.Title)
			}
		})
	}
}

func TestAchievementJSONTierAndHidden(t *testing.T) {
	a := Catalogue[0].Achievement
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := fields["tier"]; !ok {
		t.Errorf("json %s has no tier key", raw)
	}
	if _, ok := fields["hidden"]; !ok {
		t.Errorf("json %s has no hidden key", raw)
	}

	var back models.Achievement
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.Tier != a.Tier || back.Hidden != a.Hidden {
		t.Errorf("round-trip tier/hidden = %q/%v, want %q/%v", back.Tier, back.Hidden, a.Tier, a.Hidden)
	}
}

func TestResumedEventKind(t *testing.T) {
	resumed := Event{Kind: EventResumed, Entry: Entry{Status: models.StatusPlaying}}
	if !resumed.resumed() {
		t.Error("EventResumed should count as resumed")
	}
	if resumed.finished() || resumed.dropped() {
		t.Error("EventResumed should not count as finished or dropped")
	}
	// Wiring only: nothing in today's catalogue unlocks on a resume. When
	// the comeback batch lands, this expectation flips to the real set.
	if ids := unlockIDs(resumed); len(ids) != 0 {
		t.Errorf("resumed event unlocked %v, want nothing yet", ids)
	}
}
