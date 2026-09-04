package store

import (
	"strings"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/models"
)

func TestCompileRulesRejectsUnsafeInput(t *testing.T) {
	tests := []struct {
		name string
		rule models.Rule
	}{
		{"sql in field name", models.Rule{Field: "g.name); DROP TABLE games;--", Op: "eq", Value: "x"}},
		{"unknown field", models.Rule{Field: "password_hash", Op: "eq", Value: "x"}},
		{"operator not allowed for field", models.Rule{Field: "status", Op: "gt", Value: "backlog"}},
		{"invalid enum value", models.Rule{Field: "status", Op: "eq", Value: "nonsense"}},
		{"wrong value type for in", models.Rule{Field: "genre", Op: "in", Value: "RPG"}},
		{"wrong value type for contains", models.Rule{Field: "name", Op: "contains", Value: 42}},
		{"series only supports ref operators", models.Rule{Field: "series", Op: "gt", Value: "Mass Effect"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := compileRules(models.RuleSet{Match: "all", Rules: []models.Rule{tt.rule}})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestCompileRulesParameterisesValues(t *testing.T) {
	rs := models.RuleSet{
		Match: "all",
		Rules: []models.Rule{
			{Field: "status", Op: "eq", Value: models.StatusBacklog},
			{Field: "hours_to_beat", Op: "lt", Value: 8.0},
			{Field: "name", Op: "contains", Value: "hollow"},
		},
	}

	sql, args, err := compileRules(rs)
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}

	// Values must never be inlined into the SQL text.
	for _, forbidden := range []string{"backlog", "hollow", "8"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("value %q was interpolated into SQL: %s", forbidden, sql)
		}
	}
	if len(args) != 3 {
		t.Errorf("got %d args, want 3: %v", len(args), args)
	}
	if !strings.Contains(sql, " AND ") {
		t.Errorf("match=all should join with AND: %s", sql)
	}
}

func TestCompileRulesMatchAny(t *testing.T) {
	rs := models.RuleSet{
		Match: "any",
		Rules: []models.Rule{
			{Field: "status", Op: "eq", Value: models.StatusPlaying},
			{Field: "user_rating", Op: "not_null"},
		},
	}
	sql, _, err := compileRules(rs)
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, " OR ") {
		t.Errorf("match=any should join with OR: %s", sql)
	}
}

// A series rule compiles to an EXISTS against the membership join table, with
// the series names as parameters — never inlined into the query text.
func TestCompileRulesSeriesRef(t *testing.T) {
	rs := models.RuleSet{
		Match: "all",
		Rules: []models.Rule{
			{Field: "series", Op: "in", Value: []any{"Mass Effect", "Dragon Age"}},
		},
	}
	sql, args, err := compileRules(rs)
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, "EXISTS") || !strings.Contains(sql, "series_games") {
		t.Errorf("series rule should compile to an EXISTS on series_games: %s", sql)
	}
	if strings.Contains(sql, "Mass Effect") {
		t.Errorf("series value was interpolated into SQL: %s", sql)
	}
	if len(args) != 2 {
		t.Errorf("got %d args, want 2: %v", len(args), args)
	}

	sql, _, err = compileRules(models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "series", Op: "not_in", Value: []any{"Mass Effect"}}},
	})
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Errorf("not_in should negate the EXISTS: %s", sql)
	}
}

// A NULL time-to-beat must not satisfy "under 8 hours".
func TestNumericComparisonExcludesNull(t *testing.T) {
	sql, _, err := compileRules(models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "hours_to_beat", Op: "lt", Value: 8.0}},
	})
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, "IS NOT NULL") {
		t.Errorf("comparison should exclude NULLs explicitly: %s", sql)
	}
}

func TestValidateRuleSetChecksSort(t *testing.T) {
	err := ValidateRuleSet(models.RuleSet{
		Match: "all",
		Sort:  &models.Sort{Field: "e.user_id", Dir: "asc"},
	})
	if err == nil {
		t.Fatal("expected unknown sort field to be rejected")
	}
}

func TestSeededDefaultRuleSetsAreValid(t *testing.T) {
	// The seeded lists are written by hand; this guards against a typo shipping
	// broken lists to every newly registered account.
	for _, rs := range defaultListRuleSets() {
		if err := ValidateRuleSet(rs.rules); err != nil {
			t.Errorf("seeded list %q is invalid: %v", rs.name, err)
		}
	}
}

func TestBookScopedRuleSetRejectsGameOnlyFields(t *testing.T) {
	tests := []struct {
		name    string
		rules   []models.Rule
		wantErr bool
	}{
		{
			"book scope rejects a game-only field",
			[]models.Rule{
				{Field: "media_type", Op: "eq", Value: models.MediaBook},
				{Field: "genre", Op: "in", Value: []any{"RPG"}},
			},
			true,
		},
		{
			"book in-list scope rejects a game-only field",
			[]models.Rule{
				{Field: "media_type", Op: "in", Value: []any{models.MediaBook}},
				{Field: "igdb_rating", Op: "gt", Value: 80.0},
			},
			true,
		},
		{
			"book scope keeps media-agnostic fields",
			[]models.Rule{
				{Field: "media_type", Op: "eq", Value: models.MediaBook},
				{Field: "status", Op: "eq", Value: models.StatusBacklog},
			},
			false,
		},
		{
			"game scope keeps game-only fields",
			[]models.Rule{
				{Field: "media_type", Op: "eq", Value: models.MediaGame},
				{Field: "genre", Op: "in", Value: []any{"RPG"}},
			},
			false,
		},
		{
			"mixed scope keeps game-only fields",
			[]models.Rule{
				{Field: "media_type", Op: "in", Value: []any{models.MediaGame, models.MediaBook}},
				{Field: "hours_to_beat", Op: "lt", Value: 8.0},
			},
			false,
		},
		{
			"unscoped set unchanged",
			[]models.Rule{
				{Field: "series", Op: "in", Value: []any{"Mass Effect"}},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRuleSet(models.RuleSet{Match: "all", Rules: tt.rules})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRuleSet error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCompileMediaTypeRule(t *testing.T) {
	sql, args, err := compileRules(models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "media_type", Op: "eq", Value: models.MediaGame}},
	})
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, "e.media_type") {
		t.Errorf("media_type rule should compile against e.media_type: %s", sql)
	}
	if len(args) != 1 || args[0] != models.MediaGame {
		t.Errorf("args = %v, want [game]", args)
	}

	// An off-catalogue media value is rejected like any other enum.
	if _, _, err := compileRules(models.RuleSet{
		Match: "all",
		Rules: []models.Rule{{Field: "media_type", Op: "eq", Value: "vinyl"}},
	}); err == nil {
		t.Error("invalid media_type value should be rejected")
	}
}

func TestGameScopedRuleSetRejectsBookOnlyFields(t *testing.T) {
	tests := []struct {
		name    string
		rules   []models.Rule
		wantErr bool
	}{
		{
			"game scope rejects a book-only field",
			[]models.Rule{
				{Field: "media_type", Op: "eq", Value: models.MediaGame},
				{Field: "author", Op: "contains", Value: "Tolkien"},
			},
			true,
		},
		{
			"game in-list scope rejects a book-only field",
			[]models.Rule{
				{Field: "media_type", Op: "in", Value: []any{models.MediaGame}},
				{Field: "publish_year", Op: "lt", Value: 1970.0},
			},
			true,
		},
		{
			"book scope keeps book-only fields",
			[]models.Rule{
				{Field: "media_type", Op: "eq", Value: models.MediaBook},
				{Field: "title", Op: "contains", Value: "dune"},
			},
			false,
		},
		{
			"mixed scope keeps book-only fields",
			[]models.Rule{
				{Field: "media_type", Op: "in", Value: []any{models.MediaGame, models.MediaBook}},
				{Field: "author", Op: "eq", Value: "Frank Herbert"},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRuleSet(models.RuleSet{Match: "all", Rules: tt.rules})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRuleSet error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// The book-only fields compile against the books table and accept the new
// book sorts.
func TestBookFieldsCompile(t *testing.T) {
	rs := models.RuleSet{
		Match: "all",
		Rules: []models.Rule{
			{Field: "media_type", Op: "eq", Value: models.MediaBook},
			{Field: "author", Op: "contains", Value: "le guin"},
			{Field: "publish_year", Op: "gte", Value: 1960.0},
			{Field: "logged_hours", Op: "gt", Value: 4.0},
		},
		Sort: &models.Sort{Field: "author", Dir: "asc"},
	}
	sql, args, err := compileRules(rs)
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}
	if !strings.Contains(sql, "authors_json") || !strings.Contains(sql, "first_publish_year") {
		t.Errorf("book fields should read the books table: %s", sql)
	}
	if !strings.Contains(sql, "reading_sessions") {
		t.Errorf("logged_hours should include reading sessions: %s", sql)
	}
	if strings.Contains(sql, "le guin") {
		t.Errorf("author value was interpolated into SQL: %s", sql)
	}
	if len(args) != 4 {
		t.Errorf("args = %d, want 4: %v", len(args), args)
	}

	for _, key := range []string{"author", "published", "pages", "name"} {
		if err := ValidateRuleSet(models.RuleSet{Match: "all", Sort: &models.Sort{Field: key}}); err != nil {
			t.Errorf("sort %q rejected: %v", key, err)
		}
	}
	// "name" must now coalesce so a book-scoped set sorts by its titles.
	if !strings.Contains(smartSorts["name"], "COALESCE") {
		t.Errorf(`sort "name" should coalesce game names and book titles: %s`, smartSorts["name"])
	}
}
