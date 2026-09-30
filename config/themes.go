package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Palette is a theme's ten colors as the CSS generation consumes them
// (crates/wayle-config/src/infrastructure/themes/mod.rs). The values are
// the strings a theme or provider supplied; the [styling.palette]
// section's validated form is PaletteConfig.
type Palette struct {
	Bg, Surface, Elevated string
	Fg, FgMuted           string
	Primary               string
	Red, Yellow           string
	Green, Blue           string
}

// ThemeEntry is one theme available for selection: a built-in, or a
// user theme file.
type ThemeEntry struct {
	Name    string
	Palette Palette
	Builtin bool
}

// wayleTheme is the default palette (palettes.rs wayle_theme).
var wayleTheme = Palette{
	Bg: "#141420", Surface: "#1c1c2c", Elevated: "#262638",
	Fg: "#d4d6e8", FgMuted: "#8a8ca4", Primary: "#e0947a",
	Red: "#e46870", Yellow: "#e0b870", Green: "#68c898", Blue: "#78a0e0",
}

// paletteConfigOf lifts a built-in palette into the [styling.palette]
// form; a malformed built-in literal panics at init (mustHex).
func paletteConfigOf(p Palette) PaletteConfig {
	return PaletteConfig{
		Bg: mustHex(p.Bg), Surface: mustHex(p.Surface), Elevated: mustHex(p.Elevated),
		Fg: mustHex(p.Fg), FgMuted: mustHex(p.FgMuted), Primary: mustHex(p.Primary),
		Red: mustHex(p.Red), Yellow: mustHex(p.Yellow), Green: mustHex(p.Green), Blue: mustHex(p.Blue),
	}
}

// builtinThemes is the built-in theme table in the Rust order
// (palettes.rs builtins).
var builtinThemes = []struct {
	name    string
	palette Palette
}{
	{"wayle", wayleTheme},
	{"catppuccin-mocha", Palette{"#11111b", "#181825", "#1e1e2e", "#cdd6f4", "#bac2de", "#b4befe", "#f38ba8", "#f9e2af", "#a6e3a1", "#74c7ec"}},
	{"catppuccin-macchiato", Palette{"#181926", "#1e2030", "#24273a", "#cad3f5", "#b8c0e0", "#b7bdf8", "#ed8796", "#eed49f", "#a6da95", "#8aadf4"}},
	{"catppuccin-frappe", Palette{"#232634", "#292c3c", "#303446", "#c6d0f5", "#b5bfe2", "#babbf1", "#e78284", "#e5c890", "#a6d189", "#8caaee"}},
	{"catppuccin-latte", Palette{"#eff1f5", "#e6e9ef", "#dce0e8", "#4c4f69", "#5c5f77", "#7287fd", "#d20f39", "#df8e1d", "#40a02b", "#1e66f5"}},
	{"dracula", Palette{"#282a36", "#343746", "#44475a", "#f8f8f2", "#6272a4", "#bd93f9", "#ff5555", "#f1fa8c", "#50fa7b", "#8be9fd"}},
	{"everforest-dark", Palette{"#2d353b", "#343f44", "#3d484d", "#d3c6aa", "#859289", "#7fbbb3", "#e67e80", "#dbbc7f", "#a7c080", "#83c092"}},
	{"everforest-dark-hard", Palette{"#272e33", "#2d353b", "#343f44", "#d3c6aa", "#859289", "#7fbbb3", "#e67e80", "#dbbc7f", "#a7c080", "#83c092"}},
	{"everforest-dark-soft", Palette{"#333c43", "#3a464c", "#4d5960", "#d3c6aa", "#859289", "#7fbbb3", "#e67e80", "#dbbc7f", "#a7c080", "#83c092"}},
	{"everforest-light", Palette{"#fdf6e3", "#f4f0d9", "#e6e2cc", "#5c6a72", "#939f91", "#3a94c5", "#f85552", "#dfa000", "#8da101", "#35a77c"}},
	{"everforest-light-hard", Palette{"#fffbef", "#fdf6e3", "#f4f0d9", "#5c6a72", "#939f91", "#3a94c5", "#f85552", "#dfa000", "#8da101", "#35a77c"}},
	{"everforest-light-soft", Palette{"#f3ead3", "#eae4ca", "#ddd8be", "#5c6a72", "#939f91", "#3a94c5", "#f85552", "#dfa000", "#8da101", "#35a77c"}},
	{"gruvbox-dark", Palette{"#282828", "#3c3836", "#504945", "#ebdbb2", "#d5c4a1", "#83a598", "#fb4934", "#fabd2f", "#b8bb26", "#8ec07c"}},
	{"gruvbox-dark-hard", Palette{"#1d2021", "#282828", "#3c3836", "#ebdbb2", "#d5c4a1", "#83a598", "#fb4934", "#fabd2f", "#b8bb26", "#8ec07c"}},
	{"gruvbox-dark-soft", Palette{"#32302f", "#3c3836", "#504945", "#ebdbb2", "#d5c4a1", "#83a598", "#fb4934", "#fabd2f", "#b8bb26", "#8ec07c"}},
	{"gruvbox-light", Palette{"#fbf1c7", "#ebdbb2", "#d5c4a1", "#3c3836", "#504945", "#076678", "#9d0006", "#b57614", "#79740e", "#427b58"}},
	{"gruvbox-light-hard", Palette{"#f9f5d7", "#fbf1c7", "#ebdbb2", "#3c3836", "#504945", "#076678", "#9d0006", "#b57614", "#79740e", "#427b58"}},
	{"gruvbox-light-soft", Palette{"#f2e5bc", "#ebdbb2", "#d5c4a1", "#3c3836", "#504945", "#076678", "#9d0006", "#b57614", "#79740e", "#427b58"}},
	{"kanagawa-wave", Palette{"#1a1a22", "#1f1f28", "#2a2a37", "#dcd7ba", "#727169", "#957fb8", "#e82424", "#dca561", "#98bb6c", "#7e9cd8"}},
	{"kanagawa-dragon", Palette{"#0d0c0c", "#181616", "#282727", "#c5c9c5", "#a6a69c", "#8992a7", "#c4746e", "#c4b28a", "#87a987", "#8ba4b0"}},
	{"kanagawa-lotus", Palette{"#f2ecbc", "#e5ddb0", "#e7dba0", "#545464", "#716e61", "#766b90", "#c84053", "#de9800", "#6f894e", "#4d699b"}},
	{"monokai", Palette{"#1e1f1c", "#272822", "#3e3d32", "#f8f8f2", "#75715e", "#ae81ff", "#f92672", "#e6db74", "#a6e22e", "#66d9ef"}},
	{"monokai-pro-classic", Palette{"#221f22", "#2d2a2e", "#403e41", "#fcfcfa", "#727072", "#ab9df2", "#ff6188", "#ffd866", "#a9dc76", "#78dce8"}},
	{"monokai-pro-octagon", Palette{"#1d1f28", "#282a3a", "#3a3d4b", "#eaf2f1", "#696d77", "#c39ac9", "#ff657a", "#ffd76d", "#bad761", "#9cd1bb"}},
	{"monokai-pro-machine", Palette{"#1d2528", "#273136", "#363c42", "#f2fffc", "#6b7678", "#baa0f8", "#ff6d7e", "#ffed72", "#a2e57b", "#7cd5f1"}},
	{"monokai-pro-ristretto", Palette{"#211c1c", "#2c2525", "#403838", "#fff1f3", "#72696a", "#a8a9eb", "#fd6883", "#f9cc6c", "#adda78", "#85dacc"}},
	{"monokai-pro-spectrum", Palette{"#191919", "#222222", "#363537", "#f7f1ff", "#69676c", "#948ae3", "#fc618d", "#fce566", "#7bd88f", "#5ad4e6"}},
	{"nightfox-carbonfox", Palette{"#0a0a0a", "#161616", "#1f1f1f", "#f2f4f8", "#a8aab1", "#78a9ff", "#ee5396", "#08bdba", "#25be6a", "#33b1ff"}},
	{"nightfox-nightfox", Palette{"#131a24", "#192330", "#212e3f", "#cdcecf", "#71839b", "#719cd6", "#c94f6d", "#dbc074", "#81b29a", "#63cdcf"}},
	{"nightfox-duskfox", Palette{"#191726", "#232136", "#2d2a45", "#e0def4", "#6e6a86", "#c4a7e7", "#eb6f92", "#f6c177", "#a3be8c", "#9ccfd8"}},
	{"nightfox-nordfox", Palette{"#232831", "#2e3440", "#39404f", "#cdcecf", "#7e8188", "#88c0d0", "#bf616a", "#ebcb8b", "#a3be8c", "#81a1c1"}},
	{"nightfox-terafox", Palette{"#0f1c1e", "#152528", "#1d3337", "#e6eaea", "#587b7b", "#5a93aa", "#e85c51", "#fda47f", "#7aa4a1", "#a1cdd8"}},
	{"nightfox-dayfox", Palette{"#f6f2ee", "#e4dcd4", "#d3c7bb", "#3d2b5a", "#824d5b", "#2848a9", "#a5222f", "#ac5402", "#396847", "#287980"}},
	{"nord", Palette{"#2e3440", "#3b4252", "#434c5e", "#eceff4", "#d8dee9", "#88c0d0", "#bf616a", "#ebcb8b", "#a3be8c", "#81a1c1"}},
	{"one-dark", Palette{"#21252b", "#282c34", "#3e4451", "#abb2bf", "#5c6370", "#61afef", "#e06c75", "#e5c07b", "#98c379", "#56b6c2"}},
	{"one-light", Palette{"#fafafa", "#ebebec", "#e5e5e6", "#383a42", "#a0a1a7", "#4078f2", "#e45649", "#c18401", "#50a14f", "#0184bc"}},
	{"rose-pine-main", Palette{"#191724", "#1f1d2e", "#26233a", "#e0def4", "#908caa", "#c4a7e7", "#eb6f92", "#f6c177", "#31748f", "#9ccfd8"}},
	{"rose-pine-moon", Palette{"#232136", "#2a273f", "#393552", "#e0def4", "#908caa", "#c4a7e7", "#eb6f92", "#f6c177", "#3e8fb0", "#9ccfd8"}},
	{"rose-pine-dawn", Palette{"#fffaf3", "#faf4ed", "#f2e9e1", "#464261", "#797593", "#907aa9", "#b4637a", "#ea9d34", "#286983", "#56949f"}},
	{"solarized-dark", Palette{"#002b36", "#073642", "#094553", "#839496", "#586e75", "#268bd2", "#dc322f", "#b58900", "#859900", "#2aa198"}},
	{"solarized-light", Palette{"#fdf6e3", "#eee8d5", "#e0d9c4", "#657b83", "#93a1a1", "#268bd2", "#dc322f", "#b58900", "#859900", "#2aa198"}},
	{"tokyo-night-night", Palette{"#16161e", "#1a1b26", "#292e42", "#c0caf5", "#a9b1d6", "#7aa2f7", "#f7768e", "#e0af68", "#9ece6a", "#7dcfff"}},
	{"tokyo-night-storm", Palette{"#1f2335", "#24283b", "#292e42", "#c0caf5", "#a9b1d6", "#7aa2f7", "#f7768e", "#e0af68", "#9ece6a", "#7dcfff"}},
	{"tokyo-night-moon", Palette{"#1e2030", "#222436", "#2f334d", "#c8d3f5", "#828bb8", "#82aaff", "#ff757f", "#ffc777", "#c3e88d", "#86e1fc"}},
	{"tokyo-night-day", Palette{"#e1e2e7", "#d0d5e3", "#c4c8da", "#3760bf", "#6172b0", "#2e7de9", "#f52a65", "#8c6c3e", "#587539", "#007197"}},
}

// BuiltinThemes returns every built-in theme, in the Rust order.
func BuiltinThemes() []ThemeEntry {
	out := make([]ThemeEntry, len(builtinThemes))
	for i, t := range builtinThemes {
		out[i] = ThemeEntry{Name: t.name, Palette: t.palette, Builtin: true}
	}
	return out
}

// PaletteByName looks up a built-in theme's palette.
func PaletteByName(name string) (Palette, bool) {
	for _, t := range builtinThemes {
		if t.name == name {
			return t.palette, true
		}
	}
	return Palette{}, false
}

// themePairs are the canonical dark/light pairs, read both ways by
// AppearanceVariant; each family's first entry is the dark theme a light
// one maps back to.
var themePairs = [][2]string{
	{"catppuccin-mocha", "catppuccin-latte"},
	{"everforest-dark", "everforest-light"},
	{"everforest-dark-hard", "everforest-light-hard"},
	{"everforest-dark-soft", "everforest-light-soft"},
	{"gruvbox-dark", "gruvbox-light"},
	{"gruvbox-dark-hard", "gruvbox-light-hard"},
	{"gruvbox-dark-soft", "gruvbox-light-soft"},
	{"kanagawa-wave", "kanagawa-lotus"},
	{"one-dark", "one-light"},
	{"rose-pine-main", "rose-pine-dawn"},
	{"solarized-dark", "solarized-light"},
	{"tokyo-night-night", "tokyo-night-day"},
	{"nightfox-nightfox", "nightfox-dayfox"},
}

// darkSubvariantLight maps the extra dark sub-variants to their family's
// shared light theme; one-way, they are already dark.
var darkSubvariantLight = [][2]string{
	{"catppuccin-macchiato", "catppuccin-latte"},
	{"catppuccin-frappe", "catppuccin-latte"},
	{"kanagawa-dragon", "kanagawa-lotus"},
	{"rose-pine-moon", "rose-pine-dawn"},
	{"tokyo-night-storm", "tokyo-night-day"},
	{"tokyo-night-moon", "tokyo-night-day"},
	{"nightfox-carbonfox", "nightfox-dayfox"},
	{"nightfox-duskfox", "nightfox-dayfox"},
	{"nightfox-nordfox", "nightfox-dayfox"},
	{"nightfox-terafox", "nightfox-dayfox"},
}

// AppearanceVariant names theme's light (wantLight) or dark sibling; ok
// is false when theme already has the requested mode or has no pair
// (palettes.rs appearance_variant).
func AppearanceVariant(theme string, wantLight bool) (string, bool) {
	if wantLight {
		for _, p := range themePairs {
			if p[1] == theme {
				return "", false // already light
			}
		}
		for _, table := range [][][2]string{themePairs, darkSubvariantLight} {
			for _, p := range table {
				if p[0] == theme {
					return p[1], true
				}
			}
		}
		return "", false
	}
	for _, table := range [][][2]string{themePairs, darkSubvariantLight} {
		for _, p := range table {
			if p[0] == theme {
				return "", false // already dark
			}
		}
	}
	for _, p := range themePairs {
		if p[1] == theme {
			return p[0], true
		}
	}
	return "", false
}

// ThemesDir is the user themes directory inside the config dir.
func ThemesDir(configDir string) string { return filepath.Join(configDir, "themes") }

// themeFileDoc is a user theme file: every palette key is required, as
// the Rust Palette deserializes without defaults; note fg_muted is the
// serde field name, not kebab-case.
type themeFileDoc struct {
	Bg       *string `toml:"bg"`
	Surface  *string `toml:"surface"`
	Elevated *string `toml:"elevated"`
	Fg       *string `toml:"fg"`
	FgMuted  *string `toml:"fg_muted"`
	Primary  *string `toml:"primary"`
	Red      *string `toml:"red"`
	Yellow   *string `toml:"yellow"`
	Green    *string `toml:"green"`
	Blue     *string `toml:"blue"`
}

// LoadThemes discovers the selectable themes: the built-ins first, then
// every <dir>/*.toml theme file named by its file stem. A missing or
// unreadable directory yields the built-ins alone. A file that cannot be
// read or parsed, or whose name is already taken, is skipped; each skip
// comes back in skipped for the caller to log, as load_themes logs them
// (infrastructure/themes/utils.rs). Files load in name order.
func LoadThemes(dir string) (themes []ThemeEntry, skipped []error) {
	themes = BuiltinThemes()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return themes, nil
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		theme, err := loadThemeFile(path)
		if err != nil {
			skipped = append(skipped, err)
			continue
		}
		if themeIndex(themes, theme.Name) >= 0 {
			skipped = append(skipped, fmt.Errorf("config: theme %q already exists, skipping %s", theme.Name, path))
			continue
		}
		themes = append(themes, theme)
	}
	return themes, skipped
}

func themeIndex(themes []ThemeEntry, name string) int {
	for i, t := range themes {
		if t.Name == name {
			return i
		}
	}
	return -1
}

// loadThemeFile reads one user theme (get_theme_from_file).
func loadThemeFile(path string) (ThemeEntry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a directory entry of the themes dir
	if err != nil {
		return ThemeEntry{}, fmt.Errorf("config: cannot load theme %s: %w", path, err)
	}
	var doc themeFileDoc
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return ThemeEntry{}, fmt.Errorf("config: cannot load theme %s: %w", path, err)
	}
	var p Palette
	for _, f := range []struct {
		raw *string
		dst *string
		key string
	}{
		{doc.Bg, &p.Bg, "bg"},
		{doc.Surface, &p.Surface, "surface"},
		{doc.Elevated, &p.Elevated, "elevated"},
		{doc.Fg, &p.Fg, "fg"},
		{doc.FgMuted, &p.FgMuted, "fg_muted"},
		{doc.Primary, &p.Primary, "primary"},
		{doc.Red, &p.Red, "red"},
		{doc.Yellow, &p.Yellow, "yellow"},
		{doc.Green, &p.Green, "green"},
		{doc.Blue, &p.Blue, "blue"},
	} {
		if f.raw == nil {
			return ThemeEntry{}, fmt.Errorf("config: cannot load theme %s: missing field %q", path, f.key)
		}
		*f.dst = *f.raw
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return ThemeEntry{Name: name, Palette: p}, nil
}
