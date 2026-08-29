package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/collinpendleton/backhog/api/internal/db"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/models"
)

func newPlatformStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "platforms.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(database)
}

func platformByID(t *testing.T, platforms []models.Platform, id int64) models.Platform {
	t.Helper()
	for _, p := range platforms {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("platform %d not found in %v", id, platforms)
	return models.Platform{}
}

// UpsertGame must classify catalog platforms and leave unknown ones
// unclassified without failing: reads degrade them to family "other" and a
// NULL generation.
func TestUpsertGameWritesPlatformMeta(t *testing.T) {
	s := newPlatformStore(t)
	ctx := context.Background()

	err := s.UpsertGame(ctx, metadata.Game{
		ID:   1,
		Name: "Cross-Platform Hit",
		Platforms: []metadata.Ref{
			{ID: 7, Name: "PlayStation"},
			{ID: 9999, Name: "WeirdOS"},
		},
	}, "")
	if err != nil {
		t.Fatalf("UpsertGame: %v", err)
	}

	game, err := s.GetGame(ctx, 1)
	if err != nil {
		t.Fatalf("GetGame: %v", err)
	}

	ps := platformByID(t, game.Platforms, 7)
	if ps.Manufacturer != metadata.ManufacturerSony || ps.Family != metadata.FamilyPlayStation {
		t.Errorf("PS1 classification = %s/%s", ps.Manufacturer, ps.Family)
	}
	if ps.Generation == nil || *ps.Generation != 5 {
		t.Errorf("PS1 generation = %v, want 5", ps.Generation)
	}

	weird := platformByID(t, game.Platforms, 9999)
	if weird.Family != metadata.FamilyOther || weird.Manufacturer != metadata.ManufacturerOther {
		t.Errorf("unknown platform should degrade to other/Other, got %s/%s", weird.Manufacturer, weird.Family)
	}
	if weird.Generation != nil {
		t.Errorf("unknown platform generation should be null, got %d", *weird.Generation)
	}
}

// Rows cached before the columns existed get healed by the startup sync;
// unknown rows stay NULL/empty rather than being guessed at.
func TestSyncPlatformMetaHealsExistingRows(t *testing.T) {
	s := newPlatformStore(t)
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	exec(`INSERT INTO platforms (id, name) VALUES (167, 'PlayStation 5'), (1234, 'Mystery Box')`)

	if err := s.SyncPlatformMeta(ctx); err != nil {
		t.Fatalf("SyncPlatformMeta: %v", err)
	}

	var gen sql.NullInt64
	var fam string
	if err := s.db.QueryRowContext(ctx,
		`SELECT generation, family FROM platforms WHERE id = 167`).Scan(&gen, &fam); err != nil {
		t.Fatalf("query ps5: %v", err)
	}
	if !gen.Valid || gen.Int64 != 9 || fam != metadata.FamilyPlayStation {
		t.Errorf("PS5 meta = gen %d/%t fam %q, want 9 playstation", gen.Int64, gen.Valid, fam)
	}

	var mgen sql.NullInt64
	var mfam string
	if err := s.db.QueryRowContext(ctx,
		`SELECT generation, family FROM platforms WHERE id = 1234`).Scan(&mgen, &mfam); err != nil {
		t.Fatalf("query mystery: %v", err)
	}
	if mgen.Valid || mfam != "" {
		t.Errorf("unknown platform should stay NULL/empty, got gen=%d fam=%q", mgen.Int64, mfam)
	}
}

// The filter-rail facets expose the same classification, with the same
// degradation for unclassified platforms.
func TestFacetsIncludePlatformMeta(t *testing.T) {
	s := newPlatformStore(t)
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'u1@example.com', 'u1', 'x')`)
	exec(`INSERT INTO platforms (id, name) VALUES (7, 'PlayStation'), (9999, 'WeirdOS')`)
	if err := s.SyncPlatformMeta(ctx); err != nil {
		t.Fatalf("SyncPlatformMeta: %v", err)
	}
	exec(`INSERT INTO games (id, name) VALUES (1, 'Hit')`)
	exec(`INSERT INTO game_platforms (game_id, platform_id) VALUES (1, 7), (1, 9999)`)
	exec(`INSERT INTO library_entries (id, user_id, game_id) VALUES ('e1', 'u1', 1)`)

	platforms, _, err := s.Facets(ctx, "u1")
	if err != nil {
		t.Fatalf("Facets: %v", err)
	}

	ps := platformByID(t, platforms, 7)
	if ps.Family != metadata.FamilyPlayStation || ps.Generation == nil || *ps.Generation != 5 {
		t.Errorf("PS1 facet = %+v", ps)
	}
	weird := platformByID(t, platforms, 9999)
	if weird.Family != metadata.FamilyOther || weird.Generation != nil {
		t.Errorf("unknown facet should degrade to other/null, got %+v", weird)
	}
}

// The platform family/generation smart-list rules filter on the entry's own
// "playing on" platform, not the game's release platforms.
func TestSmartListPlatformMetaRules(t *testing.T) {
	s := newPlatformStore(t)
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, username, password_hash) VALUES ('u1', 'u1@example.com', 'u1', 'x')`)
	exec(`INSERT INTO platforms (id, name) VALUES (167, 'PlayStation 5'), (6, 'PC (Microsoft Windows)')`)
	if err := s.SyncPlatformMeta(ctx); err != nil {
		t.Fatalf("SyncPlatformMeta: %v", err)
	}
	exec(`INSERT INTO games (id, name) VALUES (1, 'Console Game'), (2, 'Desk Game'), (3, 'Undecided')`)
	exec(`INSERT INTO library_entries (id, user_id, game_id, platform_id) VALUES
		('e1', 'u1', 1, 167),
		('e2', 'u1', 2, 6),
		('e3', 'u1', 3, NULL)`)

	names := func(t *testing.T, rs models.RuleSet) map[string]bool {
		t.Helper()
		entries, err := s.evaluateSmart(ctx, "u1", rs)
		if err != nil {
			t.Fatalf("evaluateSmart: %v", err)
		}
		out := map[string]bool{}
		for _, e := range entries {
			out[e.Game.Name] = true
		}
		return out
	}

	got := names(t, models.RuleSet{Match: "all", Rules: []models.Rule{
		{Field: "platform_family", Op: "eq", Value: metadata.FamilyPlayStation},
	}})
	if len(got) != 1 || !got["Console Game"] {
		t.Errorf("platform_family=playstation matched %v", got)
	}

	got = names(t, models.RuleSet{Match: "all", Rules: []models.Rule{
		{Field: "platform_generation", Op: "gte", Value: 8},
	}})
	if len(got) != 1 || !got["Console Game"] {
		t.Errorf("platform_generation>=8 matched %v", got)
	}

	got = names(t, models.RuleSet{Match: "all", Rules: []models.Rule{
		{Field: "platform_generation", Op: "is_null"},
	}})
	if len(got) != 1 || !got["Undecided"] {
		t.Errorf("platform_generation is_null matched %v", got)
	}

	got = names(t, models.RuleSet{Match: "any", Rules: []models.Rule{
		{Field: "platform_family", Op: "in", Value: []any{metadata.FamilyPlayStation, metadata.FamilyPC}},
	}})
	if len(got) != 2 || !got["Console Game"] || !got["Desk Game"] {
		t.Errorf("platform_family in [playstation, pc] matched %v", got)
	}

	// Enum values are validated like every other enum field.
	if err := ValidateRuleSet(models.RuleSet{Match: "all", Rules: []models.Rule{
		{Field: "platform_family", Op: "eq", Value: "nonsense"},
	}}); err == nil {
		t.Error("invalid platform_family value should be rejected")
	}
}
