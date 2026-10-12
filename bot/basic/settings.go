package basic

// Settings is what the client tells the server about itself (the client
// information packet) and the brand it presents.
type Settings struct {
	Locale             string // the client's language, as the game names it: "en_us", "de_de", …
	ViewDistance       int    // in chunks
	ChatMode           int    // which chat the server sends: 0 all, 1 system messages and commands only, 2 none
	ChatColors         bool   // whether chat colors are shown
	DisplayedSkinParts uint8  // the skin layers shown: Jacket, LeftSleeve, … or-ed together
	MainHand           int    // 0 left, 1 right

	// Enables filtering of text on signs and written book titles.
	// Currently, always false (i.e. the filtering is disabled)
	EnableTextFiltering bool
	AllowListing        bool

	// ParticleStatus controls which particles are shown.
	// 0 = all, 1 = decreased, 2 = minimal.
	// Added in 1.21.4 (protocol 769).
	ParticleStatus int

	// The brand string presented to the server.
	Brand string
}

// Used by Settings.DisplayedSkinParts.
// For each bit set if shows match part.
const (
	_ = 1 << iota
	Jacket
	LeftSleeve
	RightSleeve
	LeftPantsLeg
	RightPantsLeg
	Hat
)

// DefaultSettings are the default settings of client
var DefaultSettings = Settings{
	Locale:             "en_us", // the vanilla client's default
	ViewDistance:       15,
	ChatMode:           0,
	DisplayedSkinParts: Jacket | LeftSleeve | RightSleeve | LeftPantsLeg | RightPantsLeg | Hat,
	MainHand:           1,

	EnableTextFiltering: false,
	AllowListing:        true,
	ParticleStatus:      0, // all

	Brand: "vanilla",
}
