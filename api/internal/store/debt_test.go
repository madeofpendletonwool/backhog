package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

func datePtr(s string) *string { return &s }

func TestHoursPerWeek(t *testing.T) {
	tests := []struct {
		name                string
		minutes, windowDays float64
		want                float64
	}{
		{"90d window", 600, 90, 600 / 60 / (90.0 / 7)}, // 10h over 90 days
		{"one exact week", 420, 7, 7},                  // 7h in a week
		{"zero minutes", 0, 90, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hoursPerWeek(tt.minutes, tt.windowDays); got != tt.want {
				t.Errorf("hoursPerWeek(%v, %v) = %v, want %v", tt.minutes, tt.windowDays, got, tt.want)
			}
		})
	}
}

func TestComputePace(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	first := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

	tests := []struct {
		name                        string
		recentMinutes, totalMinutes float64
		firstPlayedOn               sql.NullString
		want90d, wantAll            *float64
	}{
		{
			name:    "no sessions at all",
			want90d: nil, wantAll: nil,
		},
		{
			name: "recent sessions only, first today",
			// 10h over the trailing 90 days; all-time floored at one week.
			recentMinutes: 600, totalMinutes: 600,
			firstPlayedOn: first("2026-08-25"),
			want90d:       ptr(0.8), wantAll: ptr(10),
		},
		{
			name:          "old sessions only, nothing recent",
			recentMinutes: 0, totalMinutes: 1200,
			firstPlayedOn: first("2026-01-27"), // 210 days before now
			want90d:       nil,
			wantAll:       ptr(0.7), // 20h / 30 weeks
		},
		{
			name:          "mixed windows",
			recentMinutes: 900, totalMinutes: 3000,
			firstPlayedOn: first("2026-02-25"), // 181 days before now
			want90d:       ptr(1.2),            // 15h / (90/7) weeks
			wantAll:       ptr(1.9),            // 50h / (181/7) weeks
		},
		{
			name:          "invalid first date is ignored for all-time",
			recentMinutes: 600, totalMinutes: 600,
			firstPlayedOn: first("not-a-date"),
			want90d:       ptr(0.8), wantAll: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computePace(tt.recentMinutes, tt.totalMinutes, tt.firstPlayedOn, now)

			if (got.HoursPerWeek90d == nil) != (tt.want90d == nil) {
				t.Fatalf("HoursPerWeek90d = %v, want %v", got.HoursPerWeek90d, tt.want90d)
			}
			if tt.want90d != nil && *got.HoursPerWeek90d != *tt.want90d {
				t.Errorf("HoursPerWeek90d = %v, want %v", *got.HoursPerWeek90d, *tt.want90d)
			}

			if (got.HoursPerWeekAll == nil) != (tt.wantAll == nil) {
				t.Fatalf("HoursPerWeekAll = %v, want %v", got.HoursPerWeekAll, tt.wantAll)
			}
			if tt.wantAll != nil && *got.HoursPerWeekAll != *tt.wantAll {
				t.Errorf("HoursPerWeekAll = %v, want %v", *got.HoursPerWeekAll, *tt.wantAll)
			}
		})
	}
}

func TestProjectClearance(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name                    string
		debtHours, hoursPerWeek float64
		wantWeeks               float64
		wantClearBy             *string
	}{
		{
			name:      "no debt is already clear",
			debtHours: 0, hoursPerWeek: 5,
			wantWeeks: 0, wantClearBy: datePtr("2026-08-25"),
		},
		{
			name:      "no pace never clears",
			debtHours: 100, hoursPerWeek: 0,
			wantWeeks: 0, wantClearBy: nil,
		},
		{
			name:      "exact whole weeks",
			debtHours: 100, hoursPerWeek: 10, // 10 weeks = 70 days
			wantWeeks: 10, wantClearBy: datePtr("2026-11-03"),
		},
		{
			name:      "partial week rounds the date up",
			debtHours: 102, hoursPerWeek: 10, // 10.2 weeks = 71.4 days -> 72
			wantWeeks: 10.2, wantClearBy: datePtr("2026-11-05"),
		},
		{
			name:      "slow pace spans years",
			debtHours: 780, hoursPerWeek: 5, // 156 weeks = 1092 days
			wantWeeks: 156, wantClearBy: datePtr("2029-08-21"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectClearance(tt.debtHours, tt.hoursPerWeek, now)

			if got.Weeks != tt.wantWeeks {
				t.Errorf("Weeks = %v, want %v", got.Weeks, tt.wantWeeks)
			}
			if (got.ClearBy == nil) != (tt.wantClearBy == nil) {
				t.Fatalf("ClearBy = %v, want %v", got.ClearBy, tt.wantClearBy)
			}
			if tt.wantClearBy != nil && *got.ClearBy != *tt.wantClearBy {
				t.Errorf("ClearBy = %v, want %v", *got.ClearBy, *tt.wantClearBy)
			}
		})
	}
}

func TestBuildProjection(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	t.Run("with a current pace", func(t *testing.T) {
		pace := models.Pace{HoursPerWeek90d: ptr(10.0), HoursPerWeekAll: ptr(8.0)}
		proj := buildProjection(100, pace, now)

		if proj.CurrentPace == nil {
			t.Fatal("CurrentPace should be set when there is a 90-day pace")
		}
		if proj.CurrentPace.Weeks != 10 {
			t.Errorf("CurrentPace.Weeks = %v, want 10", proj.CurrentPace.Weeks)
		}
		if len(proj.Scenarios) != 3 {
			t.Fatalf("got %d scenarios, want 3", len(proj.Scenarios))
		}
		wantRates := []float64{5, 10, 15}
		for i, want := range wantRates {
			if proj.Scenarios[i].HoursPerWeek != want {
				t.Errorf("scenario %d rate = %v, want %v", i, proj.Scenarios[i].HoursPerWeek, want)
			}
			if proj.Scenarios[i].ClearBy == nil {
				t.Errorf("scenario %d should have a clearance date at a fixed rate", i)
			}
		}
	})

	t.Run("zero pace drops the current-pace estimate", func(t *testing.T) {
		proj := buildProjection(100, models.Pace{}, now)
		if proj.CurrentPace != nil {
			t.Errorf("CurrentPace = %v, want nil with no pace", proj.CurrentPace)
		}
		if len(proj.Scenarios) != 3 {
			t.Fatalf("got %d scenarios, want 3", len(proj.Scenarios))
		}
	})

	t.Run("no debt still projects the fixed scenarios", func(t *testing.T) {
		pace := models.Pace{HoursPerWeek90d: ptr(3.0)}
		proj := buildProjection(0, pace, now)
		if proj.CurrentPace == nil || proj.CurrentPace.Weeks != 0 {
			t.Errorf("CurrentPace = %+v, want an already-clear zero weeks", proj.CurrentPace)
		}
		for _, sc := range proj.Scenarios {
			if sc.Weeks != 0 || sc.ClearBy == nil {
				t.Errorf("scenario %+v should be clear today", sc)
			}
		}
	})
}
