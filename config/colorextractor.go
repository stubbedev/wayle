package config

import (
	"fmt"
	"log"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// ThemeProvider is [styling] theme-provider: where palette values come
// from. Wayle is the static built-in palettes; the others are the
// dynamic providers fed by wallpaper color extraction.
type ThemeProvider string

// Theme providers; Wayle is the default.
const (
	ThemeWayle   ThemeProvider = "wayle"
	ThemeMatugen ThemeProvider = "matugen"
	ThemePywal   ThemeProvider = "pywal"
	ThemeWallust ThemeProvider = "wallust"
)

// Tool is the extractor the provider needs (build_extractor_config);
// Wayle extracts nothing.
func (p ThemeProvider) Tool() extract.Tool {
	switch p {
	case ThemeMatugen:
		return extract.Matugen
	case ThemePywal:
		return extract.Pywal
	case ThemeWallust:
		return extract.Wallust
	}
	return extract.None
}

// ColorExtractorConfig is the slice of [styling] that drives wallpaper
// color extraction: the provider, the theming monitor, and the
// matugen-/wallust-/pywal- tool parameters
// (crates/wayle-shell/src/watchers/color_extractor.rs). The rest of
// [styling] belongs to the theme providers.
type ColorExtractorConfig struct {
	ThemeProvider ThemeProvider
	// ThemingMonitor is the monitor whose wallpaper is extracted; empty
	// uses the lowest-named one.
	ThemingMonitor string
	// Extractor carries the tool parameters with Tool already derived
	// from ThemeProvider.
	Extractor extract.Config
}

// DefaultsColorExtractor returns the schema defaults.
func DefaultsColorExtractor() ColorExtractorConfig {
	ex := extract.DefaultConfig()
	ex.Tool = ThemeWayle.Tool()
	return ColorExtractorConfig{ThemeProvider: ThemeWayle, Extractor: ex}
}

// clampWarn clamps a ranged float the way the Rust validated newtypes
// deserialize: out of range is clamped with a warning, not an error.
func clampWarn(key string, v, lo, hi float64) float64 {
	c := min(max(v, lo), hi)
	if c != v {
		log.Printf("config: styling %s %v out of range (valid: %v-%v), clamped to %v", key, v, lo, hi, c)
	}
	return c
}

// applyColorExtractor overlays the extraction keys of [styling].
func applyColorExtractor(md toml.MetaData, prim toml.Primitive) (ColorExtractorConfig, error) {
	cfg := DefaultsColorExtractor()
	ex := &cfg.Extractor
	var doc struct {
		ThemeProvider        *string  `toml:"theme-provider"`
		ThemingMonitor       *string  `toml:"theming-monitor"`
		MatugenScheme        *string  `toml:"matugen-scheme"`
		MatugenContrast      *float64 `toml:"matugen-contrast"`
		MatugenSourceColor   *int64   `toml:"matugen-source-color"`
		MatugenLight         *bool    `toml:"matugen-light"`
		WallustPalette       *string  `toml:"wallust-palette"`
		WallustSaturation    *int64   `toml:"wallust-saturation"`
		WallustCheckContrast *bool    `toml:"wallust-check-contrast"`
		WallustBackend       *string  `toml:"wallust-backend"`
		WallustColorspace    *string  `toml:"wallust-colorspace"`
		WallustApplyGlobally *bool    `toml:"wallust-apply-globally"`
		PywalSaturation      *float64 `toml:"pywal-saturation"`
		PywalContrast        *float64 `toml:"pywal-contrast"`
		PywalLight           *bool    `toml:"pywal-light"`
		PywalApplyGlobally   *bool    `toml:"pywal-apply-globally"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.ThemeProvider != nil {
		switch p := ThemeProvider(*doc.ThemeProvider); p {
		case ThemeWayle, ThemeMatugen, ThemePywal, ThemeWallust:
			cfg.ThemeProvider = p
			ex.Tool = p.Tool()
		default:
			return cfg, fmt.Errorf("styling: theme-provider: unknown variant %q (valid: wayle, matugen, pywal, wallust)", p)
		}
	}
	if doc.ThemingMonitor != nil {
		cfg.ThemingMonitor = *doc.ThemingMonitor
	}
	if doc.MatugenScheme != nil {
		s, err := extract.ParseMatugenScheme(*doc.MatugenScheme)
		if err != nil {
			return cfg, fmt.Errorf("styling: matugen-scheme: %w", err)
		}
		ex.MatugenScheme = s
	}
	if doc.MatugenContrast != nil {
		ex.MatugenContrast = clampWarn("matugen-contrast", *doc.MatugenContrast, -1, 1)
	}
	if doc.MatugenSourceColor != nil {
		v := *doc.MatugenSourceColor
		if v < 0 || v > 255 {
			return cfg, fmt.Errorf("styling: matugen-source-color: invalid value %d, expected u8", v)
		}
		ex.MatugenSourceColor = uint8(v)
	}
	if doc.MatugenLight != nil {
		ex.MatugenLight = *doc.MatugenLight
	}
	if doc.WallustPalette != nil {
		p, err := extract.ParseWallustPalette(*doc.WallustPalette)
		if err != nil {
			return cfg, fmt.Errorf("styling: wallust-palette: %w", err)
		}
		ex.WallustPalette = p
	}
	if doc.WallustSaturation != nil {
		v := *doc.WallustSaturation
		if v < 0 || v > 255 {
			return cfg, fmt.Errorf("styling: wallust-saturation: invalid value %d, expected u8", v)
		}
		if v > 100 {
			log.Printf("config: percentage %d exceeds maximum, clamped to 100", v)
			v = 100
		}
		ex.WallustSaturation = uint8(v)
	}
	if doc.WallustCheckContrast != nil {
		ex.WallustCheckContrast = *doc.WallustCheckContrast
	}
	if doc.WallustBackend != nil {
		b, err := extract.ParseWallustBackend(*doc.WallustBackend)
		if err != nil {
			return cfg, fmt.Errorf("styling: wallust-backend: %w", err)
		}
		ex.WallustBackend = b
	}
	if doc.WallustColorspace != nil {
		c, err := extract.ParseWallustColorspace(*doc.WallustColorspace)
		if err != nil {
			return cfg, fmt.Errorf("styling: wallust-colorspace: %w", err)
		}
		ex.WallustColorspace = c
	}
	if doc.WallustApplyGlobally != nil {
		ex.WallustApplyGlobally = *doc.WallustApplyGlobally
	}
	if doc.PywalSaturation != nil {
		ex.PywalSaturation = clampWarn("pywal-saturation", *doc.PywalSaturation, 0, 1)
	}
	if doc.PywalContrast != nil {
		ex.PywalContrast = clampWarn("pywal-contrast", *doc.PywalContrast, 1, 21)
	}
	if doc.PywalLight != nil {
		ex.PywalLight = *doc.PywalLight
	}
	if doc.PywalApplyGlobally != nil {
		ex.PywalApplyGlobally = *doc.PywalApplyGlobally
	}
	return cfg, nil
}
