package config

import (
	"slices"
	"strings"
)

// MediaConfig is ported from crates/wayle-config/src/schemas/modules/media/mod.rs.
//
// Now-playing title and playback controls for the active MPRIS player.
type MediaConfig struct {
	// Icon display mode.
	IconType MediaIconType `cfg:"icon-type"`
	// Custom player-to-icon mappings for application-mapped mode.
	//
	// Keys are glob patterns matching MPRIS bus names, values are icon names
	// from the installed icon set. These override built-in mappings when
	// matched.
	//
	// ## Example
	//
	// ```toml
	// [modules.media.player-icons]
	// "*spotify*" = "si-spotify-symbolic"
	// "*firefox*" = "ld-globe-symbolic"
	// "*.mpv" = "ld-play-circle-symbolic"
	// ```
	PlayerIcons map[string]string `cfg:"player-icons"`
	// Player bus name patterns to exclude from discovery. Requires a restart
	// to take effect.
	//
	// ## Example
	//
	// ```toml
	// [modules.media]
	// players-ignored = ["*chromium*", "*discord*"]
	// ```
	PlayersIgnored []string `cfg:"players-ignored"`
	// Preferred player priority order as glob patterns matching bus names.
	//
	// When no player is manually selected, this determines which player
	// becomes active. Patterns are checked in order; first match wins.
	// If no pattern matches, the first playing player is selected.
	//
	// ## Example
	//
	// ```toml
	// [modules.media]
	// player-priority = ["*spotify*", "*firefox*"]
	// ```
	PlayerPriority []string `cfg:"player-priority"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ title }}` - Track title
	// - `{{ artist }}` - Artist name(s)
	// - `{{ album }}` - Album name
	// - `{{ status }}` - Playback status text (Playing, Paused, Stopped)
	// - `{{ status_icon }}` - Playback status icon character
	//
	// ## Examples
	//
	// - `"{{ title }} - {{ artist }}"` - "Bohemian Rhapsody - Queen"
	// - `"{{ status_icon }} {{ title }}"` - "▶ Bohemian Rhapsody"
	// - `"{{ artist }}: {{ title }} ({{ album }})"` - "Queen: Bohemian Rhapsody (A Night at the Opera)"
	Format string `cfg:"format"`
	// Symbolic icon name for default mode.
	IconName string `cfg:"icon-name"`
	// Icon shown for spinning-disc mode.
	SpinningDiscIcon string `cfg:"spinning-disc-icon"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display text label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsMedia returns the schema defaults.
func DefaultsMedia() MediaConfig {
	return MediaConfig{
		IconType:         MediaIconTypeApplicationMapped,
		PlayerIcons:      map[string]string{},
		PlayersIgnored:   []string{},
		PlayerPriority:   []string{},
		Format:           "{{ title }} - {{ artist }}",
		IconName:         "ld-music-symbolic",
		SpinningDiscIcon: "ld-disc-3-symbolic",
		BorderShow:       false,
		BorderColor:      mustColor("blue"),
		IconShow:         true,
		IconColor:        mustColor("auto"),
		IconBgColor:      mustColor("blue"),
		LabelShow:        true,
		LabelColor:       mustColor("blue"),
		LabelMaxLength:   35,
		ButtonBgColor:    mustColor("bg-surface-elevated"),
		LeftClick:        ParseClickAction("dropdown:media"),
		RightClick:       ClickAction{},
		MiddleClick:      ClickAction{},
		ScrollUp:         ClickAction{},
		ScrollDown:       ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c MediaConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// MediaIconType is ported from crates/wayle-config/src/schemas/modules/media/mod.rs.
//
// Icon display mode for the media module.
type MediaIconType string

// MediaIconType values.
const (
	// Static icon from icon-name field.
	MediaIconTypeDefault MediaIconType = "default"
	// Dynamic icon from media player's desktop entry, falling back to icon-name.
	MediaIconTypeApplication MediaIconType = "application"
	// Spinning disc icon that animates during playback. Uses slightly more CPU.
	MediaIconTypeSpinningDisc MediaIconType = "spinning-disc"
	// Maps player to icon via glob patterns, with built-in mappings for common players.
	MediaIconTypeApplicationMapped MediaIconType = "application-mapped"
)

var _ = registerEnum(MediaIconTypeDefault, MediaIconTypeApplication, MediaIconTypeSpinningDisc, MediaIconTypeApplicationMapped)

// IconMapping is one glob pattern -> icon pair.
type IconMapping struct {
	Pattern string
	Icon    string
}

// MediaBuiltinIcons is the schema's BUILTIN_MAPPINGS, verbatim and in
// order: the first pattern matching the bus name wins.
var MediaBuiltinIcons = []IconMapping{
	{"*spotify*", "si-spotify-symbolic"},
	{"*tidal*", "si-tidal-symbolic"},
	{"*vlc*", "si-vlcmediaplayer-symbolic"},
	{"*mpv*", "si-mpv-symbolic"},
	{"*kodi*", "si-kodi-symbolic"},
	{"*celluloid*", "si-mpv-symbolic"},
	{"*jellyfin*", "si-jellyfin-symbolic"},
	{"*firefox*", "si-firefox-symbolic"},
	{"*librewolf*", "si-librewolf-symbolic"},
	{"*floorp*", "si-floorp-symbolic"},
	{"*zen*", "si-zenbrowser-symbolic"},
	{"*chrom*", "si-googlechrome-symbolic"},
	{"*brave*", "si-brave-symbolic"},
	{"*vivaldi*", "si-vivaldi-symbolic"},
	{"*opera*", "si-opera-symbolic"},
	{"*edge*", "tb-brand-edge-symbolic"},
	{"*tor*", "si-torbrowser-symbolic"},
	{"*helium*", "si-heliumbrowser-symbolic"},
}

// PlayerIconMappings is player-icons in pattern order, the BTreeMap
// order the Rust lookup walks.
func (c MediaConfig) PlayerIconMappings() []IconMapping {
	out := make([]IconMapping, 0, len(c.PlayerIcons))
	for pattern, icon := range c.PlayerIcons {
		out = append(out, IconMapping{Pattern: pattern, Icon: icon})
	}
	slices.SortFunc(out, func(a, b IconMapping) int { return strings.Compare(a.Pattern, b.Pattern) })
	return out
}
