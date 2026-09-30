package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinThemesTable(t *testing.T) {
	themes := BuiltinThemes()
	if len(themes) != 45 {
		t.Fatalf("builtins = %d, want the 45 Rust themes", len(themes))
	}
	if themes[0].Name != "wayle" || themes[0].Palette != wayleTheme {
		t.Errorf("first builtin = %+v, want the wayle theme", themes[0])
	}
	seen := map[string]bool{}
	for _, theme := range themes {
		if seen[theme.Name] {
			t.Errorf("duplicate builtin %q", theme.Name)
		}
		seen[theme.Name] = true
		if !theme.Builtin {
			t.Errorf("%s: Builtin = false", theme.Name)
		}
		p := theme.Palette
		for _, c := range []string{p.Bg, p.Surface, p.Elevated, p.Fg, p.FgMuted, p.Primary, p.Red, p.Yellow, p.Green, p.Blue} {
			if _, err := ParseHexColor(c); err != nil || len(c) != 7 {
				t.Errorf("%s: color %q is not #rrggbb", theme.Name, c)
			}
		}
	}
	// Spot-check a palette field by field against palettes.rs.
	nord, ok := PaletteByName("nord")
	want := Palette{"#2e3440", "#3b4252", "#434c5e", "#eceff4", "#d8dee9", "#88c0d0", "#bf616a", "#ebcb8b", "#a3be8c", "#81a1c1"}
	if !ok || nord != want {
		t.Errorf("nord = %+v, want %+v", nord, want)
	}
	if _, ok := PaletteByName("nope"); ok {
		t.Error("PaletteByName(nope) found a theme")
	}
	// Every name the variant tables mention is a builtin.
	for _, table := range [][][2]string{themePairs, darkSubvariantLight} {
		for _, pair := range table {
			for _, name := range pair {
				if !seen[name] {
					t.Errorf("variant table names %q, not a builtin", name)
				}
			}
		}
	}
}

func TestAppearanceVariant(t *testing.T) {
	for _, tc := range []struct {
		theme     string
		wantLight bool
		want      string
		ok        bool
	}{
		{"catppuccin-mocha", true, "catppuccin-latte", true},
		{"catppuccin-latte", false, "catppuccin-mocha", true},
		{"catppuccin-macchiato", true, "catppuccin-latte", true},
		{"nightfox-terafox", true, "nightfox-dayfox", true},
		{"nightfox-dayfox", false, "nightfox-nightfox", true},
		{"tokyo-night-day", false, "tokyo-night-night", true},
		// Already in the requested mode.
		{"catppuccin-latte", true, "", false},
		{"catppuccin-mocha", false, "", false},
		{"catppuccin-macchiato", false, "", false},
		// No pair.
		{"dracula", true, "", false},
		{"dracula", false, "", false},
		{"", true, "", false},
	} {
		got, ok := AppearanceVariant(tc.theme, tc.wantLight)
		if got != tc.want || ok != tc.ok {
			t.Errorf("AppearanceVariant(%q, light=%v) = (%q, %v), want (%q, %v)", tc.theme, tc.wantLight, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLoadThemesMissingDirIsBuiltinsOnly(t *testing.T) {
	themes, skipped := LoadThemes(filepath.Join(t.TempDir(), "themes"))
	if len(themes) != len(BuiltinThemes()) || len(skipped) != 0 {
		t.Errorf("missing dir: %d themes, %v skipped; want the builtins alone", len(themes), skipped)
	}
}

func TestLoadThemesDiscoversUserThemes(t *testing.T) {
	dir := ThemesDir(t.TempDir())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	full := `bg = "#000001"
surface = "#000002"
elevated = "#000003"
fg = "#000004"
fg_muted = "#000005"
primary = "#000006"
red = "#000007"
yellow = "#000008"
green = "#000009"
blue = "#00000a"
extra = "ignored"
`
	for name, body := range map[string]string{
		"mine.toml":    full,
		"wayle.toml":   full,                                                     // a builtin's name: skipped
		"broken.toml":  "bg = ",                                                  // unparsable: skipped
		"partial.toml": strings.Replace(full, "fg_muted = \"#000005\"\n", "", 1), // missing field: skipped
		"kebab.toml":   strings.Replace(full, "fg_muted", "fg-muted", 1),         // wrong key spelling: skipped
		"readme.txt":   full,                                                     // not a theme file: ignored
		"dir.toml/x":   "",                                                       // a directory named .toml: unreadable, skipped
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	themes, skipped := LoadThemes(dir)
	builtins := len(BuiltinThemes())
	if len(themes) != builtins+1 {
		t.Fatalf("themes = %d, want the builtins plus mine", len(themes))
	}
	mine := themes[builtins]
	want := Palette{"#000001", "#000002", "#000003", "#000004", "#000005", "#000006", "#000007", "#000008", "#000009", "#00000a"}
	if mine.Name != "mine" || mine.Builtin || mine.Palette != want {
		t.Errorf("user theme = %+v, want mine with the file's palette", mine)
	}
	if themes[0].Palette != wayleTheme {
		t.Error("a user wayle.toml replaced the builtin; duplicates must be skipped")
	}
	if len(skipped) != 5 {
		t.Fatalf("skipped = %v, want wayle, broken, partial, kebab, dir", skipped)
	}
	joined := errors.Join(skipped...).Error()
	for _, want := range []string{"already exists", "broken.toml", `missing field "fg_muted"`, "kebab.toml", "dir.toml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("skipped errors lack %q:\n%s", want, joined)
		}
	}
}
