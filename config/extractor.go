package config

import "reflect"

// ColorExtractorConfig is the slice of [styling] that drives wallpaper
// color extraction (crates/wayle-shell/src/watchers/color_extractor.rs):
// the provider, the theming monitor, and the matugen-/wallust-/pywal-
// tool parameters. It is an inline group: its keys sit directly in
// [styling].
type ColorExtractorConfig struct {
	// Theme provider (wayle, matugen, pywal, wallust).
	ThemeProvider ThemeProvider `cfg:"theme-provider"`
	// Monitor whose wallpaper drives color extraction. Empty uses the first available.
	ThemingMonitor string `cfg:"theming-monitor"`
	// Matugen color scheme type.
	MatugenScheme MatugenScheme `cfg:"matugen-scheme"`
	// Matugen contrast level (-1.0 to 1.0).
	MatugenContrast SignedNormalizedF64 `cfg:"matugen-contrast"`
	// Matugen source color index (0-3).
	MatugenSourceColor uint8 `cfg:"matugen-source-color"`
	// Matugen light mode.
	MatugenLight bool `cfg:"matugen-light"`
	// Wallust palette mode.
	WallustPalette WallustPalette `cfg:"wallust-palette"`
	// Wallust saturation boost (0-100, 0 disables).
	WallustSaturation Percentage `cfg:"wallust-saturation"`
	// Wallust contrast checking against background.
	WallustCheckContrast bool `cfg:"wallust-check-contrast"`
	// Wallust image sampling backend.
	WallustBackend WallustBackend `cfg:"wallust-backend"`
	// Wallust color space for dominant color selection.
	WallustColorspace WallustColorspace `cfg:"wallust-colorspace"`
	// Apply wallust colors to terminals and external tools.
	WallustApplyGlobally bool `cfg:"wallust-apply-globally"`
	// Pywal saturation adjustment (0.0-1.0).
	PywalSaturation NormalizedF64 `cfg:"pywal-saturation"`
	// Pywal minimum contrast ratio (1.0-21.0).
	PywalContrast PywalContrast `cfg:"pywal-contrast"`
	// Pywal light mode.
	PywalLight bool `cfg:"pywal-light"`
	// Apply pywal colors to terminals and external tools.
	PywalApplyGlobally bool `cfg:"pywal-apply-globally"`
}

// DefaultsColorExtractor returns the schema defaults.
func DefaultsColorExtractor() ColorExtractorConfig {
	return ColorExtractorConfig{
		ThemeProvider:        ThemeProviderWayle,
		MatugenScheme:        MatugenSchemeTonalSpot,
		WallustPalette:       WallustPaletteDark16,
		WallustCheckContrast: true,
		WallustBackend:       WallustBackendFastresize,
		WallustColorspace:    WallustColorspaceLabmixed,
		WallustApplyGlobally: true,
		PywalSaturation:      0.05,
		PywalContrast:        3,
		PywalApplyGlobally:   true,
	}
}

// MatugenScheme is a color-extractor parameter (styling/types/extractor.rs).
//
// Matugen color scheme type.
type MatugenScheme string

// MatugenScheme values.
const (
	// Adapts to image content.
	MatugenSchemeContent MatugenScheme = "content"
	// Bold, dramatic palette.
	MatugenSchemeExpressive MatugenScheme = "expressive"
	// Stays close to source colors.
	MatugenSchemeFidelity MatugenScheme = "fidelity"
	// Playful multi-color palette.
	MatugenSchemeFruitSalad MatugenScheme = "fruit-salad"
	// Single-hue grayscale palette.
	MatugenSchemeMonochrome MatugenScheme = "monochrome"
	// Muted, understated palette.
	MatugenSchemeNeutral MatugenScheme = "neutral"
	// Broad hue spread.
	MatugenSchemeRainbow MatugenScheme = "rainbow"
	// Balanced Material You default.
	MatugenSchemeTonalSpot MatugenScheme = "tonal-spot"
	// High-saturation palette.
	MatugenSchemeVibrant MatugenScheme = "vibrant"
)

// WallustPalette is a color-extractor parameter (styling/types/extractor.rs).
//
// Wallust palette mode.
type WallustPalette string

// WallustPalette values.
const (
	// 8 dark colors with 16-color trick.
	WallustPaletteDark16 WallustPalette = "dark16"
	// 8 dark colors, dark background and light contrast.
	WallustPaletteDark WallustPalette = "dark"
	// Dark with complementary counterparts.
	WallustPaletteDarkcomp WallustPalette = "darkcomp"
	// Dark complementary with 16-color trick.
	WallustPaletteDarkcomp16 WallustPalette = "darkcomp16"
	// Dark with hard hue colors.
	WallustPaletteHarddark WallustPalette = "harddark"
	// Hard dark with 16-color trick.
	WallustPaletteHarddark16 WallustPalette = "harddark16"
	// Hard dark complementary variant.
	WallustPaletteHarddarkcomp WallustPalette = "harddarkcomp"
	// Hard dark complementary with 16-color trick.
	WallustPaletteHarddarkcomp16 WallustPalette = "harddarkcomp16"
	// Light background, dark foreground.
	WallustPaletteLight WallustPalette = "light"
	// Light with 16-color trick.
	WallustPaletteLight16 WallustPalette = "light16"
	// Light with complementary colors.
	WallustPaletteLightcomp WallustPalette = "lightcomp"
	// Light complementary with 16-color trick.
	WallustPaletteLightcomp16 WallustPalette = "lightcomp16"
	// Lightest colors with dark background.
	WallustPaletteSoftdark WallustPalette = "softdark"
	// Soft dark with 16-color trick.
	WallustPaletteSoftdark16 WallustPalette = "softdark16"
	// Soft dark complementary variant.
	WallustPaletteSoftdarkcomp WallustPalette = "softdarkcomp"
	// Soft dark complementary with 16-color trick.
	WallustPaletteSoftdarkcomp16 WallustPalette = "softdarkcomp16"
	// Light with soft pastel colors.
	WallustPaletteSoftlight WallustPalette = "softlight"
	// Soft light with 16-color trick.
	WallustPaletteSoftlight16 WallustPalette = "softlight16"
	// Soft light with complementary colors.
	WallustPaletteSoftlightcomp WallustPalette = "softlightcomp"
	// Soft light complementary with 16-color trick.
	WallustPaletteSoftlightcomp16 WallustPalette = "softlightcomp16"
	// ANSI-ordered dark palette for LS_COLORS.
	WallustPaletteAnsidark WallustPalette = "ansidark"
	// ANSI dark with 16-color trick.
	WallustPaletteAnsidark16 WallustPalette = "ansidark16"
)

// WallustBackend is a color-extractor parameter (styling/types/extractor.rs).
//
// Wallust image sampling backend.
type WallustBackend string

// WallustBackend values.
const (
	// Reads every pixel.
	WallustBackendFull WallustBackend = "full"
	// Resizes image before sampling.
	WallustBackendResized WallustBackend = "resized"
	// Uses ImageMagick convert (pywal method).
	WallustBackendWal WallustBackend = "wal"
	// Fixed 512x512 thumbnail.
	WallustBackendThumb WallustBackend = "thumb"
	// SIMD-accelerated resize.
	WallustBackendFastresize WallustBackend = "fastresize"
	// K-means clustering.
	WallustBackendKmeans WallustBackend = "kmeans"
)

// WallustColorspace is a color-extractor parameter (styling/types/extractor.rs).
//
// Wallust color space for dominant color selection.
type WallustColorspace string

// WallustColorspace values.
const (
	// CIELAB perceptual color space.
	WallustColorspaceLab WallustColorspace = "lab"
	// LAB with mixing for sparse images.
	WallustColorspaceLabmixed WallustColorspace = "labmixed"
	// Cylindrical LAB (hue/chroma/lightness).
	WallustColorspaceLch WallustColorspace = "lch"
	// LCH with mixing.
	WallustColorspaceLchmixed WallustColorspace = "lchmixed"
	// LCH mapped to ANSI color ordering.
	WallustColorspaceLchansi WallustColorspace = "lchansi"
)

var (
	_ = registerEnum(MatugenSchemeContent, MatugenSchemeExpressive, MatugenSchemeFidelity, MatugenSchemeFruitSalad,
		MatugenSchemeMonochrome, MatugenSchemeNeutral, MatugenSchemeRainbow, MatugenSchemeTonalSpot, MatugenSchemeVibrant)
	_ = registerEnum(WallustPaletteDark16, WallustPaletteDark, WallustPaletteDarkcomp, WallustPaletteDarkcomp16,
		WallustPaletteHarddark, WallustPaletteHarddark16, WallustPaletteHarddarkcomp, WallustPaletteHarddarkcomp16,
		WallustPaletteLight, WallustPaletteLight16, WallustPaletteLightcomp, WallustPaletteLightcomp16,
		WallustPaletteSoftdark, WallustPaletteSoftdark16, WallustPaletteSoftdarkcomp, WallustPaletteSoftdarkcomp16,
		WallustPaletteSoftlight, WallustPaletteSoftlight16, WallustPaletteSoftlightcomp, WallustPaletteSoftlightcomp16,
		WallustPaletteAnsidark, WallustPaletteAnsidark16)
	_ = registerEnum(WallustBackendFull, WallustBackendResized, WallustBackendWal, WallustBackendThumb,
		WallustBackendFastresize, WallustBackendKmeans)
	_ = registerEnum(WallustColorspaceLab, WallustColorspaceLabmixed, WallustColorspaceLch,
		WallustColorspaceLchmixed, WallustColorspaceLchansi)
)

// CLIValue is the value of matugen's --type flag ("scheme-tonal-spot").
func (m MatugenScheme) CLIValue() string { return "scheme-" + string(m) }

// IsLight reports whether the palette produces a light background.
func (p WallustPalette) IsLight() bool {
	switch p {
	case WallustPaletteLight, WallustPaletteLight16, WallustPaletteLightcomp, WallustPaletteLightcomp16,
		WallustPaletteSoftlight, WallustPaletteSoftlight16, WallustPaletteSoftlightcomp, WallustPaletteSoftlightcomp16:
		return true
	}
	return false
}

// SignedNormalizedF64 is a -1..1 float (styling/types/extractor.rs).
//
// Floating-point value clamped to -1.0 to 1.0.
type SignedNormalizedF64 float64

// UnmarshalConfig implements Unmarshaler: out of range clamps with the
// Rust warning.
func (n *SignedNormalizedF64) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, SignedNormalizedF64(-1), SignedNormalizedF64(1), "signed normalized value")
	*n = raw
	return err
}

// MarshalConfig implements Marshaler.
func (n SignedNormalizedF64) MarshalConfig() any { return float64(n) }

func (SignedNormalizedF64) configSchema(*schemaGen) Schema {
	return rangedSchema(reflect.TypeFor[SignedNormalizedF64](), -1.0, 1.0)
}

// PywalContrast is pywal's minimum contrast ratio, the WCAG range
// (styling/types/extractor.rs).
//
// Pywal contrast ratio clamped to 1.0-21.0 (WCAG range).
type PywalContrast float64

// UnmarshalConfig implements Unmarshaler: out of range clamps with the
// Rust warning.
func (c *PywalContrast) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, PywalContrast(1), PywalContrast(21), "pywal contrast")
	*c = raw
	return err
}

// MarshalConfig implements Marshaler.
func (c PywalContrast) MarshalConfig() any { return float64(c) }

func (PywalContrast) configSchema(*schemaGen) Schema {
	return rangedSchema(reflect.TypeFor[PywalContrast](), 1.0, 21.0)
}
