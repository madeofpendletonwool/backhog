package store

import (
	"strings"
	"testing"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

var tonightNow = time.Date(2026, 8, 25, 20, 0, 0, 0, time.UTC)

// cand builds a scorer candidate with the fields the heuristics read. Fixtures
// set the same raw signals the DB has (estimate, logged minutes); remaining is
// derived the way the loader derives it.
func cand(name, status string, mutators ...func(*tonightCandidate)) tonightCandidate {
	c := tonightCandidate{entry: models.Entry{ID: name, Status: status, Game: models.Game{Name: name}}}
	for _, m := range mutators {
		m(&c)
	}
	if c.entry.Game.TimeToBeatMain != nil && c.remaining == nil {
		remaining := float64(*c.entry.Game.TimeToBeatMain)/3600 - float64(c.entry.LoggedMinutes)/60
		if remaining < 0 {
			remaining = 0
		}
		c.remaining = &remaining
	}
	return c
}

func withEstimate(hours float64) func(*tonightCandidate) {
	return func(c *tonightCandidate) {
		sec := int64(hours * 3600)
		c.entry.Game.TimeToBeatMain = &sec
	}
}

func withIGDB(rating float64) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.entry.Game.IGDBRating = &rating }
}

func withUserRating(rating int) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.entry.UserRating = &rating }
}

func withLogged(minutes int) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.entry.LoggedMinutes = minutes }
}

func withOwned(days float64) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.ownedDays = days }
}

func withLastPlayed(daysAgo float64) func(*tonightCandidate) {
	return func(c *tonightCandidate) {
		t := tonightNow.AddDate(0, 0, -int(daysAgo))
		c.lastPlayed = &t
	}
}

func withQueueRank(rank int) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.queueRank = rank }
}

func withBurnout(share float64) func(*tonightCandidate) {
	return func(c *tonightCandidate) { c.burnoutShare = share }
}

// TestTonightFixtureLibrary runs the scorer over one fixture library and checks
// every category's winner and the human reason each pick carries.
func TestTonightFixtureLibrary(t *testing.T) {
	library := []tonightCandidate{
		// The in-progress epic: 14h left, touched 3 days ago.
		cand("Elden Ring", models.StatusPlaying,
			withEstimate(60), withLogged(46*60), withLastPlayed(3), withOwned(30)),
		// Fits a 2-hour budget, top of the queue, well reviewed.
		cand("Untitled Goose Game", models.StatusBacklog,
			withEstimate(1.5), withIGDB(80), withQueueRank(1), withOwned(60)),
		// Best reviewed thing in the library, but far too long for tonight.
		cand("Disco Elysium", models.StatusBacklog,
			withEstimate(40), withIGDB(92), withQueueRank(15), withOwned(200)),
		// The guilt purchase: six years owned, three hours logged.
		cand("Skyrim", models.StatusBacklog,
			withEstimate(60), withLogged(3*60), withOwned(6*365.25)),
		// Recently added and untouched; must not win anything here.
		cand("Hollow Knight", models.StatusBacklog,
			withEstimate(40), withIGDB(90), withOwned(30), withQueueRank(2)),
		// Wrong statuses: never eligible.
		cand("Portal", models.StatusPlayed, withEstimate(3), withIGDB(90)),
		cand("Hades II", models.StatusWishlist, withEstimate(20), withIGDB(95)),
	}

	picks := selectTonightPicks(library, 120, nil, tonightNow)

	if picks.Continue == nil || picks.Continue.Entry.Game.Name != "Elden Ring" {
		t.Fatalf("Continue = %+v, want Elden Ring", picks.Continue)
	}
	if want := "14h remaining · last played 3 days ago"; picks.Continue.Reason != want {
		t.Errorf("Continue.Reason = %q, want %q", picks.Continue.Reason, want)
	}

	if picks.ShortWin == nil || picks.ShortWin.Entry.Game.Name != "Untitled Goose Game" {
		t.Fatalf("ShortWin = %+v, want Untitled Goose Game", picks.ShortWin)
	}
	if want := "~1h 30m to beat · #1 in your queue · 80 on IGDB"; picks.ShortWin.Reason != want {
		t.Errorf("ShortWin.Reason = %q, want %q", picks.ShortWin.Reason, want)
	}

	// Disco Elysium outranks Hollow Knight on both ratings; the buried bonus
	// (rank > 10) keeps it ahead too.
	if picks.Wildcard == nil || picks.Wildcard.Entry.Game.Name != "Disco Elysium" {
		t.Fatalf("Wildcard = %+v, want Disco Elysium", picks.Wildcard)
	}
	if want := "You've never played it · 92 on IGDB"; picks.Wildcard.Reason != want {
		t.Errorf("Wildcard.Reason = %q, want %q", picks.Wildcard.Reason, want)
	}

	if picks.Rescue == nil || picks.Rescue.Entry.Game.Name != "Skyrim" {
		t.Fatalf("Rescue = %+v, want Skyrim", picks.Rescue)
	}
	if want := "You've owned it for 6 years and have 3 hours logged"; picks.Rescue.Reason != want {
		t.Errorf("Rescue.Reason = %q, want %q", picks.Rescue.Reason, want)
	}
}

// TestShortWinMustFitBudget: a game longer than the budget can never win the
// short win, no matter how well it reviews or where it sits in the queue.
func TestShortWinMustFitBudget(t *testing.T) {
	library := []tonightCandidate{
		cand("Too Long", models.StatusBacklog, withEstimate(4), withIGDB(95), withQueueRank(1)),
		cand("Fits", models.StatusBacklog, withEstimate(1), withIGDB(70), withQueueRank(9)),
		// No estimate at all: an unknown length can't satisfy "something short".
		cand("Unknown", models.StatusBacklog, withIGDB(95), withQueueRank(1)),
	}

	picks := selectTonightPicks(library, 90, nil, tonightNow)
	if picks.ShortWin == nil || picks.ShortWin.Entry.Game.Name != "Fits" {
		t.Fatalf("ShortWin = %+v, want Fits", picks.ShortWin)
	}
}

// TestGenreBurnoutDownWeights: two otherwise equal candidates, one from the
// genre that ate the last two weeks — the fresh one wins.
func TestGenreBurnoutDownWeights(t *testing.T) {
	same := func(name string, burnt float64) tonightCandidate {
		return cand(name, models.StatusBacklog,
			withEstimate(1.5), withIGDB(90), withQueueRank(1), withBurnout(burnt))
	}
	library := []tonightCandidate{same("Burnt Genre Game", 0.8), same("Fresh Genre Game", 0)}

	picks := selectTonightPicks(library, 120, nil, tonightNow)
	if picks.ShortWin == nil || picks.ShortWin.Entry.Game.Name != "Fresh Genre Game" {
		t.Fatalf("ShortWin = %+v, want Fresh Genre Game", picks.ShortWin)
	}

	// Same story for the wildcard.
	wild := []tonightCandidate{
		cand("Burnt Wild", models.StatusBacklog, withIGDB(90), withBurnout(0.8)),
		cand("Fresh Wild", models.StatusBacklog, withIGDB(90)),
	}
	picks = selectTonightPicks(wild, 120, nil, tonightNow)
	if picks.Wildcard == nil || picks.Wildcard.Entry.Game.Name != "Fresh Wild" {
		t.Fatalf("Wildcard = %+v, want Fresh Wild", picks.Wildcard)
	}
}

// TestContinuePrefersFinishableAndFresh: a game that fits the budget beats a
// fresher one that doesn't, and the reason says it can be finished tonight.
func TestContinuePrefersFinishableAndFresh(t *testing.T) {
	library := []tonightCandidate{
		cand("Fits And Stale", models.StatusPlaying, withEstimate(50), withLogged(49*60), withLastPlayed(5)),
		cand("Fresh But Long", models.StatusPlaying, withEstimate(80), withLogged(20*60), withLastPlayed(0)),
	}

	picks := selectTonightPicks(library, 120, nil, tonightNow)
	if picks.Continue == nil || picks.Continue.Entry.Game.Name != "Fits And Stale" {
		t.Fatalf("Continue = %+v, want Fits And Stale", picks.Continue)
	}
	if !strings.Contains(picks.Continue.Reason, "1h remaining — you could finish it tonight") {
		t.Errorf("Continue.Reason = %q, want it to mention finishing tonight", picks.Continue.Reason)
	}
	if !strings.Contains(picks.Continue.Reason, "5 days ago") {
		t.Errorf("Continue.Reason = %q, want last-played staleness", picks.Continue.Reason)
	}
}

// TestTonightCategoriesDoNotRepeatEntries: one dominant game must not hoard two
// categories; the next-best candidate fills the second slot.
func TestTonightCategoriesDoNotRepeatEntries(t *testing.T) {
	library := []tonightCandidate{
		// Dominates short win, wildcard and rescue alike.
		cand("Stardew Valley", models.StatusBacklog,
			withEstimate(2), withIGDB(95), withQueueRank(1), withOwned(8*365.25)),
		cand("Cuphead", models.StatusBacklog, withEstimate(10), withIGDB(88), withOwned(400)),
		cand("Old Guilt", models.StatusBacklog, withEstimate(30), withOwned(5*365.25)),
	}

	picks := selectTonightPicks(library, 120, nil, tonightNow)

	if picks.ShortWin == nil || picks.ShortWin.Entry.Game.Name != "Stardew Valley" {
		t.Fatalf("ShortWin = %+v, want Stardew Valley", picks.ShortWin)
	}
	if picks.Wildcard == nil || picks.Wildcard.Entry.Game.Name != "Cuphead" {
		t.Fatalf("Wildcard = %+v, want Cuphead to take over once the winner is claimed", picks.Wildcard)
	}
	if picks.Rescue == nil || picks.Rescue.Entry.Game.Name != "Old Guilt" {
		t.Fatalf("Rescue = %+v, want Old Guilt", picks.Rescue)
	}
}

// TestTonightExcludeDropsEntries covers the re-roll flow: excluding the pick
// just shown hands the category to the runner-up.
func TestTonightExcludeDropsEntries(t *testing.T) {
	library := []tonightCandidate{
		cand("First", models.StatusBacklog, withEstimate(1), withIGDB(95), withQueueRank(1)),
		cand("Second", models.StatusBacklog, withEstimate(1.5), withIGDB(70), withQueueRank(2)),
	}

	picks := selectTonightPicks(library, 120, map[string]bool{"First": true}, tonightNow)
	if picks.ShortWin == nil || picks.ShortWin.Entry.Game.Name != "Second" {
		t.Fatalf("ShortWin = %+v, want Second after excluding First", picks.ShortWin)
	}
}

// TestTonightEmptyLibrary: nothing eligible means null categories, not errors.
func TestTonightEmptyLibrary(t *testing.T) {
	picks := selectTonightPicks(nil, 90, nil, tonightNow)
	if picks.Continue != nil || picks.ShortWin != nil || picks.Wildcard != nil || picks.Rescue != nil {
		t.Fatalf("expected all categories null, got %+v", picks)
	}
}

// TestTonightRescueReasons: the guilt copy covers the never-played case too.
func TestTonightRescueReasons(t *testing.T) {
	tests := []struct {
		name string
		c    tonightCandidate
		want string
	}{
		{
			"hours logged",
			cand("Skyrim", models.StatusBacklog, withLogged(3*60), withOwned(6*365.25)),
			"You've owned it for 6 years and have 3 hours logged",
		},
		{
			"never played",
			cand("Oblivion", models.StatusBacklog, withOwned(6*365.25)),
			"You've owned it for 6 years and have never played it",
		},
		{
			"under an hour",
			cand("Limbo", models.StatusBacklog, withLogged(45), withOwned(400)),
			"You've owned it for 1 year and have under an hour logged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasonRescue(tt.c); got != tt.want {
				t.Errorf("reasonRescue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatPickHours(t *testing.T) {
	tests := []struct {
		hours float64
		want  string
	}{
		{0.75, "45m"},
		{1, "1h"},
		{1.5, "1h 30m"},
		{14, "14h"},
		{1.99, "1h 59m"},
		{1.995, "2h"}, // minute rounding carries into the hour
	}
	for _, tt := range tests {
		if got := formatPickHours(tt.hours); got != tt.want {
			t.Errorf("formatPickHours(%v) = %q, want %q", tt.hours, got, tt.want)
		}
	}
}

func TestDaysAgoText(t *testing.T) {
	tests := []struct {
		days float64
		want string
	}{
		{0, "today"},
		{1, "yesterday"},
		{3, "3 days ago"},
		{35, "1 month ago"},
		{70, "2 months ago"},
		{730, "2 years ago"},
	}
	for _, tt := range tests {
		if got := daysAgoText(tt.days); got != tt.want {
			t.Errorf("daysAgoText(%v) = %q, want %q", tt.days, got, tt.want)
		}
	}
}
