package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileMissingIsDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("LoadFile missing file: %v", err)
	}
	want := Defaults()
	if cfg.Bar.Location != want.Bar.Location || cfg.Bar.Exclusive != want.Bar.Exclusive ||
		cfg.Clock != want.Clock || cfg.General != want.General {
		t.Fatalf("missing file: got %+v, want defaults %+v", cfg, want)
	}
}

func TestLoadFileAppliesBarSection(t *testing.T) {
	path := writeConfig(t, `
[bar]
location = "bottom"
exclusive = false
module-gap = "4px"
padding = 0.35
background-opacity = 0

[[bar.layout]]
monitor = "DP-1"
extends = "*"
left = ["clock"]
center = [{ module = "clock", class = "primary-clock" }]
right = [{ name = "status", modules = ["battery", "network"] }]

[[bar.layout]]
monitor = "*"
center = ["clock"]

[modules.clock]
format = "%Y-%m-%d %H:%M"

[general]
font-sans = "JetBrainsMono Nerd Font"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	bar := cfg.Bar
	if bar.Location != LocationBottom {
		t.Errorf("location = %q, want bottom", bar.Location)
	}
	if bar.Exclusive {
		t.Errorf("exclusive = true, want false")
	}
	if got := bar.ModuleGap; got.Unit != SizePixels || got.Value != 4 {
		t.Errorf("module-gap = %+v, want 4px", got)
	}
	if bar.BackgroundOpacity != 0 {
		t.Errorf("background-opacity = %d, want 0 (explicit zero overrides the default 100)", bar.BackgroundOpacity)
	}
	if len(bar.Layout) != 2 {
		t.Fatalf("layout entries = %d, want 2", len(bar.Layout))
	}
	dp1 := bar.Layout[0]
	if len(dp1.Left) != 1 || dp1.Left[0].Module != "clock" || dp1.Left[0].IsGroup() {
		t.Errorf("DP-1 left = %+v, want one bare clock", dp1.Left)
	}
	if center := dp1.Center; len(center) != 1 || center[0].Module != "clock" || center[0].Class != "primary-clock" {
		t.Errorf("DP-1 center = %+v, want classed clock", dp1.Center)
	}
	group := dp1.Right
	if len(group) != 1 || !group[0].IsGroup() {
		t.Fatalf("DP-1 right = %+v, want one group", group)
	}
	if group[0].Group.Name != "status" || len(group[0].Group.Modules) != 2 {
		t.Errorf("group = %+v, want status[battery network]", group[0].Group)
	}
	if cfg.Clock.Format != "%Y-%m-%d %H:%M" {
		t.Errorf("clock format = %q, want %%Y-%%m-%%d %%H:%%M", cfg.Clock.Format)
	}
	if cfg.General.FontSans != "JetBrainsMono Nerd Font" {
		t.Errorf("font-sans = %q", cfg.General.FontSans)
	}
}

func TestLoadFileUnknownKeysAndSectionsAreIgnored(t *testing.T) {
	path := writeConfig(t, `
[osd]
unknown-key = true

[bar]
not-in-the-schema = 3
location = "bottom"

[modules.clock]
also-unknown = 1
`)
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile with unknown keys: %v", err)
	}
}

func TestLoadFileInvalidLocationErrorsAndFallsBack(t *testing.T) {
	path := writeConfig(t, "[bar]\nlocation = \"sideways\"\n")
	cfg, err := LoadFile(path)
	if err == nil {
		t.Fatal("invalid location: want error, got nil")
	}
	if !strings.Contains(err.Error(), "sideways") {
		t.Errorf("error %q does not name the bad value", err)
	}
	if cfg.Bar.Location != LocationTop {
		t.Errorf("fallback location = %q, want top (defaults, not the bad value)", cfg.Bar.Location)
	}
}

func TestLoadFileInvalidLayerErrors(t *testing.T) {
	path := writeConfig(t, "[bar]\nlayer = \"middle\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("invalid layer: want error, got nil")
	}
}

func TestLoadFileInvalidRoundingErrors(t *testing.T) {
	path := writeConfig(t, "[bar]\nrounding = \"xl\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("invalid rounding: want error, got nil")
	}
}

func TestLoadFileInvalidSizeErrors(t *testing.T) {
	for _, content := range []string{
		"[bar]\nmodule-gap = \"4em\"\n",
		"[bar]\nmodule-gap = \"huge\"\n",
		"[bar]\nmodule-gap = -1\n",
	} {
		path := writeConfig(t, content)
		if _, err := LoadFile(path); err == nil {
			t.Errorf("size %q: want error, got nil", content)
		}
	}
}

func TestLoadFileMalformedTOMLErrors(t *testing.T) {
	path := writeConfig(t, "[bar\nlocation = ")
	cfg, err := LoadFile(path)
	if err == nil {
		t.Fatal("malformed TOML: want error, got nil")
	}
	if cfg == nil || cfg.Bar.Location != LocationTop {
		t.Fatalf("malformed TOML: cfg %+v, want defaults", cfg)
	}
}

func TestLoadFileOpacityAndScaleRanges(t *testing.T) {
	path := writeConfig(t, "[bar]\nbackground-opacity = 120\n")
	if _, err := LoadFile(path); err == nil {
		t.Error("background-opacity 120: want error, got nil")
	}
	path = writeConfig(t, "[bar]\nscale = 5.0\n")
	if _, err := LoadFile(path); err == nil {
		t.Error("scale 5.0: want error, got nil")
	}
}

func TestLoadFileLayoutWithoutMonitorErrors(t *testing.T) {
	path := writeConfig(t, "[[bar.layout]]\nleft = [\"clock\"]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("layout without monitor: want error, got nil")
	}
}

func TestLoadFileGroupWithoutModulesErrors(t *testing.T) {
	path := writeConfig(t, "[[bar.layout]]\nmonitor = \"*\"\nleft = [{ name = \"status\" }]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("group without modules: want error, got nil")
	}
}

func TestLoadFileItemWithoutModuleErrors(t *testing.T) {
	path := writeConfig(t, "[[bar.layout]]\nmonitor = \"*\"\nleft = [{ class = \"x\" }]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("item without module: want error, got nil")
	}
}

func TestLoadFileSelfExtendingLayoutErrors(t *testing.T) {
	path := writeConfig(t, "[[bar.layout]]\nmonitor = \"DP-1\"\nextends = \"DP-1\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("self-extending layout: want error, got nil")
	}
}

func TestLoadFileButtonRounding(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, "[bar]\nbutton-rounding = \"lg\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bar.ButtonRounding != RoundingLg || cfg.Bar.ButtonGroupRounding != RoundingSm {
		t.Errorf("rounding = %q/%q, want lg with the group untouched", cfg.Bar.ButtonRounding, cfg.Bar.ButtonGroupRounding)
	}
	if _, err := LoadFile(writeConfig(t, "[bar]\nbutton-rounding = \"round\"\n")); err == nil || !strings.Contains(err.Error(), "button-rounding") {
		t.Errorf("bad button-rounding: err = %v", err)
	}
}

func TestLoadFileButtonBorder(t *testing.T) {
	cfg := Defaults()
	if cfg.Bar.ButtonBorderLocation != BorderAll || cfg.Bar.ButtonBorderWidth != 1 {
		t.Fatalf("defaults = %q/%d, want all/1", cfg.Bar.ButtonBorderLocation, cfg.Bar.ButtonBorderWidth)
	}
	cfg, err := LoadFile(writeConfig(t, "[bar]\nbutton-border-location = \"bottom\"\nbutton-border-width = 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bar.ButtonBorderLocation != BorderBottom || cfg.Bar.ButtonBorderWidth != 3 {
		t.Errorf("got %q/%d", cfg.Bar.ButtonBorderLocation, cfg.Bar.ButtonBorderWidth)
	}
	for _, body := range []string{"button-border-location = \"middle\"", "button-border-width = 300"} {
		if _, err := LoadFile(writeConfig(t, "[bar]\n"+body+"\n")); err == nil {
			t.Errorf("%s: want error", body)
		}
	}
}
