package achievements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

var base = Entry{
	ID:        "e1",
	Status:    models.StatusPlayed,
	CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
}

func i64(v int64) *int64 { return &v }

func ptrTime(t time.Time) *time.Time { return &t }

func boolPtr(b bool) *bool { return &b }

// unlockIDs runs an event against the catalogue and returns the ids of every
// achievement whose predicate fired. Lazy (time-window) entries have no
// event predicate and never fire here.
func unlockIDs(e Event) []string {
	var ids []string
	for _, def := range Catalogue {
		if def.Predicate != nil && def.Predicate(e) {
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
			want: []string{"first_blood", "dusty_relic", "archaeologist"},
		},
		{
			name: "four and a half years of ownership is not archaeologist",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "dusty_relic"},
		},
		{
			name: "seven years of ownership reaches lost civilization",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "dusty_relic", "archaeologist", "lost_civilization"},
		},
		{
			name: "ten years of ownership completes the age ladder",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "dusty_relic", "archaeologist",
				"lost_civilization", "ancient_artifact"},
		},
		{
			name: "owned exactly three years is a dusty relic",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC).Add(dustyRelicWindow),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "dusty_relic"},
		},
		{
			name: "one hour short of three years is not a dusty relic",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC).Add(dustyRelicWindow - time.Hour),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "release eleven years before the finish is a time capsule",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				FirstReleaseDate: i64(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-11 * 365 * 24 * time.Hour).Unix()),
			}},
			want: []string{"first_blood", "time_capsule"},
		},
		{
			name: "one hour short of the time capsule window is not one",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				FirstReleaseDate: i64(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-timeCapsuleWindow + time.Hour).Unix()),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "a pre-2000 release is old hardware and a time capsule",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				FirstReleaseDate: i64(time.Date(1999, 6, 1, 0, 0, 0, 0, time.UTC).Unix()),
			}},
			want: []string{"first_blood", "time_capsule", "old_hardware"},
		},
		{
			name: "a 2000 release is not old hardware",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				FirstReleaseDate: i64(time.Date(2000, 6, 1, 0, 0, 0, 0, time.UTC).Unix()),
			}},
			want: []string{"first_blood", "time_capsule"},
		},
		{
			name: "unknown release date unlocks no era achievements",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the third oldest owned game joins the fossil record",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				CreatedAtRank: 3,
			}},
			want: []string{"first_blood", "dusty_relic", "archaeologist", "fossil_record"},
		},
		{
			name: "the fourth oldest is outside the fossil record",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				CreatedAtRank: 4,
			}},
			want: []string{"first_blood", "dusty_relic", "archaeologist"},
		},
		{
			name: "an unset rank never joins the fossil record",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "dusty_relic", "archaeologist"},
		},
		{
			name: "finished exactly thirty days after adding it",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(instantGratificationWindow),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "instant_gratification"},
		},
		{
			name: "thirty-one days is past instant gratification",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(31 * 24 * time.Hour),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "finished exactly seven days after adding skips the shelf",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(noShelfTimeWindow),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "instant_gratification", "no_shelf_time"},
		},
		{
			name: "eight days gives instant gratification only",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(8 * 24 * time.Hour),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood", "instant_gratification"},
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
				IsOldestOwned: true, CreatedAtRank: 1,
			}},
			want: []string{"first_blood", "cleanup_crew", "dusty_relic", "archaeologist",
				"lost_civilization", "ancient_artifact", "the_ancient_one", "fossil_record"},
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
			name: "dropping after five honest hours folds 'em",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:        models.StatusDropped,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 300,
			}},
			want: []string{"know_when_to_fold"},
		},
		{
			name: "one minute short of five hours is not a fold",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:        models.StatusDropped,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 299,
			}},
			want: nil,
		},
		{
			name: "dropping under a tenth of the main estimate cuts losses",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:         models.StatusDropped,
				CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:             time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes:  599,
				TimeToBeatMain: i64(100 * 3600),
			}},
			want: []string{"know_when_to_fold", "cut_your_losses"},
		},
		{
			name: "exactly a tenth logged is not cutting losses",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:         models.StatusDropped,
				CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:             time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes:  600,
				TimeToBeatMain: i64(100 * 3600),
			}},
			want: []string{"know_when_to_fold"},
		},
		{
			name: "unknown estimate never cuts losses",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:    models.StatusDropped,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: nil,
		},
		{
			name: "dropping five games is the breakup",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:       models.StatusDropped,
				CreatedAt:    time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
				At:           time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				DroppedCount: 5,
			}},
			want: []string{"wasnt_you_it_was_me"},
		},
		{
			name: "dropping ten games brings the reaper",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:       models.StatusDropped,
				CreatedAt:    time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
				At:           time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				DroppedCount: 10,
			}},
			want: []string{"wasnt_you_it_was_me", "the_reaper"},
		},
		{
			name: "dropping exactly seven days after adding is remorse",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:    models.StatusDropped,
				CreatedAt: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: []string{"buyers_remorse"},
		},
		{
			name: "dropping eight days in is past remorse",
			event: Event{Kind: EventDropped, Entry: Entry{
				Status:    models.StatusDropped,
				CreatedAt: time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
				At:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: nil,
		},
		{
			name: "resuming a dropped game is a resurrection",
			event: Event{Kind: EventResumed, Entry: Entry{
				Status: models.StatusPlaying,
				At:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			}},
			want: []string{"resurrection"},
		},
		{
			name: "resuming six months to the day after the drop",
			event: Event{Kind: EventResumed, Entry: Entry{
				Status: models.StatusBacklog,
				At:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-secondChanceWindow),
					ResumedAt: ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				}},
			}},
			want: []string{"resurrection", "second_chance"},
		},
		{
			name: "one day short of six months is only a resurrection",
			event: Event{Kind: EventResumed, Entry: Entry{
				Status: models.StatusPlaying,
				At:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-secondChanceWindow + 24*time.Hour),
					ResumedAt: ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				}},
			}},
			want: []string{"resurrection"},
		},
		{
			name: "finishing a previously dropped game never gives up",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
					ResumedAt: ptrTime(time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)),
				}},
			}},
			want: []string{"first_blood", "never_give_up"},
		},
		{
			name: "finishing a year after the drop beats the odds",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour),
					ResumedAt: ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				}},
			}},
			want: []string{"first_blood", "never_give_up", "against_all_odds"},
		},
		{
			name: "returning two years after the drop rises like a phoenix",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-phoenixWindow),
					ResumedAt: ptrTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				}},
			}},
			want: []string{"first_blood", "dusty_relic", "never_give_up", "against_all_odds", "phoenix"},
		},
		{
			name: "finishing directly from dropped counts the finish as the return",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				DropHistory: []DropCycle{{
					DroppedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Add(-phoenixWindow),
				}},
			}},
			want: []string{"first_blood", "dusty_relic", "never_give_up", "against_all_odds", "phoenix"},
		},
		{
			name: "a finish without any drop history earns no comeback",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
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
		{
			name: "the third finish of a month is a hat trick",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				MonthFinishes: 3, FinishYear: 2026, FinishMonth: 6,
			}},
			want: []string{"first_blood", "hat_trick"},
		},
		{
			name: "the second finish of a month is not a hat trick",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				MonthFinishes: 2, FinishYear: 2026, FinishMonth: 6,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the fifth finish of a month cleans it up",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				MonthFinishes: 5, FinishYear: 2026, FinishMonth: 6,
			}},
			want: []string{"first_blood", "cleanup_crew", "hat_trick", "cleanup_month"},
		},
		{
			name: "a third consecutive month runs the backlog machine",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				FinishStreak: 3, FinishYear: 2026, FinishMonth: 6,
			}},
			want: []string{"first_blood", "backlog_machine"},
		},
		{
			name: "two consecutive months stall the machine",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				FinishStreak: 2, FinishYear: 2026, FinishMonth: 6,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "twelve distinct months crown the perfect season",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 12,
				YearMonthsFinished: 12, FinishYear: 2026, FinishMonth: 12,
			}},
			want: []string{"first_blood", "cleanup_crew", "spring_cleaning", "perfect_season", "strong_finish"},
		},
		{
			name: "eleven months of the year is not a perfect season",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 12,
				YearMonthsFinished: 11, FinishYear: 2026, FinishMonth: 12,
			}},
			want: []string{"first_blood", "cleanup_crew", "spring_cleaning", "strong_finish"},
		},
		{
			name: "a January finish opens the season",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				FinishYear: 2026, FinishMonth: 1,
			}},
			want: []string{"first_blood", "season_opener"},
		},
		{
			name: "a February finish is not the season opener",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				FinishYear: 2026, FinishMonth: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "a December finish is a strong one",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				FinishYear: 2026, FinishMonth: 12,
			}},
			want: []string{"first_blood", "strong_finish"},
		},
		{
			name: "the fifth summer finish cleans the summer",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				SummerFinishes: 5, FinishYear: 2026, FinishMonth: 7,
			}},
			want: []string{"first_blood", "cleanup_crew", "summer_cleanup"},
		},
		{
			name: "four summer finishes are not enough",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				SummerFinishes: 4, FinishYear: 2026, FinishMonth: 7,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "a September finish is outside the summer bucket",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				SummerFinishes: 5, FinishYear: 2026, FinishMonth: 9,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "finishing the queue top is next",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				WasQueueTop: true,
			}},
			want: []string{"first_blood", "next_up"},
		},
		{
			name: "finishing from mid-queue is not next",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				WasQueueTop: false,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "a 100h game at the boundary is an ultra marathon",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				TimeToBeatMain: i64(100 * 3600),
			}},
			want: []string{"first_blood", "long_haul", "ultra_marathon"},
		},
		{
			name: "one second under 100h is a long haul only",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				TimeToBeatMain: i64(100*3600 - 1),
			}},
			want: []string{"first_blood", "long_haul"},
		},
		{
			name: "the third 50h+ finish is the commitment",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				TimeToBeatMain:   i64(51 * 3600),
				LongHaulFinishes: 3,
			}},
			want: []string{"first_blood", "long_haul", "the_commitment"},
		},
		{
			name: "the second 50h+ finish is not yet the commitment",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				TimeToBeatMain:   i64(51 * 3600),
				LongHaulFinishes: 2,
			}},
			want: []string{"first_blood", "long_haul"},
		},
		{
			name: "the third finish of a series completes the trilogy",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 4, Played: 3, Unplayed: 1, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood", "trilogy"},
		},
		{
			name: "the second finish of a series is not a trilogy",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 4, Played: 2, Unplayed: 2, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the fifth finish of a series is franchise mode",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 6, Played: 5, Unplayed: 1, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood", "cleanup_crew", "trilogy", "saga", "franchise_mode"},
		},
		{
			name: "finishing inside a five-entry saga counts as one",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 5, Played: 1, Unplayed: 4, YearPlayed: 1},
				},
			}},
			want: []string{"first_blood", "saga"},
		},
		{
			name: "a four-entry series is not yet a saga",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 4, Played: 1, Unplayed: 3, YearPlayed: 1},
				},
			}},
			want: []string{"first_blood"},
		},
		{
			name: "two series finishes in a row are back to back",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 3, Played: 2, Unplayed: 1, YearPlayed: 2},
				},
				PrevFinishSharesSeries: true,
			}},
			want: []string{"first_blood", "back_to_back"},
		},
		{
			name: "a foreign finish between series games breaks the run",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 3, Played: 2, Unplayed: 1, YearPlayed: 2},
				},
				PrevFinishSharesSeries: false,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "three series finishes in one calendar year run the marathon",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 4, Played: 3, Unplayed: 1, YearPlayed: 3},
				},
			}},
			want: []string{"first_blood", "trilogy", "marathon_series"},
		},
		{
			name: "three series finishes split across years do not run the marathon",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 4, Played: 3, Unplayed: 1, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood", "trilogy"},
		},
		{
			name: "emptying a series with a dropped member closes the loop but is no full set",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 3, Played: 2, Unplayed: 0, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood", "closing_the_loop"},
		},
		{
			name: "finishing a series' lone owned game does not close any loop",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 1, Played: 1, Unplayed: 0, YearPlayed: 1},
				},
			}},
			want: []string{"first_blood"},
		},
		{
			name: "playing every owned member of a series is the full set",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				SeriesStandings: map[string]SeriesStanding{
					"sr1": {Owned: 3, Played: 3, Unplayed: 0, YearPlayed: 2},
				},
			}},
			want: []string{"first_blood", "trilogy", "closing_the_loop", "full_set"},
		},
		{
			name: "the fifth distinct genre of the year is the sampler",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				YearGenres: 5,
			}},
			want: []string{"first_blood", "cleanup_crew", "sampler"},
		},
		{
			name: "four genres in the year is not the sampler",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				YearGenres: 4,
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "the fifth distinct platform is the world tour",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				DistinctPlatforms: 5, PlatformID: i64(130),
			}},
			want: []string{"first_blood", "cleanup_crew", "world_tour"},
		},
		{
			name: "four platforms is not the world tour",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				DistinctPlatforms: 4, PlatformID: i64(130),
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "finishing on a dormant platform is retroactive",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PlatformID: i64(18), PlatformDormant: boolPtr(true),
			}},
			want: []string{"first_blood", "retroactive"},
		},
		{
			name: "an active platform is not retroactive",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				PlatformID: i64(18), PlatformDormant: boolPtr(false),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "a finish with no platform set is never retroactive",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
			}},
			want: []string{"first_blood"},
		},
		{
			name: "five console generations bridge the generation gap",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				DistinctGenerations: 5, PlatformID: i64(18),
			}},
			want: []string{"first_blood", "cleanup_crew", "generation_gap"},
		},
		{
			name: "four generations leave the gap uncrossed",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				DistinctGenerations: 4, PlatformID: i64(18),
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "the fifth Nintendo console runs the time machine",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				NintendoConsoles: 5, PlatformID: i64(5),
			}},
			want: []string{"first_blood", "cleanup_crew", "nintendo_time_machine"},
		},
		{
			name: "four Nintendo consoles stall the time machine",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				NintendoConsoles: 4, PlatformID: i64(5),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the seventh Big N console completes the set",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 7,
				BigNConsoles: 7, PlatformID: i64(130),
			}},
			want: []string{"first_blood", "cleanup_crew", "the_big_n"},
		},
		{
			name: "six of seven is not The Big N",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 6,
				BigNConsoles: 6, PlatformID: i64(41),
			}},
			want: []string{"first_blood", "cleanup_crew"},
		},
		{
			name: "the third Game Boy system completes the generation",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				GameBoySystems: 3, PlatformID: i64(24),
			}},
			want: []string{"first_blood", "game_boy_generation"},
		},
		{
			name: "two Game Boy systems leave the generation short",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 2,
				GameBoySystems: 2, PlatformID: i64(22),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the fourth handheld generation crowns the historian",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				HandheldGenerations: 4, PlatformID: i64(20),
			}},
			want: []string{"first_blood", "handheld_historian"},
		},
		{
			name: "three handheld generations are not history yet",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				HandheldGenerations: 3, PlatformID: i64(20),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the fifth station completes the pilgrimage",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 5,
				PilgrimConsoles: 5, PlatformID: i64(167),
			}},
			want: []string{"first_blood", "cleanup_crew", "playstation_pilgrim"},
		},
		{
			name: "four stations leave the pilgrimage unfinished",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				PilgrimConsoles: 4, PlatformID: i64(48),
			}},
			want: []string{"first_blood"},
		},
		{
			name: "the fourth Xbox generation stays green across the ages",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 4,
				XboxGenerations: 4, PlatformID: i64(169),
			}},
			want: []string{"first_blood", "green_across_the_ages"},
		},
		{
			name: "three Xbox generations are not green enough",
			event: Event{Kind: EventFinished, Entry: Entry{
				Status:        models.StatusPlayed,
				CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				At:            time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LoggedMinutes: 600, PlayedCount: 3,
				XboxGenerations: 3, PlatformID: i64(49),
			}},
			want: []string{"first_blood"},
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

// TestPlatformMasterySets pins the curated sets and thresholds against
// the platform catalog: the sizes the predicates compare must match the
// hardware the metadata package actually classifies, and every set member
// must sit in the family the predicate's family-based cousins read.
func TestPlatformMasterySets(t *testing.T) {
	if bigNConsoles != len(metadata.BigNPlatformIDs) {
		t.Errorf("bigNConsoles = %d, want the %d curated consoles",
			bigNConsoles, len(metadata.BigNPlatformIDs))
	}
	if pilgrimConsoles != len(metadata.PilgrimPlatformIDs) {
		t.Errorf("pilgrimConsoles = %d, want the %d curated consoles",
			pilgrimConsoles, len(metadata.PilgrimPlatformIDs))
	}
	if gameBoySystems != metadata.FamilySize(metadata.FamilyGameBoy) {
		t.Errorf("gameBoySystems = %d, want the family's %d systems",
			gameBoySystems, metadata.FamilySize(metadata.FamilyGameBoy))
	}
	if n := metadata.FamilySize(metadata.FamilyGameBoy); n != 3 {
		t.Errorf("game boy family has %d systems, want exactly GB/GBC/GBA", n)
	}
	for _, id := range metadata.BigNPlatformIDs {
		meta, ok := metadata.PlatformCatalog[id]
		if !ok || meta.Family != metadata.FamilyNintendoConsole {
			t.Errorf("Big N member %d is not a classified Nintendo console", id)
		}
	}
	for _, id := range metadata.PilgrimPlatformIDs {
		meta, ok := metadata.PlatformCatalog[id]
		if !ok || meta.Family != metadata.FamilyPlayStation || meta.Handheld {
			t.Errorf("pilgrim station %d is not a PlayStation home console", id)
		}
	}
	// The handheld and Xbox generation runs must be reachable from what
	// the catalog classifies, and the generation gap's five generations
	// must exist across families.
	handheldGens, xboxGens, allGens := map[int]bool{}, map[int]bool{}, map[int]bool{}
	for _, meta := range metadata.PlatformCatalog {
		if meta.Generation <= 0 {
			continue
		}
		allGens[meta.Generation] = true
		if meta.Handheld {
			handheldGens[meta.Generation] = true
		}
		if meta.Family == metadata.FamilyXbox {
			xboxGens[meta.Generation] = true
		}
	}
	if len(handheldGens) < handheldHistorianGenerations {
		t.Errorf("catalog covers %d handheld generations, need %d",
			len(handheldGens), handheldHistorianGenerations)
	}
	if len(xboxGens) != xboxGenerations {
		t.Errorf("catalog covers %d Xbox generations, want exactly %d",
			len(xboxGens), xboxGenerations)
	}
	if len(allGens) < generationGapGenerations {
		t.Errorf("catalog covers %d generations, need %d", len(allGens), generationGapGenerations)
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
	if !HasTimePredicates() {
		t.Error("HasTimePredicates should be true now that lazy predicates ship")
	}
}

// defByID returns the catalogue definition (predicate included) for an id.
func defByID(id string) *Definition {
	for i := range Catalogue {
		if Catalogue[i].Achievement.ID == id {
			return &Catalogue[i]
		}
	}
	return nil
}

// TestNoAcquisitionWindows pins the lazy 30/90-day boundaries: the window
// is measured from the last non-wishlist add, inclusively at the boundary.
func TestNoAcquisitionWindows(t *testing.T) {
	restraint := defByID("restraint")
	discipline := defByID("discipline")
	if restraint == nil || discipline == nil {
		t.Fatalf("restraint/discipline missing from catalogue")
	}
	if restraint.Predicate != nil || discipline.Predicate != nil {
		t.Error("lazy achievements must not carry an event predicate")
	}

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		gap            time.Duration
		wantRestraint  bool
		wantDiscipline bool
	}{
		{"twenty-nine days and change", restraintWindow - time.Hour, false, false},
		{"exactly thirty days", restraintWindow, true, false},
		{"eighty-nine days and change", disciplineWindow - time.Hour, true, false},
		{"exactly ninety days", disciplineWindow, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := TimeSnapshot{Now: now, LastAcquiredAt: now.Add(-tt.gap)}
			if got := restraint.TimePredicate(snap); got != tt.wantRestraint {
				t.Errorf("restraint = %v, want %v", got, tt.wantRestraint)
			}
			if got := discipline.TimePredicate(snap); got != tt.wantDiscipline {
				t.Errorf("discipline = %v, want %v", got, tt.wantDiscipline)
			}
		})
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
	// A bare resume is the comeback ladder's first rung.
	if ids := unlockIDs(resumed); len(ids) != 1 || ids[0] != "resurrection" {
		t.Errorf("resumed event unlocked %v, want [resurrection]", ids)
	}
}

func TestCatalogueEggInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range Catalogue {
		if seen[def.Achievement.ID] {
			t.Errorf("duplicate catalogue id %q", def.Achievement.ID)
		}
		seen[def.Achievement.ID] = true

		if !def.Achievement.Egg {
			continue
		}
		// An egg implies hidden: the reveal must never sit on the wall.
		if !def.Achievement.Hidden {
			t.Errorf("%s: egg without hidden", def.Achievement.ID)
		}
		// Predicates never fire for eggs — no event, no time window.
		if def.Predicate != nil {
			t.Errorf("%s: egg carries an event predicate", def.Achievement.ID)
		}
		if def.TimePredicate != nil {
			t.Errorf("%s: egg carries a time predicate", def.Achievement.ID)
		}
	}

	for _, id := range []string{"night_owl", "hog_watcher", "konami", "queue_shuffler"} {
		if !IsEgg(id) {
			t.Errorf("IsEgg(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"first_blood", "the_big_n", "nope"} {
		if IsEgg(id) {
			t.Errorf("IsEgg(%q) = true, want false", id)
		}
	}
}

func TestCatalogueHiddenSet(t *testing.T) {
	// The curated hidden set: surprise-flavored achievements plus the eggs.
	// Keep it small enough that the visible wall still reads rich.
	want := map[string]bool{
		"empty_the_closet": true, "backlog_negative": true, "perfect_season": true,
		"closing_the_loop": true, "the_big_n": true, "fossil_record": true,
		"phoenix": true, "buyers_remorse": true, "know_when_to_fold": true,
		"cut_your_losses": true,
		"breaking_even":   true,
		"night_owl":       true, "hog_watcher": true, "konami": true, "queue_shuffler": true,
	}
	hidden := 0
	for _, def := range Catalogue {
		if !def.Achievement.Hidden {
			continue
		}
		hidden++
		if !want[def.Achievement.ID] {
			t.Errorf("unexpected hidden achievement %q", def.Achievement.ID)
		}
	}
	if hidden != len(want) {
		t.Errorf("hidden count = %d, want %d", hidden, len(want))
	}
}

func TestMaskedHintTeases(t *testing.T) {
	// Every curated hidden entry ships teasing copy — the tease is the fun.
	for _, def := range Catalogue {
		if !def.Achievement.Hidden {
			continue
		}
		if got := MaskedHint(def.Achievement.ID); got == MaskedDescription {
			t.Errorf("%s: no teasing copy, falls back to the placeholder", def.Achievement.ID)
		}
	}

	locked := models.Achievement{ID: "konami", Title: "Old Habits", Description: "Enter the Konami code.", Hidden: true, Egg: true}
	served := Present(locked, true)
	if served.Title != MaskedTitle || served.Icon != MaskedIcon {
		t.Errorf("masked title/icon = %q/%q, want %q/%q", served.Title, served.Icon, MaskedTitle, MaskedIcon)
	}
	if served.Description != maskedHints["konami"] {
		t.Errorf("masked description = %q, want the tease %q", served.Description, maskedHints["konami"])
	}
	if served.Egg != true || served.Hidden != true {
		t.Errorf("masking dropped hidden/egg flags: %v/%v", served.Hidden, served.Egg)
	}
	if locked.Title != "Old Habits" {
		t.Errorf("Present mutated its input: title = %q", locked.Title)
	}

	// An unlisted hidden id falls back to the generic placeholder.
	unknown := models.Achievement{ID: "mystery", Title: "??", Hidden: true}
	if got := Present(unknown, true).Description; got != MaskedDescription {
		t.Errorf("fallback description = %q, want %q", got, MaskedDescription)
	}
}

// TestAchievementsDocSync pins docs/ACHIEVEMENTS.md to the catalogue: every
// entry appears exactly once with its id, title, tier, hidden and egg flags,
// and unlock text in step with the Go definitions. The doc is
// hand-maintained; this test is what keeps it honest.
func TestAchievementsDocSync(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "ACHIEVEMENTS.md"))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}

	parseBool := func(field string) bool {
		if field == "true" {
			return true
		}
		if field == "false" {
			return false
		}
		t.Fatalf("doc boolean column = %q, want true/false", field)
		return false
	}

	rows := map[string]struct {
		title, tier, unlock string
		hidden, egg         bool
	}{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cols := strings.Split(strings.TrimSpace(line), "|")
		if len(cols) != 8 {
			t.Fatalf("doc row has %d columns, want 7: %q", len(cols)-1, line)
		}
		id := strings.Trim(strings.TrimSpace(cols[1]), "`")
		rows[id] = struct {
			title, tier, unlock string
			hidden, egg         bool
		}{
			title:  strings.TrimSpace(cols[2]),
			tier:   strings.TrimSpace(cols[3]),
			hidden: parseBool(strings.TrimSpace(cols[4])),
			egg:    parseBool(strings.TrimSpace(cols[5])),
			unlock: strings.TrimSpace(cols[6]),
		}
	}

	if len(rows) != len(Catalogue) {
		t.Errorf("doc has %d rows, catalogue has %d — extra or missing entries", len(rows), len(Catalogue))
	}
	for _, def := range Catalogue {
		row, ok := rows[def.Achievement.ID]
		if !ok {
			t.Errorf("%s: no doc row — add it to docs/ACHIEVEMENTS.md", def.Achievement.ID)
			continue
		}
		if row.title != def.Achievement.Title || row.tier != def.Achievement.Tier ||
			row.hidden != def.Achievement.Hidden || row.egg != def.Achievement.Egg ||
			row.unlock != def.Achievement.Description {
			t.Errorf("%s: doc row = %q/%q/%v/%v/%q, want %q/%q/%v/%v/%q",
				def.Achievement.ID, row.title, row.tier, row.hidden, row.egg, row.unlock,
				def.Achievement.Title, def.Achievement.Tier, def.Achievement.Hidden,
				def.Achievement.Egg, def.Achievement.Description)
		}
	}
}
