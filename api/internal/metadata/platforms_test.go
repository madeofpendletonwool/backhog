package metadata

import "testing"

// fixtureIDs are the platform ids the store test fixtures insert; every one
// of them must be classified so tests exercise the real catalog path.
var fixtureIDs = []int64{6, 130, 38}

func TestCatalogCoversFixturePlatforms(t *testing.T) {
	for _, id := range fixtureIDs {
		if _, ok := PlatformCatalog[id]; !ok {
			t.Errorf("platform %d used by store test fixtures is not in the catalog", id)
		}
	}
}

func TestCatalogInvariants(t *testing.T) {
	validFamily := make(map[string]bool, len(PlatformFamilies))
	for _, f := range PlatformFamilies {
		validFamily[f] = true
	}
	manufacturers := map[string]bool{
		ManufacturerNintendo: true, ManufacturerSony: true, ManufacturerMicrosoft: true,
		ManufacturerSega: true, ManufacturerPC: true, ManufacturerOther: true,
	}

	for id, meta := range PlatformCatalog {
		if !validFamily[meta.Family] {
			t.Errorf("platform %d: unknown family %q", id, meta.Family)
		}
		if !manufacturers[meta.Manufacturer] {
			t.Errorf("platform %d: unknown manufacturer %q", id, meta.Manufacturer)
		}
		if meta.Generation < 0 || meta.Generation > 9 {
			t.Errorf("platform %d: generation %d outside 0-9", id, meta.Generation)
		}
	}
}

func TestCatalogClassifications(t *testing.T) {
	cases := []struct {
		id           int64
		manufacturer string
		family       string
		generation   int
		handheld     bool
	}{
		{18, ManufacturerNintendo, FamilyNintendoConsole, 3, false}, // NES
		{19, ManufacturerNintendo, FamilyNintendoConsole, 4, false}, // SNES
		{4, ManufacturerNintendo, FamilyNintendoConsole, 5, false},  // N64
		{21, ManufacturerNintendo, FamilyNintendoConsole, 6, false}, // GameCube
		{5, ManufacturerNintendo, FamilyNintendoConsole, 7, false},  // Wii
		{41, ManufacturerNintendo, FamilyNintendoConsole, 8, false}, // Wii U
		{130, ManufacturerNintendo, FamilyNintendoConsole, 8, true}, // Switch
		{508, ManufacturerNintendo, FamilyNintendoConsole, 9, true}, // Switch 2
		{33, ManufacturerNintendo, FamilyGameBoy, 4, true},          // Game Boy
		{22, ManufacturerNintendo, FamilyGameBoy, 5, true},          // Game Boy Color
		{24, ManufacturerNintendo, FamilyGameBoy, 6, true},          // GBA
		{20, ManufacturerNintendo, FamilyNintendoHandheld, 7, true}, // DS
		{37, ManufacturerNintendo, FamilyNintendoHandheld, 8, true}, // 3DS
		{7, ManufacturerSony, FamilyPlayStation, 5, false},          // PS1
		{8, ManufacturerSony, FamilyPlayStation, 6, false},          // PS2
		{9, ManufacturerSony, FamilyPlayStation, 7, false},          // PS3
		{48, ManufacturerSony, FamilyPlayStation, 8, false},         // PS4
		{167, ManufacturerSony, FamilyPlayStation, 9, false},        // PS5
		{38, ManufacturerSony, FamilyPlayStation, 7, true},          // PSP
		{46, ManufacturerSony, FamilyPlayStation, 8, true},          // Vita
		{11, ManufacturerMicrosoft, FamilyXbox, 6, false},           // Xbox
		{12, ManufacturerMicrosoft, FamilyXbox, 7, false},           // Xbox 360
		{49, ManufacturerMicrosoft, FamilyXbox, 8, false},           // Xbox One
		{169, ManufacturerMicrosoft, FamilyXbox, 9, false},          // Series X|S
		{29, ManufacturerSega, FamilyOther, 4, false},               // Genesis
		{32, ManufacturerSega, FamilyOther, 5, false},               // Saturn
		{23, ManufacturerSega, FamilyOther, 6, false},               // Dreamcast
		{6, ManufacturerPC, FamilyPC, 0, false},                     // Windows
		{3, ManufacturerPC, FamilyPC, 0, false},                     // Linux
		{14, ManufacturerPC, FamilyPC, 0, false},                    // Mac
	}
	for _, c := range cases {
		meta, ok := PlatformCatalog[c.id]
		if !ok {
			t.Errorf("platform %d missing from catalog", c.id)
			continue
		}
		if meta.Manufacturer != c.manufacturer || meta.Family != c.family ||
			meta.Generation != c.generation || meta.Handheld != c.handheld {
			t.Errorf("platform %d = %+v, want %s/%s/gen %d/handheld %t",
				c.id, meta, c.manufacturer, c.family, c.generation, c.handheld)
		}
	}
}

// Unknown platforms must never be an error: callers serve family "other" and
// a NULL generation for ids the catalog does not know.
func TestUnknownPlatformDegrades(t *testing.T) {
	if _, ok := PlatformCatalog[999999]; ok {
		t.Fatal("id 999999 should not be classified")
	}
	if !contains(PlatformFamilies, FamilyOther) {
		t.Fatal("family fallback \"other\" must stay in the closed family set")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
