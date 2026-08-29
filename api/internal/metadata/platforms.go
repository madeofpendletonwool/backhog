package metadata

// Curated classification of IGDB platform ids, keyed by the same stable ids
// the shared platforms cache uses. This is the foundation for the
// platform-mastery achievements (Generation Gap, the manufacturer sets) and
// the platform smart-list fields. Ids and generation numbers were verified
// against igdb.com's own platform and generation pages.

const (
	FamilyNintendoConsole  = "nintendo_console"
	FamilyNintendoHandheld = "nintendo_handheld"
	FamilyGameBoy          = "game_boy"
	FamilyPlayStation      = "playstation"
	FamilyXbox             = "xbox"
	FamilyPC               = "pc"
	FamilyOther            = "other"
)

const (
	ManufacturerNintendo  = "Nintendo"
	ManufacturerSony      = "Sony"
	ManufacturerMicrosoft = "Microsoft"
	ManufacturerSega      = "Sega"
	ManufacturerPC        = "PC"
	ManufacturerOther     = "Other"
)

// PlatformFamilies is the closed set of family values, for validators and the
// smart-list rule builder.
var PlatformFamilies = []string{
	FamilyNintendoConsole,
	FamilyNintendoHandheld,
	FamilyGameBoy,
	FamilyPlayStation,
	FamilyXbox,
	FamilyPC,
	FamilyOther,
}

// PlatformMeta classifies one IGDB platform. Generation follows IGDB's own
// generation pages (GB=4, N64=5, PS2=6 … PS5/Series/Switch 2=9); 0 means the
// platform does not map to a home-console generation (PC, DOS, mobile).
// Handheld marks portables, including the hybrid Switch.
type PlatformMeta struct {
	Manufacturer string
	Family       string
	Generation   int
	Handheld     bool
}

// PlatformCatalog maps IGDB platform id → classification. Platforms absent
// from the map are unclassified: reads serve family "other" and a NULL
// generation, never an error.
var PlatformCatalog = map[int64]PlatformMeta{
	// Nintendo home consoles (Switch and Switch 2 are hybrids: console family,
	// handheld form factor).
	18:  {ManufacturerNintendo, FamilyNintendoConsole, 3, false}, // NES
	99:  {ManufacturerNintendo, FamilyNintendoConsole, 3, false}, // Famicom
	19:  {ManufacturerNintendo, FamilyNintendoConsole, 4, false}, // SNES
	58:  {ManufacturerNintendo, FamilyNintendoConsole, 4, false}, // Super Famicom
	4:   {ManufacturerNintendo, FamilyNintendoConsole, 5, false}, // Nintendo 64
	21:  {ManufacturerNintendo, FamilyNintendoConsole, 6, false}, // GameCube
	5:   {ManufacturerNintendo, FamilyNintendoConsole, 7, false}, // Wii
	41:  {ManufacturerNintendo, FamilyNintendoConsole, 8, false}, // Wii U
	130: {ManufacturerNintendo, FamilyNintendoConsole, 8, true},  // Switch
	508: {ManufacturerNintendo, FamilyNintendoConsole, 9, true},  // Switch 2

	// Game Boy line.
	33: {ManufacturerNintendo, FamilyGameBoy, 4, true}, // Game Boy
	22: {ManufacturerNintendo, FamilyGameBoy, 5, true}, // Game Boy Color
	24: {ManufacturerNintendo, FamilyGameBoy, 6, true}, // Game Boy Advance

	// Nintendo handhelds beyond the Game Boy line.
	87:  {ManufacturerNintendo, FamilyNintendoHandheld, 5, true}, // Virtual Boy
	20:  {ManufacturerNintendo, FamilyNintendoHandheld, 7, true}, // DS
	37:  {ManufacturerNintendo, FamilyNintendoHandheld, 8, true}, // 3DS
	137: {ManufacturerNintendo, FamilyNintendoHandheld, 8, true}, // New 3DS

	// PlayStation family, handhelds included.
	7:   {ManufacturerSony, FamilyPlayStation, 5, false}, // PS1
	8:   {ManufacturerSony, FamilyPlayStation, 6, false}, // PS2
	9:   {ManufacturerSony, FamilyPlayStation, 7, false}, // PS3
	48:  {ManufacturerSony, FamilyPlayStation, 8, false}, // PS4
	167: {ManufacturerSony, FamilyPlayStation, 9, false}, // PS5
	38:  {ManufacturerSony, FamilyPlayStation, 7, true},  // PSP
	46:  {ManufacturerSony, FamilyPlayStation, 8, true},  // Vita

	// Xbox family.
	11:  {ManufacturerMicrosoft, FamilyXbox, 6, false}, // Xbox
	12:  {ManufacturerMicrosoft, FamilyXbox, 7, false}, // Xbox 360
	49:  {ManufacturerMicrosoft, FamilyXbox, 8, false}, // Xbox One
	169: {ManufacturerMicrosoft, FamilyXbox, 9, false}, // Xbox Series X|S

	// Sega hardware: kept in the "other" family, manufacturer preserves the
	// house for a future Sega set.
	64: {ManufacturerSega, FamilyOther, 3, false}, // Master System
	29: {ManufacturerSega, FamilyOther, 4, false}, // Mega Drive/Genesis
	30: {ManufacturerSega, FamilyOther, 4, false}, // 32X
	32: {ManufacturerSega, FamilyOther, 5, false}, // Saturn
	23: {ManufacturerSega, FamilyOther, 6, false}, // Dreamcast
	35: {ManufacturerSega, FamilyOther, 4, true},  // Game Gear

	// Computer platforms. Steam Deck has no IGDB platform entity: IGDB files
	// Steam Deck releases under Linux/PC, so Linux covers it here.
	6:  {ManufacturerPC, FamilyPC, 0, false}, // PC (Microsoft Windows)
	3:  {ManufacturerPC, FamilyPC, 0, false}, // Linux
	14: {ManufacturerPC, FamilyPC, 0, false}, // Mac
	13: {ManufacturerPC, FamilyPC, 0, false}, // DOS
	16: {ManufacturerPC, FamilyPC, 0, false}, // Amiga

	// Notable retro and mobile platforms, classified for the retro
	// achievements.
	52: {ManufacturerOther, FamilyOther, 0, false}, // Arcade
	82: {ManufacturerOther, FamilyOther, 0, false}, // Web browser
	59: {ManufacturerOther, FamilyOther, 2, false}, // Atari 2600
	68: {ManufacturerOther, FamilyOther, 2, false}, // ColecoVision
	67: {ManufacturerOther, FamilyOther, 2, false}, // Intellivision
	50: {ManufacturerOther, FamilyOther, 5, false}, // 3DO
	86: {ManufacturerOther, FamilyOther, 4, false}, // TurboGrafx-16
	80: {ManufacturerOther, FamilyOther, 4, false}, // Neo Geo AES
	57: {ManufacturerOther, FamilyOther, 5, true},  // WonderSwan
	61: {ManufacturerOther, FamilyOther, 4, true},  // Atari Lynx
	34: {ManufacturerOther, FamilyOther, 0, false}, // Android
	39: {ManufacturerOther, FamilyOther, 0, false}, // iOS
}
