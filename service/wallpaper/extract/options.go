package extract

import (
	"fmt"
	"slices"
	"strings"
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

// enumSet is a closed set of config spellings; each typed enum below
// is an index into one.
type enumSet []string

func (e enumSet) parse(kind, s string) (int, error) {
	if i := slices.Index(e, s); i >= 0 {
		return i, nil
	}
	return 0, fmt.Errorf("unknown %s %q (valid: %s)", kind, s, strings.Join(e, ", "))
}

func (e enumSet) name(i int) string {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return fmt.Sprintf("invalid(%d)", i)
}

// MatugenScheme is matugen's color scheme type
// (styling/types/extractor.rs). The config spelling is kebab-case
// ("tonal-spot"); CLIValue is matugen's --type value.
type MatugenScheme uint8

// Matugen schemes; TonalSpot is the default.
const (
	SchemeContent MatugenScheme = iota
	SchemeExpressive
	SchemeFidelity
	SchemeFruitSalad
	SchemeMonochrome
	SchemeNeutral
	SchemeRainbow
	SchemeTonalSpot
	SchemeVibrant
)

var matugenSchemes = enumSet{
	"content", "expressive", "fidelity", "fruit-salad", "monochrome",
	"neutral", "rainbow", "tonal-spot", "vibrant",
}

// ParseMatugenScheme reads the config spelling.
func ParseMatugenScheme(s string) (MatugenScheme, error) {
	i, err := matugenSchemes.parse("matugen scheme", s)
	return MatugenScheme(i), err
}

// String is the config spelling.
func (m MatugenScheme) String() string { return matugenSchemes.name(int(m)) }

// CLIValue is the value of matugen's --type flag ("scheme-tonal-spot").
func (m MatugenScheme) CLIValue() string { return "scheme-" + m.String() }

// WallustPalette is wallust's palette mode.
type WallustPalette uint8

// Wallust palettes, in the Rust declaration order; Dark16 is the
// default.
const (
	PaletteDark16 WallustPalette = iota
	PaletteDark
	PaletteDarkcomp
	PaletteDarkcomp16
	PaletteHarddark
	PaletteHarddark16
	PaletteHarddarkcomp
	PaletteHarddarkcomp16
	PaletteLight
	PaletteLight16
	PaletteLightcomp
	PaletteLightcomp16
	PaletteSoftdark
	PaletteSoftdark16
	PaletteSoftdarkcomp
	PaletteSoftdarkcomp16
	PaletteSoftlight
	PaletteSoftlight16
	PaletteSoftlightcomp
	PaletteSoftlightcomp16
	PaletteAnsidark
	PaletteAnsidark16
)

var wallustPalettes = enumSet{
	"dark16", "dark", "darkcomp", "darkcomp16", "harddark", "harddark16",
	"harddarkcomp", "harddarkcomp16", "light", "light16", "lightcomp",
	"lightcomp16", "softdark", "softdark16", "softdarkcomp", "softdarkcomp16",
	"softlight", "softlight16", "softlightcomp", "softlightcomp16",
	"ansidark", "ansidark16",
}

// ParseWallustPalette reads the config spelling.
func ParseWallustPalette(s string) (WallustPalette, error) {
	i, err := wallustPalettes.parse("wallust palette", s)
	return WallustPalette(i), err
}

// String is the config (and wallust.toml) spelling.
func (p WallustPalette) String() string { return wallustPalettes.name(int(p)) }

// IsLight reports whether the palette produces a light background.
func (p WallustPalette) IsLight() bool {
	switch p {
	case PaletteLight, PaletteLight16, PaletteLightcomp, PaletteLightcomp16,
		PaletteSoftlight, PaletteSoftlight16, PaletteSoftlightcomp, PaletteSoftlightcomp16:
		return true
	}
	return false
}

// WallustBackend is wallust's image sampling backend.
type WallustBackend uint8

// Wallust backends; Fastresize is the default.
const (
	BackendFull WallustBackend = iota
	BackendResized
	BackendWal
	BackendThumb
	BackendFastresize
	BackendKmeans
)

var wallustBackends = enumSet{"full", "resized", "wal", "thumb", "fastresize", "kmeans"}

// ParseWallustBackend reads the config spelling.
func ParseWallustBackend(s string) (WallustBackend, error) {
	i, err := wallustBackends.parse("wallust backend", s)
	return WallustBackend(i), err
}

// String is the config (and wallust.toml) spelling.
func (b WallustBackend) String() string { return wallustBackends.name(int(b)) }

// WallustColorspace is wallust's color space for dominant colors.
type WallustColorspace uint8

// Wallust color spaces; Labmixed is the default.
const (
	ColorspaceLab WallustColorspace = iota
	ColorspaceLabmixed
	ColorspaceLch
	ColorspaceLchmixed
	ColorspaceLchansi
)

var wallustColorspaces = enumSet{"lab", "labmixed", "lch", "lchmixed", "lchansi"}

// ParseWallustColorspace reads the config spelling.
func ParseWallustColorspace(s string) (WallustColorspace, error) {
	i, err := wallustColorspaces.parse("wallust colorspace", s)
	return WallustColorspace(i), err
}

// String is the config (and wallust.toml) spelling.
func (c WallustColorspace) String() string { return wallustColorspaces.name(int(c)) }
