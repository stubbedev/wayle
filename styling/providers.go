package styling

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// The wallpaper palette providers, ported from
// crates/wayle-styling/src/palette_provider/{matugen,pywal,wallust}.rs:
// each reads the JSON its extractor wrote, picks the light or dark
// variant, and derives the three background layers from the one
// background color.

// ErrPaletteNotFound reports that a provider's palette file does not
// exist yet (Error::PaletteNotFound); the extractor has not run.
var ErrPaletteNotFound = errors.New("styling: palette file not found")

// readPaletteJSON decodes one provider file: a missing file is
// ErrPaletteNotFound, bad JSON a parse error.
func readPaletteJSON(path string, into any) error {
	data, err := os.ReadFile(path) //nolint:gosec // the provider paths are fixed cache locations
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrPaletteNotFound, path)
	}
	if err != nil {
		return fmt.Errorf("styling: read palette %s: %w", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("styling: cannot parse palette JSON %s: %w", path, err)
	}
	return nil
}

// required checks that every field serde would require was present.
func required(path string, fields map[string]*string) error {
	for name, v := range fields {
		if v == nil {
			return fmt.Errorf("styling: cannot parse palette JSON %s: missing field %q", path, name)
		}
	}
	return nil
}

// matugenColor is matugen's color value: a bare string (older matugen)
// or {"color": "..."} (newer), the untagged Rust ColorValue.
type matugenColor string

func (c *matugenColor) UnmarshalJSON(data []byte) error {
	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		*c = matugenColor(plain)
		return nil
	}
	var nested struct {
		Color *string `json:"color"`
	}
	if err := json.Unmarshal(data, &nested); err != nil || nested.Color == nil {
		return errors.New("data did not match any variant of untagged enum ColorValue")
	}
	*c = matugenColor(*nested.Color)
	return nil
}

type matugenVariants struct {
	Dark  *matugenColor `json:"dark"`
	Light *matugenColor `json:"light"`
}

// LoadMatugen reads a matugen palette: primary doubles as blue, tertiary
// is yellow, secondary green, error red.
func LoadMatugen(path string, isLight bool) (config.Palette, error) {
	var out struct {
		Colors *struct {
			Background       *matugenVariants `json:"background"`
			OnBackground     *matugenVariants `json:"on_background"`
			OnSurfaceVariant *matugenVariants `json:"on_surface_variant"`
			Primary          *matugenVariants `json:"primary"`
			Secondary        *matugenVariants `json:"secondary"`
			Tertiary         *matugenVariants `json:"tertiary"`
			Error            *matugenVariants `json:"error"`
		} `json:"colors"`
	}
	if err := readPaletteJSON(path, &out); err != nil {
		return config.Palette{}, err
	}
	if out.Colors == nil {
		return config.Palette{}, fmt.Errorf("styling: cannot parse palette JSON %s: missing field \"colors\"", path)
	}
	c := out.Colors
	picked := map[string]*string{}
	pick := func(name string, v *matugenVariants) string {
		if v == nil {
			picked[name] = nil
			return ""
		}
		dark, light := (*string)(v.Dark), (*string)(v.Light)
		picked[name+".dark"], picked[name+".light"] = dark, light
		if dark == nil || light == nil {
			return ""
		}
		if isLight {
			return *light
		}
		return *dark
	}
	bg := pick("background", c.Background)
	fg := pick("on_background", c.OnBackground)
	fgMuted := pick("on_surface_variant", c.OnSurfaceVariant)
	primary := pick("primary", c.Primary)
	green := pick("secondary", c.Secondary)
	yellow := pick("tertiary", c.Tertiary)
	red := pick("error", c.Error)
	if err := required(path, picked); err != nil {
		return config.Palette{}, err
	}
	l := deriveLayers(bg, isLight)
	return config.Palette{
		Bg: l.bg, Surface: l.surface, Elevated: l.elevated,
		Fg: fg, FgMuted: fgMuted, Primary: primary,
		Red: red, Yellow: yellow, Green: green, Blue: primary,
	}, nil
}

// LoadPywal reads pywal's colors.json: the special background and
// foreground, color1-4 as red/green/yellow/blue, color7 muted.
func LoadPywal(path string, isLight bool) (config.Palette, error) {
	var out struct {
		Special *struct {
			Background *string `json:"background"`
			Foreground *string `json:"foreground"`
		} `json:"special"`
		Colors *struct {
			Color1 *string `json:"color1"`
			Color2 *string `json:"color2"`
			Color3 *string `json:"color3"`
			Color4 *string `json:"color4"`
			Color7 *string `json:"color7"`
		} `json:"colors"`
	}
	if err := readPaletteJSON(path, &out); err != nil {
		return config.Palette{}, err
	}
	if out.Special == nil || out.Colors == nil {
		return config.Palette{}, fmt.Errorf("styling: cannot parse palette JSON %s: missing field \"special\" or \"colors\"", path)
	}
	s, c := out.Special, out.Colors
	if err := required(path, map[string]*string{
		"background": s.Background, "foreground": s.Foreground,
		"color1": c.Color1, "color2": c.Color2, "color3": c.Color3, "color4": c.Color4, "color7": c.Color7,
	}); err != nil {
		return config.Palette{}, err
	}
	l := deriveLayers(*s.Background, isLight)
	return config.Palette{
		Bg: l.bg, Surface: l.surface, Elevated: l.elevated,
		Fg: *s.Foreground, FgMuted: *c.Color7, Primary: *c.Color4,
		Red: *c.Color1, Yellow: *c.Color3, Green: *c.Color2, Blue: *c.Color4,
	}, nil
}

// LoadWallust reads wallust's cached palette: color3 red, color4 green,
// color5 yellow, color6 primary and blue, color7 muted.
func LoadWallust(path string, isLight bool) (config.Palette, error) {
	var out struct {
		Background *string `json:"background"`
		Foreground *string `json:"foreground"`
		Color3     *string `json:"color3"`
		Color4     *string `json:"color4"`
		Color5     *string `json:"color5"`
		Color6     *string `json:"color6"`
		Color7     *string `json:"color7"`
	}
	if err := readPaletteJSON(path, &out); err != nil {
		return config.Palette{}, err
	}
	if err := required(path, map[string]*string{
		"background": out.Background, "foreground": out.Foreground, "color3": out.Color3,
		"color4": out.Color4, "color5": out.Color5, "color6": out.Color6, "color7": out.Color7,
	}); err != nil {
		return config.Palette{}, err
	}
	l := deriveLayers(*out.Background, isLight)
	return config.Palette{
		Bg: l.bg, Surface: l.surface, Elevated: l.elevated,
		Fg: *out.Foreground, FgMuted: *out.Color7, Primary: *out.Color6,
		Red: *out.Color3, Yellow: *out.Color5, Green: *out.Color4, Blue: *out.Color6,
	}, nil
}

// ResolvePalette is the active palette for the configured theme
// provider (resolve_palette): the wayle provider is fallback itself, the
// extractors load the file extract.ColorsPath names for their tool, with
// the forced appearance overriding the tool's own light setting. A
// provider that fails yields fallback and the failure, which the caller
// logs — the palette is always usable, as in Rust, which logs and falls
// back.
func ResolvePalette(fallback config.Palette, s config.StylingConfig, ce config.ColorExtractorConfig) (config.Palette, error) {
	forcedLight, forced := s.Appearance.ForcedLight()
	isLight := func(own bool) bool {
		if forced {
			return forcedLight
		}
		return own
	}
	ex := ce.Extractor
	var (
		load  func(string, bool) (config.Palette, error)
		light bool
	)
	switch ce.ThemeProvider {
	case config.ThemeMatugen:
		load, light = LoadMatugen, isLight(ex.MatugenLight)
	case config.ThemeWallust:
		load, light = LoadWallust, isLight(ex.WallustPalette.IsLight())
	case config.ThemePywal:
		load, light = LoadPywal, isLight(ex.PywalLight)
	default:
		return fallback, nil
	}
	path, _, err := extract.ColorsPath(ce.ThemeProvider.Tool())
	if err != nil {
		return fallback, fmt.Errorf("styling: %s palette load failed: %w", ce.ThemeProvider, err)
	}
	p, err := load(path, light)
	if err != nil {
		return fallback, fmt.Errorf("styling: %s palette load failed: %w", ce.ThemeProvider, err)
	}
	return p, nil
}
