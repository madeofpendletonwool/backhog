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

func ptrTime(t time.Time) *time.Time { return &t }

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
