package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// The genre-burnout lookback: genres dominating this much recent play are
// considered fatigued, and backlog suggestions from them are down-weighted.
const burnoutWindowDays = 14

// burnoutMinMinutes is the smallest amount of recent play the burnout signal is
// computed from. Below it, two evenings of one genre say nothing.
const burnoutMinMinutes = 120

// burnoutPenalty is the most a fatigued genre can suppress a candidate: a game
// whose genres cover the whole recent mix scores at half strength.
const burnoutPenalty = 0.5

// tonightCandidate distils one library entry down to the signals the scorer
// needs. The DB layer fills these in; the scorer itself is a pure function so
// the heuristics can be table-tested without a database.
type tonightCandidate struct {
	entry models.Entry
	// remaining is hours left to beat (estimate minus logged, floored at
	// zero). Nil means no estimate exists.
	remaining *float64
	// lastPlayed is the newest session date, falling back to started_at.
	lastPlayed *time.Time
	// ownedDays is how long the entry has been in the library.
	ownedDays float64
	// queueRank is the 1-based position in the play queue; 0 when unqueued.
	queueRank int
	// burnoutShare is the largest share (0..1) of recent minutes that went to
	// one of this game's genres. Zero when there is no recent mix.
	burnoutShare float64
}

// TonightPicks answers "I have N minutes tonight, what should I play?" with one
// pick per category: continue something in progress, a short game that fits the
// budget, a wildcard never played, and the longest-owned least-progressed
// rescue. Deterministic heuristics only — no randomness, no AI.
func (s *Store) TonightPicks(ctx context.Context, userID string, minutes int, exclude []string) (models.TonightPicksResult, error) {
	now := time.Now().UTC()

	candidates, err := s.tonightCandidates(ctx, userID, now)
	if err != nil {
		return models.TonightPicksResult{}, err
	}

	skip := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		skip[id] = true
	}
	return selectTonightPicks(candidates, float64(minutes), skip, now), nil
}

// tonightCandidates loads and distils every backlog and playing entry.
func (s *Store) tonightCandidates(ctx context.Context, userID string, now time.Time) ([]tonightCandidate, error) {
	entries, err := s.queryEntries(ctx, entrySelect+`
		WHERE e.user_id = ? AND e.status IN ('backlog','playing')`, userID)
	if err != nil {
		return nil, err
	}

	lastPlayed, err := s.lastPlayedByEntry(ctx, userID)
	if err != nil {
		return nil, err
	}

	genreMinutes, err := s.recentGenreMinutes(ctx, userID)
	if err != nil {
		return nil, err
	}
	var genreTotal float64
	for _, m := range genreMinutes {
		genreTotal += m
	}
	if genreTotal < burnoutMinMinutes {
		genreMinutes = nil // sample too thin to mean anything
	}

	// Queue ranks come from the same ordering the queue endpoint uses.
	queued := make([]models.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Status == models.StatusBacklog {
			queued = append(queued, e)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		a, b := queued[i], queued[j]
		ap, bp := a.QueuePosition, b.QueuePosition
		switch {
		case ap != nil && bp != nil && *ap != *bp:
			return *ap < *bp
		case ap != nil && bp == nil:
			return true
		case ap == nil && bp != nil:
			return false
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	rankByID := make(map[string]int, len(queued))
	for i, e := range queued {
		rankByID[e.ID] = i + 1
	}

	candidates := make([]tonightCandidate, 0, len(entries))
	for _, e := range entries {
		c := tonightCandidate{
			entry:      e,
			ownedDays:  now.Sub(e.CreatedAt).Hours() / 24,
			queueRank:  rankByID[e.ID],
			lastPlayed: lastPlayed[e.ID],
		}
		if c.lastPlayed == nil {
			c.lastPlayed = e.StartedAt
		}
		if e.Game.TimeToBeatMain != nil {
			remaining := math.Max(0, float64(*e.Game.TimeToBeatMain)-float64(e.LoggedMinutes)*60) / 3600
			c.remaining = &remaining
		}
		if genreMinutes != nil {
			c.burnoutShare = maxGenreShare(e, genreMinutes, genreTotal)
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// lastPlayedByEntry maps entry id to the date of its newest session.
func (s *Store) lastPlayedByEntry(ctx context.Context, userID string) (map[string]*time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, MAX(played_on) FROM play_sessions
		WHERE user_id = ? GROUP BY entry_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]*time.Time{}
	for rows.Next() {
		var entryID, playedOn string
		if err := rows.Scan(&entryID, &playedOn); err != nil {
			return nil, err
		}
		if t, err := time.Parse("2006-01-02", playedOn); err == nil {
			out[entryID] = &t
		}
	}
	return out, rows.Err()
}

// recentGenreMinutes sums logged minutes per genre over the burnout window.
func (s *Store) recentGenreMinutes(ctx context.Context, userID string) (map[int64]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT gg.genre_id, SUM(ps.minutes)
		FROM play_sessions ps
		JOIN library_entries e ON e.id = ps.entry_id
		JOIN game_genres gg ON gg.game_id = e.game_id
		WHERE ps.user_id = ? AND ps.played_on >= date('now', '-14 days')
		GROUP BY gg.genre_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]float64{}
	for rows.Next() {
		var genreID int64
		var minutes float64
		if err := rows.Scan(&genreID, &minutes); err != nil {
			return nil, err
		}
		out[genreID] = minutes
	}
	return out, rows.Err()
}

// maxGenreShare returns the largest share of the recent genre mix covered by
// one of the entry's genres.
func maxGenreShare(e models.Entry, genreMinutes map[int64]float64, total float64) float64 {
	if total <= 0 {
		return 0
	}
	share := 0.0
	for _, g := range e.Game.Genres {
		if m := genreMinutes[g.ID]; m > 0 {
			share = math.Max(share, m/total)
		}
	}
	return math.Min(share, 1)
}

// category pairs one category's eligibility+score with the reason text built
// from the same signals, so a pick's explanation can never drift from the
// formula that chose it.
type category struct {
	score  func(tonightCandidate) (float64, bool)
	reason func(tonightCandidate) string
}

// selectTonightPicks picks the best candidate per category. Categories claim
// their winner in display order, and a game already chosen for one category is
// not offered again for another. Ties break alphabetically so the same library
// always yields the same picks.
func selectTonightPicks(candidates []tonightCandidate, budgetMinutes float64, skip map[string]bool, now time.Time) models.TonightPicksResult {
	continueCat := category{
		score:  func(c tonightCandidate) (float64, bool) { return scoreContinue(c, budgetMinutes, now) },
		reason: func(c tonightCandidate) string { return reasonContinue(c, budgetMinutes, now) },
	}
	shortWinCat := category{
		score:  func(c tonightCandidate) (float64, bool) { return scoreShortWin(c, budgetMinutes) },
		reason: reasonShortWin,
	}
	wildcardCat := category{score: scoreWildcard, reason: reasonWildcard}
	rescueCat := category{score: scoreRescue, reason: reasonRescue}

	used := make(map[string]bool, len(skip)+4)
	taken := func(c tonightCandidate) bool { return used[c.entry.ID] || skip[c.entry.ID] }
	claim := func(cat category) *models.TonightPick {
		best := -1.0
		var bestPick *models.TonightPick
		for _, c := range candidates {
			if taken(c) {
				continue
			}
			score, ok := cat.score(c)
			if !ok {
				continue
			}
			if bestPick != nil && (score < best || (score == best && c.entry.Game.Name >= bestPick.Entry.Game.Name)) {
				continue
			}
			best = score
			bestPick = &models.TonightPick{Entry: c.entry, Score: score, Reason: cat.reason(c)}
		}
		if bestPick != nil {
			used[bestPick.Entry.ID] = true
		}
		return bestPick
	}

	return models.TonightPicksResult{
		Continue: claim(continueCat),
		ShortWin: claim(shortWinCat),
		Wildcard: claim(wildcardCat),
		Rescue:   claim(rescueCat),
	}
}

// scoreContinue favours the in-progress game with momentum: the more recently
// it was played the better, and a game that fits the remaining budget gets a
// "you could finish it tonight" boost.
func scoreContinue(c tonightCandidate, budgetMinutes float64, now time.Time) (float64, bool) {
	if c.entry.Status != models.StatusPlaying {
		return 0, false
	}
	score := 30.0
	if c.remaining != nil {
		score = 50
		if *c.remaining*60 <= budgetMinutes {
			score += 25
		}
	}
	score += math.Max(0, 40-2*c.daysSinceLastPlay(now))
	return score, true
}

// scoreShortWin wants a backlog game that fits entirely in the budget,
// preferring ones high in the queue and well rated.
func scoreShortWin(c tonightCandidate, budgetMinutes float64) (float64, bool) {
	if c.entry.Status != models.StatusBacklog || c.remaining == nil || *c.remaining*60 > budgetMinutes {
		return 0, false
	}
	score := 0.0
	if c.queueRank > 0 {
		score += math.Max(0, 40-4*float64(c.queueRank-1))
	}
	score += ratingScore(c, 35, 15)
	return score * (1 - burnoutPenalty*c.burnoutShare), true
}

// scoreWildcard ignores fit on purpose: the highest-rated game you have never
// touched, with a small bonus for buried treasures the queue never surfaces.
func scoreWildcard(c tonightCandidate) (float64, bool) {
	if c.entry.Status != models.StatusBacklog || c.entry.LoggedMinutes != 0 {
		return 0, false
	}
	score := ratingScore(c, 60, 20)
	if c.queueRank == 0 || c.queueRank > 10 {
		score += 15
	}
	return score * (1 - burnoutPenalty*c.burnoutShare), true
}

// scoreRescue is pure guilt: owned the longest, progressed the least. Genre
// burnout deliberately does not apply — the debt does not fade with fatigue.
func scoreRescue(c tonightCandidate) (float64, bool) {
	if c.entry.Status != models.StatusBacklog && c.entry.Status != models.StatusPlaying {
		return 0, false
	}
	score := math.Min(60, c.ownedDays/365.25*12)
	score += (1 - progressFraction(c)) * 40
	return score, true
}

// ratingScore blends the IGDB crowd rating and the user's own into a score
// capped at igdbWeight+userWeight points.
func ratingScore(c tonightCandidate, igdbWeight, userWeight float64) float64 {
	score := 0.0
	if r := c.entry.Game.IGDBRating; r != nil {
		score += *r / 100 * igdbWeight
	}
	if u := c.entry.UserRating; u != nil {
		score += float64(*u) / 10 * userWeight
	}
	return score
}

// progressFraction is logged time over the total estimate, 0 when there is no
// estimate to measure against.
func progressFraction(c tonightCandidate) float64 {
	if c.remaining == nil {
		return 0
	}
	total := *c.remaining + float64(c.entry.LoggedMinutes)/60
	if total <= 0 {
		return 1
	}
	return math.Min(1, float64(c.entry.LoggedMinutes)/60/total)
}

// daysSinceLastPlay falls back to ownership age when there is no play or start
// date at all, so an untouched "playing" entry reads as long stalled.
func (c tonightCandidate) daysSinceLastPlay(now time.Time) float64 {
	if c.lastPlayed != nil {
		days := now.Sub(*c.lastPlayed).Hours() / 24
		if days < 0 {
			return 0
		}
		return days
	}
	return c.ownedDays
}

// reasonContinue explains the momentum pick: what's left and when it was last
// touched, e.g. "14h remaining · last played 3 days ago".
func reasonContinue(c tonightCandidate, budgetMinutes float64, now time.Time) string {
	parts := []string{}
	if c.remaining == nil {
		parts = append(parts, "no time estimate")
	} else if *c.remaining*60 <= budgetMinutes {
		parts = append(parts, fmt.Sprintf("%s remaining — you could finish it tonight", formatPickHours(*c.remaining)))
	} else {
		parts = append(parts, fmt.Sprintf("%s remaining", formatPickHours(*c.remaining)))
	}
	if c.lastPlayed != nil {
		parts = append(parts, "last played "+daysAgoText(c.daysSinceLastPlay(now)))
	}
	return strings.Join(parts, " · ")
}

// reasonShortWin explains the budget fit: "~2h to beat · #3 in your queue · 86
// on IGDB", dropping the parts that don't exist.
func reasonShortWin(c tonightCandidate) string {
	parts := []string{fmt.Sprintf("~%s to beat", formatPickHours(*c.remaining))}
	if c.queueRank > 0 {
		parts = append(parts, fmt.Sprintf("#%d in your queue", c.queueRank))
	}
	parts = append(parts, ratingSuffix(c)...)
	return strings.Join(parts, " · ")
}

// reasonWildcard explains the spice pick: rating-led, fit deliberately ignored.
func reasonWildcard(c tonightCandidate) string {
	parts := append([]string{"You've never played it"}, ratingSuffix(c)...)
	return strings.Join(parts, " · ")
}

// reasonRescue explains the guilt pick: how long owned and how little played.
func reasonRescue(c tonightCandidate) string {
	owned := fmt.Sprintf("You've owned it for %s and ", ownedForText(c.ownedDays))
	if c.entry.LoggedMinutes == 0 {
		return owned + "have never played it"
	}
	return owned + fmt.Sprintf("have %s logged", loggedHoursText(c.entry.LoggedMinutes))
}

// ratingSuffix appends the ratings that earned a pick, when they exist.
func ratingSuffix(c tonightCandidate) []string {
	var parts []string
	if r := c.entry.Game.IGDBRating; r != nil {
		parts = append(parts, fmt.Sprintf("%d on IGDB", int(math.Round(*r))))
	}
	if u := c.entry.UserRating; u != nil {
		parts = append(parts, fmt.Sprintf("you rated it %d", *u))
	}
	return parts
}

// formatPickHours renders hours as "45m", "14h" or "1h 30m", matching the
// client's duration format.
func formatPickHours(hours float64) string {
	if hours < 1 {
		return fmt.Sprintf("%dm", int(math.Round(hours*60)))
	}
	h := math.Floor(hours)
	m := math.Round((hours - h) * 60)
	if m == 60 {
		h++
		m = 0
	}
	if m == 0 {
		return fmt.Sprintf("%dh", int(h))
	}
	return fmt.Sprintf("%dh %dm", int(h), int(m))
}

// daysAgoText renders a day count as "today", "3 days ago", "2 months ago"…
func daysAgoText(days float64) string {
	switch d := int(math.Floor(days)); {
	case d <= 0:
		return "today"
	case d == 1:
		return "yesterday"
	case d < 30:
		return fmt.Sprintf("%d days ago", d)
	case d < 365:
		return pluralCount(int(math.Round(days/30.44)), "month") + " ago"
	default:
		return pluralCount(int(math.Round(days/365.25)), "year") + " ago"
	}
}

// ownedForText renders an ownership span as "6 years", "8 months", "3 weeks".
func ownedForText(days float64) string {
	switch {
	case days >= 365.25:
		return pluralCount(int(math.Round(days/365.25)), "year")
	case days >= 30.44:
		return pluralCount(int(math.Round(days/30.44)), "month")
	default:
		return pluralCount(int(math.Round(days/7)), "week")
	}
}

func pluralCount(n int, unit string) string {
	if n < 1 {
		n = 1
	}
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// loggedHoursText renders logged minutes as "3 hours" / "under an hour".
func loggedHoursText(minutes int) string {
	hours := float64(minutes) / 60
	if hours < 1 {
		return "under an hour"
	}
	return fmt.Sprintf("%d hours", int(math.Round(hours)))
}
