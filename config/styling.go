package config

// StylingConfig is the [styling] section
// (crates/wayle-config/src/schemas/styling/mod.rs).
//
// Theme, palette, and rounding tokens applied shell-wide. Changes recompile the stylesheet.
type StylingConfig struct {
	// Scale multiplier for dropdowns, popovers, and dialogs.
	Scale ScaleFactor `cfg:"scale"`
	// Corner rounding for dropdowns, popovers, and dialogs.
	Rounding RoundingLevel `cfg:"rounding"`
	// Light/dark appearance mode. `Auto` follows each provider / the configured
	// palette; `Light`/`Dark` force the mode (static palettes swap to their
	// built-in light/dark variant when one exists).
	Appearance Appearance `cfg:"appearance"`
	// ColorExtractor is theme-provider, theming-monitor, and the
	// matugen-/wallust-/pywal- keys.
	ColorExtractor ColorExtractorConfig `cfg:",inline"`
	// Active color palette.
	Palette PaletteConfig `cfg:"palette"`
	// Currently active theme preset name, and/or the base for the
	// palette when the palette has been modified. Persisted backing
	// state for the theme selector; not a labeled setting of its own.
	PaletteBaseTheme string `cfg:"palette_base_theme,noi18n"`
	// Available is the discovered themes, populated at runtime from
	// the built-ins and themes/ (never read from a config file).
	Available []ThemeEntry `cfg:"-"`
}

// DefaultsStyling returns the schema defaults.
func DefaultsStyling() StylingConfig {
	return StylingConfig{
		Scale:          1.01,
		Rounding:       RoundingSm,
		Appearance:     AppearanceAuto,
		ColorExtractor: DefaultsColorExtractor(),
		Palette:        paletteConfigOf(wayleTheme),
	}
}

// ActivePalette is the palette the static (wayle) provider renders:
// the configured [styling.palette], or — when the appearance forces
// light or dark and palette-base-theme names a built-in family with
// that variant — the variant's palette (theme_provider.rs
// resolve_static_palette).
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

// PaletteConfig is the [styling.palette] table (styling/palette.rs).
//
// Color palette configuration for the active theme.
type PaletteConfig struct {
	// Base background color (darkest).
	Bg HexColor `cfg:"bg"`
	// Card and sidebar background.
	Surface HexColor `cfg:"surface"`
	// Raised element background.
	Elevated HexColor `cfg:"elevated"`
	// Primary text color.
	Fg HexColor `cfg:"fg"`
	// Secondary text color.
	FgMuted HexColor `cfg:"fg-muted"`
	// Accent color for interactive elements.
	Primary HexColor `cfg:"primary"`
	// Red semantic color.
	Red HexColor `cfg:"red"`
	// Yellow semantic color.
	Yellow HexColor `cfg:"yellow"`
	// Green semantic color.
	Green HexColor `cfg:"green"`
	// Blue semantic color.
	Blue HexColor `cfg:"blue"`
}

// Appearance is the light/dark mode (styling/types/color.rs).
//
// Light/dark appearance mode.
//
// `Auto` keeps each provider's own light setting and the configured static
// palette as-is. `Light`/`Dark` force the mode: dynamic providers switch their
// light flag, and the static palette swaps to its built-in light/dark variant
// when one exists.
type Appearance string

// Appearance modes.
const (
	// Follow each provider's own setting / the configured palette.
	AppearanceAuto Appearance = "auto"
	// Force the light variant.
	AppearanceLight Appearance = "light"
	// Force the dark variant.
	AppearanceDark Appearance = "dark"
)

var _ = registerEnum(AppearanceAuto, AppearanceLight, AppearanceDark)

// ForcedLight reports whether the mode forces light (true) or dark
// (false); ok is false for Auto.
func (a Appearance) ForcedLight() (light, ok bool) {
	switch a {
	case AppearanceLight:
		return true, true
	case AppearanceDark:
		return false, true
	}
	return false, false
}

// ThemeProvider is where palette values come from (styling/types/color.rs).
//
// Source of color palette values.
//
// Dynamic providers (Matugen, Pywal, Wallust) inject palette tokens at runtime.
type ThemeProvider string

// Theme providers; Wayle is the default.
const (
	// Static theming using Wayle's built-in palettes.
	ThemeProviderWayle ThemeProvider = "wayle"
	// Dynamic theming via Matugen.
	ThemeProviderMatugen ThemeProvider = "matugen"
	// Dynamic theming via Pywal.
	ThemeProviderPywal ThemeProvider = "pywal"
	// Dynamic theming via Wallust.
	ThemeProviderWallust ThemeProvider = "wallust"
)

var _ = registerEnum(ThemeProviderWayle, ThemeProviderMatugen, ThemeProviderPywal, ThemeProviderWallust)
