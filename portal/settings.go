package portal

import (
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/dbusx"
)

// SettingsIface serves org.freedesktop.appearance from the live styling
// config, so GTK, Qt and Chromium follow wayle's theme (settings.rs).
const SettingsIface = "org.freedesktop.impl.portal.Settings"

// AppearanceNS is the cross-desktop namespace every toolkit reads.
const AppearanceNS = "org.freedesktop.appearance"

// The served appearance keys.
const (
	keyColorScheme   = "color-scheme"
	keyAccentColor   = "accent-color"
	keyContrast      = "contrast"
	keyReducedMotion = "reduced-motion"
)

// appearanceKeys is the served keys in ReadAll's order; each reads
// its value from one config snapshot.
var appearanceKeys = []struct {
	key  string
	read func(*config.Config) any
}{
	{keyColorScheme, func(c *config.Config) any { return colorScheme(c.Styling) }},
	{keyAccentColor, func(c *config.Config) any { return accentColor(c.Styling.Palette.Primary) }},
	{keyContrast, func(c *config.Config) any { return boolU32(c.Styling.ColorExtractor.MatugenContrast > 0) }},
	{keyReducedMotion, func(c *config.Config) any { return boolU32(!c.Animations.Enabled) }},
}

// settingSources are the config values SettingChanged follows, each
// with the key it re-emits (spawn_watcher's five watch streams). Under
// Auto the scheme comes from the palette background, so a palette swap
// re-emits color-scheme too.
var settingSources = []struct {
	key     string
	changed func(old, new *config.Config) bool
}{
	{keyColorScheme, func(o, n *config.Config) bool { return o.Styling.Appearance != n.Styling.Appearance }},
	{keyColorScheme, func(o, n *config.Config) bool { return o.Styling.Palette.Bg != n.Styling.Palette.Bg }},
	{keyAccentColor, func(o, n *config.Config) bool { return o.Styling.Palette.Primary != n.Styling.Palette.Primary }},
	{keyContrast, func(o, n *config.Config) bool {
		return o.Styling.ColorExtractor.MatugenContrast != n.Styling.ColorExtractor.MatugenContrast
	}},
	{keyReducedMotion, func(o, n *config.Config) bool { return o.Animations.Enabled != n.Animations.Enabled }},
}

// colorScheme is 0 no preference, 1 dark, 2 light. Auto states no
// preference, so the effective scheme is read off the active palette's
// background luminance, or apps would stay light under a dark palette.
func colorScheme(s config.StylingConfig) uint32 {
	switch s.Appearance {
	case config.AppearanceDark:
		return 1
	case config.AppearanceLight:
		return 2
	}
	bg, err := config.ParseHexColor(s.ActivePalette().Bg)
	if err != nil {
		return 0
	}
	r, g, b := rgb(bg)
	if 0.2126*r+0.7152*g+0.0722*b < 0.5 {
		return 1
	}
	return 2
}

// accentColor is the palette primary as a (ddd) sRGB tuple in [0, 1].
func accentColor(primary config.HexColor) accent {
	r, g, b := rgb(primary)
	return accent{r, g, b}
}

// accent marshals as the spec's (ddd) structure.
type accent struct{ R, G, B float64 }

func rgb(c config.HexColor) (r, g, b float64) {
	r8, g8, b8, _ := c.RGBA()
	return float64(r8) / 255, float64(g8) / 255, float64(b8) / 255
}

func boolU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// settings carries the interface's D-Bus methods.
type settings struct{ cfg *config.Service }

func settingsIface(cfg *config.Service) dbusx.Interface {
	return dbusx.Interface{
		Name:       SettingsIface,
		Methods:    settings{cfg},
		Properties: dbusx.Getters{"version": func() any { return uint32(2) }},
	}
}

// ReadAll returns every served namespace one of namespaces prefixes
// (empty is all).
func (s settings) ReadAll(namespaces []string) (map[string]Vardict, *dbus.Error) {
	out := map[string]Vardict{}
	if namespaceMatches(namespaces, AppearanceNS) {
		cfg := s.cfg.Config()
		values := Vardict{}
		for _, k := range appearanceKeys {
			values[k.key] = dbus.MakeVariant(k.read(cfg))
		}
		out[AppearanceNS] = values
	}
	return out, nil
}

// Read is the deprecated single-key read older frontends still call.
func (s settings) Read(namespace, key string) (dbus.Variant, *dbus.Error) {
	return s.ReadOne(namespace, key)
}

// ReadOne reads one key of one namespace.
func (s settings) ReadOne(namespace, key string) (dbus.Variant, *dbus.Error) {
	if namespace == AppearanceNS {
		if v, ok := readAppearance(s.cfg.Config(), key); ok {
			return dbus.MakeVariant(v), nil
		}
	}
	return dbus.Variant{}, dbusx.Failed("unknown setting " + namespace + "/" + key)
}

func readAppearance(cfg *config.Config, key string) (any, bool) {
	for _, k := range appearanceKeys {
		if k.key == key {
			return k.read(cfg), true
		}
	}
	return nil, false
}

// watchSettings emits SettingChanged for every followed source that
// changes, after one emission per source for the current values (each
// Rust watch stream yields its value first). The returned func stops.
func watchSettings(conn *dbus.Conn, cfg *config.Service) (stop func()) {
	emit := func(c *config.Config, key string) {
		v, _ := readAppearance(c, key)
		_ = conn.Emit(ObjectPath, SettingsIface+".SettingChanged", AppearanceNS, key, dbus.MakeVariant(v))
	}
	stop = cfg.Subscribe(func(old, new *config.Config) {
		for _, src := range settingSources {
			if src.changed(old, new) {
				emit(new, src.key)
			}
		}
	})
	current := cfg.Config()
	for _, src := range settingSources {
		emit(current, src.key)
	}
	return stop
}

// namespaceMatches reports whether wanted is served for the requested
// namespaces: every one when none is named, else one that prefixes it.
func namespaceMatches(namespaces []string, wanted string) bool {
	if len(namespaces) == 0 {
		return true
	}
	for _, ns := range namespaces {
		if strings.HasPrefix(wanted, ns) {
			return true
		}
	}
	return false
}
