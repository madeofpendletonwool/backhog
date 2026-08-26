package store

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Short games — the "quick wins" slice of the debt — are anything under 8h.
const shortGameSeconds = 8 * 3600

// The trailing window "current pace" is averaged over.
const paceWindowDays = 90

// Debt returns the backlog-debt report: unplayed hours split by where they
// sit, play pace from logged sessions, and clearance projections.
func (s *Store) Debt(ctx context.Context, userID string) (models.DebtReport, error) {
	// time_to_beat is stored in seconds. Started games count only their
	// remaining time: the estimate minus what the user already logged, floored
	// at zero per entry. Games with no estimate (NULL) contribute nothing —
	// SUM skips the NULLs the scalar MAX produces for them.
	var mainSeconds, startedSeconds, shortSeconds float64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN e.status = 'backlog' THEN g.time_to_beat_main END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'playing' THEN
				MAX(g.time_to_beat_main - 60 * COALESCE(
					(SELECT SUM(ps.minutes) FROM play_sessions ps WHERE ps.entry_id = e.id), 0), 0)
			END), 0),
			COALESCE(SUM(CASE WHEN e.status IN ('backlog','playing')
			                  AND g.time_to_beat_main < ? THEN g.time_to_beat_main END), 0)
		FROM library_entries e JOIN games g ON g.id = e.game_id
		WHERE e.user_id = ?`, shortGameSeconds, userID).
		Scan(&mainSeconds, &startedSeconds, &shortSeconds)
	if err != nil {
		return models.DebtReport{}, err
	}

	var totalMinutes, recentMinutes float64
	var firstPlayedOn sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(minutes), 0),
			COALESCE(SUM(CASE WHEN played_on >= date('now', '-90 days') THEN minutes END), 0),
			date(MIN(played_on))
		FROM play_sessions
		WHERE user_id = ?`, userID).
		Scan(&totalMinutes, &recentMinutes, &firstPlayedOn)
	if err != nil {
		return models.DebtReport{}, err
	}

	report := models.DebtReport{
		MainBacklogHours: round1(mainSeconds / 3600),
		StartedHours:     round1(startedSeconds / 3600),
		ShortGamesHours:  round1(shortSeconds / 3600),
	}
	report.TotalHours = round1((mainSeconds + startedSeconds) / 3600)
	report.Pace = computePace(recentMinutes, totalMinutes, firstPlayedOn, time.Now().UTC())
	report.Projection = buildProjection(report.TotalHours, report.Pace, time.Now().UTC())
	return report, nil
}

// hoursPerWeek converts minutes logged over windowDays days into an hourly
// weekly rate.
func hoursPerWeek(minutes, windowDays float64) float64 {
	return minutes / 60 / (windowDays / 7)
}

// computePace turns raw session sums into weekly rates. The 90-day figure is a
// fixed trailing average; all-time spans first session to now, floored at one
// week so a brand-new account's first binge doesn't inflate to absurd rates.
func computePace(recentMinutes, totalMinutes float64, firstPlayedOn sql.NullString, now time.Time) models.Pace {
	var pace models.Pace
	if recentMinutes > 0 {
		p := round1(hoursPerWeek(recentMinutes, paceWindowDays))
		pace.HoursPerWeek90d = &p
	}
	if firstPlayedOn.Valid && totalMinutes > 0 {
		if first, err := time.Parse("2006-01-02", firstPlayedOn.String); err == nil {
			days := now.Sub(first).Hours() / 24
			if days < 7 {
				days = 7
			}
			p := round1(hoursPerWeek(totalMinutes, days))
			pace.HoursPerWeekAll = &p
		}
	}
	return pace
}

// buildProjection pairs the current-pace estimate (when there is a pace) with
// the fixed 5/10/15 hrs/week scenarios.
func buildProjection(totalHours float64, pace models.Pace, now time.Time) models.DebtProjection {
	proj := models.DebtProjection{Scenarios: make([]models.ClearanceScenario, 0, 3)}
	if pace.HoursPerWeek90d != nil && *pace.HoursPerWeek90d > 0 {
		scenario := projectClearance(totalHours, *pace.HoursPerWeek90d, now)
		proj.CurrentPace = &scenario
	}
	for _, rate := range []float64{5, 10, 15} {
		proj.Scenarios = append(proj.Scenarios, projectClearance(totalHours, rate, now))
	}
	return proj
}

// projectClearance works out when debtHours clears at a fixed weekly rate.
// No debt is already clear; no pace never clears (clearBy stays null).
func projectClearance(debtHours, hoursPerWeek float64, now time.Time) models.ClearanceScenario {
	scenario := models.ClearanceScenario{HoursPerWeek: hoursPerWeek}
	if debtHours <= 0 {
		scenario.Weeks = 0
		clearBy := now.Format("2006-01-02")
		scenario.ClearBy = &clearBy
		return scenario
	}
	if hoursPerWeek <= 0 {
		return scenario
	}
	weeks := debtHours / hoursPerWeek
	scenario.Weeks = round1(weeks)
	// Round the day up: the backlog isn't clear until the final partial week
	// has actually been played.
	clearBy := now.AddDate(0, 0, int(math.Ceil(weeks*7))).Format("2006-01-02")
	scenario.ClearBy = &clearBy
	return scenario
}
