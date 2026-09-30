package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Appearance is the light/dark mode: auto follows each provider and the
// configured palette, light and dark force the mode
// (crates/wayle-config/src/schemas/styling/types/color.rs).
type Appearance string

// Appearance modes.
const (
	AppearanceAuto  Appearance = "auto"
	AppearanceLight Appearance = "light"
	AppearanceDark  Appearance = "dark"
)

// UnmarshalText decodes and validates an appearance mode.
func (a *Appearance) UnmarshalText(text []byte) error {
	v, err := parseEnum(text, "appearance", AppearanceAuto, AppearanceLight, AppearanceDark)
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// ForcedLight is the forced mode: (true, true) for light, (false, true)
// for dark, and ok false for auto (Appearance::forced_light).
func (a Appearance) ForcedLight() (light, ok bool) {
	switch a {
	case AppearanceLight:
		return true, true
	case AppearanceDark:
		return false, true
	}
	return false, false
}

// PaletteConfig is the [styling.palette] section: the ten palette
// colors of the active theme (crates/wayle-config/src/schemas/styling/
// palette.rs).
type PaletteConfig struct {
	Bg, Surface, Elevated HexColor
	Fg, FgMuted           HexColor
	Primary               HexColor
	Red, Yellow           HexColor
	Green, Blue           HexColor
}

// StylingConfig is the theme half of the [styling] section: the
// dropdown/popover scale and rounding, the appearance mode, the palette,
// and its base theme (crates/wayle-config/src/schemas/styling/mod.rs).
// The provider and the extractor keys of the same section live in
// ColorExtractorConfig.
type StylingConfig struct {
	// Scale multiplies dropdowns, popovers, and dialogs (0.25-3.0).
	Scale float64
	// Rounding is the corner rounding of dropdowns, popovers, dialogs.
	Rounding   RoundingLevel
	Appearance Appearance
	Palette    PaletteConfig
	// PaletteBaseTheme is the active theme preset's name, the base the
	// palette was edited from; it picks the light/dark variant a forced
	// appearance swaps to.
	PaletteBaseTheme string
}

// DefaultsStyling returns the schema defaults: the wayle theme.
func DefaultsStyling() StylingConfig {
	return StylingConfig{
		Scale:      1.01,
		Rounding:   RoundingSm,
		Appearance: AppearanceAuto,
		Palette:    paletteConfigOf(wayleTheme),
	}
}

// ActivePalette assembles the palette from the color fields under the
// configured appearance: when light or dark is forced and the base theme
// has a built-in variant for that mode, the variant's palette is
// returned instead. Non-destructive — PaletteBaseTheme is unchanged, so
// auto restores the configured colors. This is StylingConfig::palette
// (the Go field of that name holds the colors).
func (s StylingConfig) ActivePalette() Palette {
	base := Palette{
		Bg: s.Palette.Bg.String(), Surface: s.Palette.Surface.String(), Elevated: s.Palette.Elevated.String(),
		Fg: s.Palette.Fg.String(), FgMuted: s.Palette.FgMuted.String(), Primary: s.Palette.Primary.String(),
		Red: s.Palette.Red.String(), Yellow: s.Palette.Yellow.String(),
		Green: s.Palette.Green.String(), Blue: s.Palette.Blue.String(),
	}
	light, forced := s.Appearance.ForcedLight()
	if !forced || s.PaletteBaseTheme == "" {
		return base
	}
	if variant, ok := AppearanceVariant(s.PaletteBaseTheme, light); ok {
		if p, ok := PaletteByName(variant); ok {
			return p
		}
	}
	return base
}

type paletteDoc struct {
	Bg       *HexColor `toml:"bg"`
	Surface  *HexColor `toml:"surface"`
	Elevated *HexColor `toml:"elevated"`
	Fg       *HexColor `toml:"fg"`
	FgMuted  *HexColor `toml:"fg-muted"`
	Primary  *HexColor `toml:"primary"`
	Red      *HexColor `toml:"red"`
	Yellow   *HexColor `toml:"yellow"`
	Green    *HexColor `toml:"green"`
	Blue     *HexColor `toml:"blue"`
}

// applyStyling overlays the theme keys of [styling] onto the defaults.
// scale clamps out-of-range values with a warning, as ScaleFactor
// deserializes (and as ColorExtractorConfig treats its ranged keys); a
// bad rounding, appearance, or palette color is a load error.
func applyStyling(md toml.MetaData, prim toml.Primitive) (StylingConfig, error) {
	cfg := DefaultsStyling()
	var doc struct {
		Scale            *float64    `toml:"scale"`
		Rounding         *string     `toml:"rounding"`
		Appearance       *Appearance `toml:"appearance"`
		Palette          *paletteDoc `toml:"palette"`
		PaletteBaseTheme *string     `toml:"palette-base-theme"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, fmt.Errorf("styling: %w", err)
	}
	if doc.Scale != nil {
		cfg.Scale = clampWarn("scale", *doc.Scale, 0.25, 3.0)
	}
	if doc.Rounding != nil {
		level := RoundingLevel(*doc.Rounding)
		if !validRounding[level] {
			return cfg, fmt.Errorf("styling: invalid rounding %q (want none|sm|md|lg|full)", *doc.Rounding)
		}
		cfg.Rounding = level
	}
	setIf(doc.Appearance, &cfg.Appearance)
	setIf(doc.PaletteBaseTheme, &cfg.PaletteBaseTheme)
	if p := doc.Palette; p != nil {
		setIf(p.Bg, &cfg.Palette.Bg)
		setIf(p.Surface, &cfg.Palette.Surface)
		setIf(p.Elevated, &cfg.Palette.Elevated)
		setIf(p.Fg, &cfg.Palette.Fg)
		setIf(p.FgMuted, &cfg.Palette.FgMuted)
		setIf(p.Primary, &cfg.Palette.Primary)
		setIf(p.Red, &cfg.Palette.Red)
		setIf(p.Yellow, &cfg.Palette.Yellow)
		setIf(p.Green, &cfg.Palette.Green)
		setIf(p.Blue, &cfg.Palette.Blue)
	}
	return cfg, nil
}

// setIf overlays one decoded optional key onto its default.
func setIf[T any](raw *T, dst *T) {
	if raw != nil {
		*dst = *raw
	}
}
