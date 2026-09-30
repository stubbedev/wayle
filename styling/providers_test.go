package styling

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
)

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The matugen.rs test fixtures: the old bare-string shape and the new
// {"color": ...} shape.
const matugenOld = `{
  "colors": {
    "background": { "dark": "#101112", "light": "#fafbfc" },
    "surface": { "dark": "#202122", "light": "#eaebec" },
    "on_background": { "dark": "#f0f1f2", "light": "#101112" },
    "on_surface_variant": { "dark": "#a0a1a2", "light": "#505152" },
    "primary": { "dark": "#4090ff", "light": "#2060cc" },
    "secondary": { "dark": "#40ff90", "light": "#20cc60" },
    "tertiary": { "dark": "#ffcf40", "light": "#cc9f20" },
    "error": { "dark": "#ff4040", "light": "#cc2020" }
  }
}`

const matugenNew = `{
  "colors": {
    "background": { "dark": { "color": "#101112" }, "light": { "color": "#fafbfc" } },
    "on_background": { "dark": { "color": "#f0f1f2" }, "light": { "color": "#101112" } },
    "on_surface_variant": { "dark": { "color": "#a0a1a2" }, "light": { "color": "#505152" } },
    "primary": { "dark": { "color": "#4090ff", "tone": 80 }, "light": { "color": "#2060cc" } },
    "secondary": { "dark": { "color": "#40ff90" }, "light": { "color": "#20cc60" } },
    "tertiary": { "dark": { "color": "#ffcf40" }, "light": { "color": "#cc9f20" } },
    "error": { "dark": { "color": "#ff4040" }, "light": { "color": "#cc2020" } }
  }
}`

func TestLoadMatugen(t *testing.T) {
	dir := t.TempDir()
	dark := config.Palette{
		Bg: "#060707", Surface: "#101112", Elevated: "#1a1b1d", Fg: "#f0f1f2", FgMuted: "#a0a1a2",
		Primary: "#4090ff", Red: "#ff4040", Yellow: "#ffcf40", Green: "#40ff90", Blue: "#4090ff",
	}
	for name, body := range map[string]string{"old": matugenOld, "new": matugenNew} {
		path := filepath.Join(dir, name+".json")
		writeJSON(t, path, body)
		got, err := LoadMatugen(path, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != dark {
			t.Errorf("%s dark = %+v, want %+v", name, got, dark)
		}
		light, err := LoadMatugen(path, true)
		if err != nil {
			t.Fatal(err)
		}
		want := config.Palette{
			Bg: "#ffffff", Surface: "#fafbfc", Elevated: "#edf1f4", Fg: "#101112", FgMuted: "#505152",
			Primary: "#2060cc", Red: "#cc2020", Yellow: "#cc9f20", Green: "#20cc60", Blue: "#2060cc",
		}
		if light != want {
			t.Errorf("%s light = %+v, want %+v", name, light, want)
		}
	}
}

func TestLoadPywal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.json")
	writeJSON(t, path, `{
  "wallpaper": "/x.png",
  "special": { "background": "#11111b", "foreground": "#cdd6f4", "cursor": "#fff" },
  "colors": { "color0": "#000000", "color1": "#f38ba8", "color2": "#a6e3a1", "color3": "#f9e2af",
              "color4": "#74c7ec", "color5": "#ff00ff", "color7": "#bac2de" }
}`)
	got, err := LoadPywal(path, false)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Palette{
		Bg: "#09090e", Surface: "#11111b", Elevated: "#191928", Fg: "#cdd6f4", FgMuted: "#bac2de",
		Primary: "#74c7ec", Red: "#f38ba8", Yellow: "#f9e2af", Green: "#a6e3a1", Blue: "#74c7ec",
	}
	if got != want {
		t.Errorf("pywal = %+v, want %+v", got, want)
	}
	light, err := LoadPywal(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if light.Bg != "#191928" || light.Elevated != "#09090e" {
		t.Errorf("light layers = %s/%s, want the flipped ramp", light.Bg, light.Elevated)
	}
}

func TestLoadWallust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallust-colors.json")
	writeJSON(t, path, `{"background": "#e0e0e0", "foreground": "#101010", "cursor": "#000",
  "color3": "#aa0000", "color4": "#00aa00", "color5": "#aaaa00", "color6": "#0000aa", "color7": "#555555"}`)
	got, err := LoadWallust(path, true)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Palette{
		Bg: "#eaeaea", Surface: "#e0e0e0", Elevated: "#d6d6d6", Fg: "#101010", FgMuted: "#555555",
		Primary: "#0000aa", Red: "#aa0000", Yellow: "#aaaa00", Green: "#00aa00", Blue: "#0000aa",
	}
	if got != want {
		t.Errorf("wallust = %+v, want %+v", got, want)
	}
}

func TestProviderLoadFailures(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	for name, load := range map[string]func(string, bool) (config.Palette, error){
		"matugen": LoadMatugen, "pywal": LoadPywal, "wallust": LoadWallust,
	} {
		if _, err := load(missing, false); !errors.Is(err, ErrPaletteNotFound) {
			t.Errorf("%s missing file: err = %v, want ErrPaletteNotFound", name, err)
		}
		bad := filepath.Join(dir, name+"-bad.json")
		writeJSON(t, bad, `{"colors": `)
		if _, err := load(bad, false); err == nil || !strings.Contains(err.Error(), "cannot parse palette JSON") {
			t.Errorf("%s bad JSON: err = %v", name, err)
		}
		empty := filepath.Join(dir, name+"-empty.json")
		writeJSON(t, empty, `{}`)
		if _, err := load(empty, false); err == nil || !strings.Contains(err.Error(), "missing field") {
			t.Errorf("%s empty object: err = %v, want a missing field", name, err)
		}
	}
	for _, tc := range []struct{ name, body, want string }{
		{"number color", strings.Replace(matugenOld, `"#4090ff"`, `42`, 1), "untagged"},
		{"object without color", strings.Replace(matugenOld, `"#4090ff"`, `{"tone": 1}`, 1), "untagged"},
		{"missing light", strings.Replace(matugenOld, `, "light": "#cc2020"`, ``, 1), `"error.light"`},
		{"missing tertiary", strings.Replace(matugenOld, `"tertiary"`, `"quaternary"`, 1), `"tertiary"`},
	} {
		path := filepath.Join(dir, "m.json")
		writeJSON(t, path, tc.body)
		if _, err := LoadMatugen(path, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("matugen %s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	pywal := filepath.Join(dir, "p.json")
	writeJSON(t, pywal, `{"special": {"background": "#000000", "foreground": "#ffffff"}, "colors": {"color1": "#1", "color2": "#2", "color3": "#3", "color4": "#4"}}`)
	if _, err := LoadPywal(pywal, false); err == nil || !strings.Contains(err.Error(), `"color7"`) {
		t.Errorf("pywal without color7: err = %v", err)
	}
	wallust := filepath.Join(dir, "w.json")
	writeJSON(t, wallust, `{"background": 5}`)
	if _, err := LoadWallust(wallust, false); err == nil {
		t.Error("wallust with a numeric background: want an error")
	}
}

func TestResolvePalette(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	fallback := config.DefaultsStyling().ActivePalette()
	writeJSON(t, filepath.Join(cache, "wayle", "matugen-colors.json"), matugenOld)
	writeJSON(t, filepath.Join(cache, "wal", "colors.json"), `{"special": {"background": "#e0e0e0", "foreground": "#101010"},
  "colors": {"color1": "#1", "color2": "#2", "color3": "#3", "color4": "#4", "color7": "#7"}}`)

	s := config.DefaultsStyling()
	ce := config.DefaultsColorExtractor()
	if got, err := ResolvePalette(fallback, s, ce); err != nil || got != fallback {
		t.Errorf("wayle provider = %+v, %v; want the fallback, no error", got, err)
	}

	ce.ThemeProvider = config.ThemeMatugen
	got, err := ResolvePalette(fallback, s, ce)
	if err != nil || got.Primary != "#4090ff" {
		t.Errorf("matugen dark = %+v, %v", got, err)
	}
	ce.Extractor.MatugenLight = true
	if got, _ := ResolvePalette(fallback, s, ce); got.Primary != "#2060cc" {
		t.Errorf("matugen-light = %s, want the light variant", got.Primary)
	}
	// A forced appearance overrides the provider's own light flag.
	s.Appearance = config.AppearanceDark
	if got, _ := ResolvePalette(fallback, s, ce); got.Primary != "#4090ff" {
		t.Errorf("forced dark over matugen-light = %s, want the dark variant", got.Primary)
	}

	light := config.DefaultsStyling()
	light.Appearance = config.AppearanceLight
	pywal := config.DefaultsColorExtractor()
	pywal.ThemeProvider = config.ThemePywal
	if got, _ := ResolvePalette(fallback, light, pywal); got.Bg != "#eaeaea" {
		t.Errorf("forced light pywal bg = %s, want the light ramp", got.Bg)
	}

	// No wallust file: the fallback, with the failure to log.
	wallust := config.DefaultsColorExtractor()
	wallust.ThemeProvider = config.ThemeWallust
	got, err = ResolvePalette(fallback, config.DefaultsStyling(), wallust)
	if !errors.Is(err, ErrPaletteNotFound) || got != fallback {
		t.Errorf("missing wallust = %+v, %v; want the fallback and ErrPaletteNotFound", got, err)
	}
}

func TestResolvePaletteWithoutCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	fallback := config.DefaultsStyling().ActivePalette()
	ce := config.DefaultsColorExtractor()
	ce.ThemeProvider = config.ThemePywal
	if got, err := ResolvePalette(fallback, config.DefaultsStyling(), ce); err == nil || got != fallback {
		t.Errorf("no cache home = %+v, %v; want the fallback and an error", got, err)
	}
}
