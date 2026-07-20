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
