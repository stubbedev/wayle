package config

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// MediaIconType is the media module's icon mode (MediaIconType).
type MediaIconType string

// Media icon modes, by their config spelling.
const (
	// MediaIconDefault is the static icon-name.
	MediaIconDefault MediaIconType = "default"
	// MediaIconApplication is the player's desktop-entry icon.
	MediaIconApplication MediaIconType = "application"
	// MediaIconSpinningDisc is spinning-disc-icon, turning while playing.
	MediaIconSpinningDisc MediaIconType = "spinning-disc"
	// MediaIconApplicationMapped maps the bus name through player-icons
	// and the built-in table.
	MediaIconApplicationMapped MediaIconType = "application-mapped"
)

// ParseMediaIconType reads icon-type; unknown spellings are errors.
func ParseMediaIconType(raw string) (MediaIconType, error) {
	switch t := MediaIconType(raw); t {
	case MediaIconDefault, MediaIconApplication, MediaIconSpinningDisc, MediaIconApplicationMapped:
		return t, nil
	}
	return "", fmt.Errorf("unknown icon-type %q (want default, application, spinning-disc, or application-mapped)", raw)
}

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

// MediaConfig is [modules.media] (schemas/modules/media/mod.rs).
type MediaConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and LabelMaxLength
	// mirror its label-show and label-max-length.
	Button    ButtonConfig
	Format    string
	LabelShow bool
	// LabelMaxLength truncates the label with an ellipsis; 0 disables.
	LabelMaxLength int
	// Icon carries icon-show, icon-name (the default-mode glyph and
	// every mode's fallback), and icon-color.
	Icon             IconConfig
	IconType         MediaIconType
	SpinningDiscIcon string
	// PlayerIcons are the user's player-icons, sorted by pattern (the
	// BTreeMap order the Rust lookup walks).
	PlayerIcons []IconMapping
	// PlayersIgnored are bus-name substrings kept out of discovery.
	PlayersIgnored []string
	// PlayerPriority are bus-name globs, most preferred first.
	PlayerPriority []string
}

// DefaultsMedia returns the schema defaults.
func DefaultsMedia() MediaConfig {
	return MediaConfig{
		Click:            DefaultsClick(map[string]string{"left-click": "dropdown:media"}),
		Button:           DefaultsButton(buttonColors("auto", "blue", "blue", "bg-surface-elevated", "blue"), TokenBlue, true, 35),
		Format:           "{{ title }} - {{ artist }}",
		LabelShow:        true,
		LabelMaxLength:   35,
		Icon:             DefaultsIcon(true, "ld-music-symbolic"),
		IconType:         MediaIconApplicationMapped,
		SpinningDiscIcon: "ld-disc-3-symbolic",
	}
}

// applyMedia overlays [modules.media].
func applyMedia(md toml.MetaData, prim toml.Primitive) (MediaConfig, error) {
	cfg := DefaultsMedia()
	var doc struct {
		Format           *string            `toml:"format"`
		IconName         *string            `toml:"icon-name"`
		IconType         *string            `toml:"icon-type"`
		SpinningDiscIcon *string            `toml:"spinning-disc-icon"`
		PlayerIcons      *map[string]string `toml:"player-icons"`
		PlayersIgnored   *[]string          `toml:"players-ignored"`
		PlayerPriority   *[]string          `toml:"player-priority"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconType != nil {
		t, err := ParseMediaIconType(*doc.IconType)
		if err != nil {
			return cfg, fmt.Errorf("media: %w", err)
		}
		cfg.IconType = t
	}
	if doc.SpinningDiscIcon != nil {
		cfg.SpinningDiscIcon = *doc.SpinningDiscIcon
	}
	if doc.PlayerIcons != nil {
		for pattern, icon := range *doc.PlayerIcons {
			cfg.PlayerIcons = append(cfg.PlayerIcons, IconMapping{Pattern: pattern, Icon: icon})
		}
		sort.Slice(cfg.PlayerIcons, func(i, j int) bool { return cfg.PlayerIcons[i].Pattern < cfg.PlayerIcons[j].Pattern })
	}
	if doc.PlayersIgnored != nil {
		cfg.PlayersIgnored = *doc.PlayersIgnored
	}
	if doc.PlayerPriority != nil {
		cfg.PlayerPriority = *doc.PlayerPriority
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, &cfg.LabelMaxLength)
	button.mirrorIcon(&cfg.Icon)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
