package extract

import (
	"fmt"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// Tool is the external program that extracts a palette from an image
// (ColorExtractor in crates/wayle-wallpaper/src/types/color_extractor).
// None disables extraction.
type Tool uint8

// Extraction tools. Wallust is the Rust default.
const (
	Wallust Tool = iota
	Matugen
	Pywal
	None
)

// String is the tool's lowercase name, as the Rust Display writes it.
func (t Tool) String() string {
	switch t {
	case Wallust:
		return "wallust"
	case Matugen:
		return "matugen"
	case Pywal:
		return "pywal"
	case None:
		return "none"
	}
	return fmt.Sprintf("Tool(%d)", uint8(t))
}

// ParseTool reads a tool name case-insensitively, accepting the Rust
// FromStr aliases "wal" (pywal) and "disabled" (none).
func ParseTool(s string) (Tool, error) {
	switch strings.ToLower(s) {
	case "wallust":
		return Wallust, nil
	case "matugen":
		return Matugen, nil
	case "pywal", "wal":
		return Pywal, nil
	case "none", "disabled":
		return None, nil
	}
	return 0, fmt.Errorf("Invalid color extractor: %s", s) //nolint:staticcheck // the Rust message, verbatim
}

// The tool parameters are the [styling] schema enums themselves, so the
// config and the tool invocations share one type.
type (
	// MatugenScheme is the matugen color scheme.
	MatugenScheme = config.MatugenScheme
	// WallustPalette is the wallust palette mode.
	WallustPalette = config.WallustPalette
	// WallustBackend is the wallust sampling backend.
	WallustBackend = config.WallustBackend
	// WallustColorspace is the wallust color space.
	WallustColorspace = config.WallustColorspace
)

// ToolFor is the extractor a theme provider needs
// (build_extractor_config): the static wayle provider extracts nothing.
func ToolFor(p config.ThemeProvider) Tool {
	switch p {
	case config.ThemeProviderMatugen:
		return Matugen
	case config.ThemeProviderPywal:
		return Pywal
	case config.ThemeProviderWallust:
		return Wallust
	}
	return None
}

// FromConfig is the extraction config the [styling] keys describe.
func FromConfig(c config.ColorExtractorConfig) Config {
	return Config{
		Tool:                 ToolFor(c.ThemeProvider),
		MatugenScheme:        c.MatugenScheme,
		MatugenContrast:      float64(c.MatugenContrast),
		MatugenSourceColor:   c.MatugenSourceColor,
		MatugenLight:         c.MatugenLight,
		WallustPalette:       c.WallustPalette,
		WallustSaturation:    uint8(c.WallustSaturation),
		WallustCheckContrast: c.WallustCheckContrast,
		WallustBackend:       c.WallustBackend,
		WallustColorspace:    c.WallustColorspace,
		WallustApplyGlobally: c.WallustApplyGlobally,
		PywalSaturation:      float64(c.PywalSaturation),
		PywalContrast:        float64(c.PywalContrast),
		PywalLight:           c.PywalLight,
		PywalApplyGlobally:   c.PywalApplyGlobally,
	}
}
