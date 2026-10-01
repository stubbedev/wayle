package config

import (
	"strings"
	"testing"
)

func TestWallpaperSectionLoads(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", `[wallpaper]
wallpaper = "/w/a.png"
fit-mode = "center"
cycling-mode = "shuffle"
cycling-interval-mins = 30

[[wallpaper.monitors]]
name = "DP-1"
wallpaper = "/w/dp.png"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Wallpaper
	if w.Wallpaper != "/w/a.png" || w.FitMode != FitCenter || w.CyclingMode != CyclingShuffle || w.CyclingIntervalMins != 30 {
		t.Errorf("wallpaper = %+v", w)
	}
	m, ok := w.Monitor("DP-1")
	if !ok || m.Wallpaper != "/w/dp.png" || m.FitMode != FitFill {
		t.Errorf("monitor = %+v %v, want the entry with the Fill field default", m, ok)
	}
	if _, ok := w.Monitor("HDMI-1"); ok {
		t.Error("an unlisted monitor resolved")
	}
}

func TestWallpaperIntervalClampsToItsFloor(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", "[wallpaper]\ncycling-interval-mins = 0\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("a clamp is a warning, not an error: %v", err)
	}
	if cfg.Wallpaper.CyclingIntervalMins != CyclingIntervalMin {
		t.Errorf("interval = %d, want the floor", cfg.Wallpaper.CyclingIntervalMins)
	}
}

func TestWallpaperBadValuesAreFieldDiagnostics(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", `[wallpaper]
fit-mode = "tile"
wallpaper = "/w/a.png"

[[wallpaper.monitors]]
wallpaper = "/w/nameless.png"
`)
	cfg, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "wallpaper.fit-mode") || !strings.Contains(err.Error(), "missing field `name`") {
		t.Fatalf("diagnostics = %v, want the bad fit-mode and the nameless monitor", err)
	}
	w := cfg.Wallpaper
	if w.FitMode != FitFill || len(w.Monitors) != 0 {
		t.Errorf("bad fields did not keep their defaults: %+v", w)
	}
	if w.Wallpaper != "/w/a.png" {
		t.Errorf("a good field beside bad ones was dropped: %q", w.Wallpaper)
	}
}
