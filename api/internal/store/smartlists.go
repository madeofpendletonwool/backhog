package store

import (
	"fmt"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

// smartField describes one queryable field of a smart list rule.
type smartField struct {
	Column string // SQL expression; never built from user input
	Type   string // "text" | "number" | "date" | "enum" | "ref"
	Label  string // shown in the rule builder UI
	Ops    []string
	Enum   []string
	// GameOnly marks fields that read games-table columns. Not exposed to the
	// UI — the media scoping decision is list-level, not per-field (see
	// ruleSetTargetsBooks).
	GameOnly bool
}

// Durations are stored in seconds but presented in hours, so the builder sends
// hours and we convert. Ratings are IGDB's 0-100 scale.
var smartFields = map[string]smartField{
	"media_type": {
		Column: "e.media_type", Type: "enum", Label: "Media type",
		Ops:  []string{"eq", "neq", "in"},
		Enum: models.AllMediaTypes,
	},
	"status": {
		Column: "e.status", Type: "enum", Label: "Status",
		Ops:  []string{"eq", "neq", "in"},
		Enum: models.AllStatuses,
	},
	"logged_hours": {
		Column: "(SELECT COALESCE(SUM(ps.minutes), 0) / 60.0 FROM play_sessions ps WHERE ps.entry_id = e.id)",
		Type:   "number", Label: "Hours I've logged",
		Ops: []string{"gt", "lt", "gte", "lte"},
	},
	"name":               {Column: "g.name", Type: "text", Label: "Title", Ops: []string{"contains", "eq"}, GameOnly: true},
	"igdb_rating":        {Column: "g.igdb_rating", Type: "number", Label: "IGDB rating", Ops: []string{"gt", "lt", "gte", "lte"}, GameOnly: true},
	"user_rating":        {Column: "e.user_rating", Type: "number", Label: "My rating", Ops: []string{"gt", "lt", "gte", "lte", "eq", "is_null", "not_null"}},
	"hours_to_beat":      {Column: "g.time_to_beat_main / 3600.0", Type: "number", Label: "Hours to beat", Ops: []string{"lt", "gt", "lte", "gte", "is_null", "not_null"}, GameOnly: true},
	"release_year":       {Column: "CAST(strftime('%Y', g.first_release_date, 'unixepoch') AS INTEGER)", Type: "number", Label: "Release year", Ops: []string{"eq", "gt", "lt", "gte", "lte"}, GameOnly: true},
	"days_since_added":   {Column: "julianday('now') - julianday(e.created_at)", Type: "number", Label: "Days since added", Ops: []string{"gt", "lt"}},
	"days_since_started": {Column: "julianday('now') - julianday(e.started_at)", Type: "number", Label: "Days since started", Ops: []string{"gt", "lt"}},
	"genre":              {Column: "genre", Type: "ref", Label: "Genre", Ops: []string{"in", "not_in"}, GameOnly: true},
	"platform":           {Column: "platform", Type: "ref", Label: "Platform", Ops: []string{"in", "not_in"}, GameOnly: true},
	"series":             {Column: "series", Type: "ref", Label: "Series", Ops: []string{"in", "not_in"}, GameOnly: true},
	// Platform family and generation read the "playing on" platform on the
	// entry itself — the honest signal for what someone actually finished on —
	// via the curated classification on the platforms row.
	"platform_family": {
		Column: "(SELECT COALESCE(NULLIF(p.family, ''), 'other') FROM platforms p WHERE p.id = e.platform_id)",
		Type:   "enum", Label: "Platform family",
		Ops:      []string{"eq", "neq", "in"},
		Enum:     metadata.PlatformFamilies,
		GameOnly: true,
	},
	"platform_generation": {
		Column:   "(SELECT p.generation FROM platforms p WHERE p.id = e.platform_id)",
		Type:     "number", Label: "Console generation",
		Ops:      []string{"eq", "gt", "lt", "gte", "lte", "is_null", "not_null"},
		GameOnly: true,
	},
}

// smartSorts whitelists sort keys for smart lists.
var smartSorts = map[string]string{
	"name":          "g.name COLLATE NOCASE",
	"igdb_rating":   "g.igdb_rating",
	"user_rating":   "e.user_rating",
	"hours_to_beat": "g.time_to_beat_main",
	"release_year":  "g.first_release_date",
	"added":         "e.created_at",
	"updated":       "e.updated_at",
}

// SmartFields exposes the field catalogue to the rule-builder UI.
func SmartFields() map[string]smartField { return smartFields }

// Media scoping decision (documented here once): a smart list carries its
// media scope at the list level — as media_type rules inside its rule set —
// rather than each field carrying a media tag. A rule set "targets books" when
// at least one of its media_type rules admits only books (eq 'book', or an
// in-list of books only); an unscoped set, or one scoped to games or both,
// does not. Game-only fields (title, IGDB rating, time-to-beat, genre,
// platform, series…) are rejected in a book-targeting set: the entry
// projection joins games, so those columns cannot be read there. In an
// unscoped or mixed set they stay available — they simply match nothing for
// book rows, which is ordinary SQL filter semantics.
func ruleSetTargetsBooks(rs models.RuleSet) bool {
	for _, rule := range rs.Rules {
		if rule.Field != "media_type" {
			continue
		}
		switch rule.Op {
		case "eq":
			if s, err := asString(rule.Value); err == nil && s == models.MediaBook {
				return true
			}
		case "in":
			values, err := asSlice(rule.Value)
			if err != nil || len(values) == 0 {
				continue
			}
			booksOnly := true
			for _, v := range values {
				s, err := asString(v)
				if err != nil || s != models.MediaBook {
					booksOnly = false
					break
				}
			}
			if booksOnly {
				return true
			}
		}
	}
	return false
}

// compileRules turns a validated rule set into a SQL fragment and its arguments.
// Field names and operators are only ever resolved through the maps above, so no
// user-controlled string reaches the query text.
func compileRules(rs models.RuleSet) (string, []any, error) {
	if len(rs.Rules) == 0 {
		return "1=1", nil, nil
	}

	booksOnly := ruleSetTargetsBooks(rs)

	joiner := " AND "
	if strings.EqualFold(rs.Match, "any") {
		joiner = " OR "
	}

	var clauses []string
	var args []any
	for _, rule := range rs.Rules {
		field, ok := smartFields[rule.Field]
		if !ok {
			return "", nil, fmt.Errorf("unknown field %q", rule.Field)
		}
		if booksOnly && field.GameOnly {
			return "", nil, fmt.Errorf("field %q is game-only and cannot be used in a book-scoped rule set", rule.Field)
		}
		clause, clauseArgs, err := compileRule(rule, field)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	return "(" + strings.Join(clauses, joiner) + ")", args, nil
}

func compileRule(rule models.Rule, field smartField) (string, []any, error) {
	if !contains(field.Ops, rule.Op) {
		return "", nil, fmt.Errorf("operator %q is not valid for field %q", rule.Op, rule.Field)
	}

	// Genre and platform are many-to-many, so they compile to EXISTS subqueries
	// against the join tables rather than a column comparison.
	if field.Type == "ref" {
		return compileRefRule(rule)
	}

	switch rule.Op {
	case "is_null":
		return field.Column + " IS NULL", nil, nil
	case "not_null":
		return field.Column + " IS NOT NULL", nil, nil
	case "contains":
		s, err := asString(rule.Value)
		if err != nil {
			return "", nil, err
		}
		return field.Column + " LIKE ? COLLATE NOCASE", []any{"%" + escapeLike(s) + "%"}, nil
	case "in":
		values, err := asSlice(rule.Value)
		if err != nil {
			return "", nil, err
		}
		if len(values) == 0 {
			return "0=1", nil, nil
		}
		if err := validateEnum(field, values); err != nil {
			return "", nil, err
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
		return field.Column + " IN (" + placeholders + ")", values, nil
	}

	op, ok := comparisonOps[rule.Op]
	if !ok {
		return "", nil, fmt.Errorf("unsupported operator %q", rule.Op)
	}
	if field.Type == "enum" {
		s, err := asString(rule.Value)
		if err != nil {
			return "", nil, err
		}
		if !contains(field.Enum, s) {
			return "", nil, fmt.Errorf("%q is not a valid value for %q", s, rule.Field)
		}
	}
	// NULL never satisfies a comparison, which is what we want: a game with no
	// known time-to-beat should not appear in a "under 8 hours" list.
	return fmt.Sprintf("(%s IS NOT NULL AND %s %s ?)", field.Column, field.Column, op), []any{rule.Value}, nil
}

var comparisonOps = map[string]string{
	"eq": "=", "neq": "!=", "gt": ">", "lt": "<", "gte": ">=", "lte": "<=",
}

// refJoins whitelists the many-to-many fields and the tables behind them.
// Table and column names are only ever resolved through this map, so no
// user-controlled string reaches the query text.
var refJoins = map[string][3]string{
	// field → {join table, lookup table, join column}
	"genre":    {"game_genres", "genres", "genre_id"},
	"platform": {"game_platforms", "platforms", "platform_id"},
	"series":   {"series_games", "series", "series_id"},
}

func compileRefRule(rule models.Rule) (string, []any, error) {
	join, ok := refJoins[rule.Field]
	if !ok {
		return "", nil, fmt.Errorf("%q is not a ref field", rule.Field)
	}
	joinTable, lookupTable, joinCol := join[0], join[1], join[2]

	values, err := asSlice(rule.Value)
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "1=1", nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")

	// Values are names, not ids: a smart list should survive a metadata refresh
	// and stay readable in the stored JSON.
	clause := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM %s j JOIN %s l ON l.id = j.%s
		WHERE j.game_id = e.game_id AND l.name IN (%s) COLLATE NOCASE)`,
		joinTable, lookupTable, joinCol, placeholders)

	if rule.Op == "not_in" {
		clause = "NOT " + clause
	}
	return clause, values, nil
}

func validateEnum(field smartField, values []any) error {
	if field.Type != "enum" {
		return nil
	}
	for _, v := range values {
		s, err := asString(v)
		if err != nil {
			return err
		}
		if !contains(field.Enum, s) {
			return fmt.Errorf("%q is not a valid value", s)
		}
	}
	return nil
}

func asString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected a text value, got %T", v)
	}
	return s, nil
}

func asSlice(v any) ([]any, error) {
	s, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list of values, got %T", v)
	}
	return s, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ValidateRuleSet checks a rule set without running it, so the API can reject a
// bad smart list at create time rather than at read time.
func ValidateRuleSet(rs models.RuleSet) error {
	if _, _, err := compileRules(rs); err != nil {
		return err
	}
	if rs.Sort != nil {
		if _, ok := smartSorts[rs.Sort.Field]; !ok {
			return fmt.Errorf("unknown sort field %q", rs.Sort.Field)
		}
	}
	return nil
}
