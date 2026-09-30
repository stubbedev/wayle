package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLockDefaults pins the schema defaults (lock/mod.rs
// defaults_are_secure_and_sensible): enabled, never on start, a black
// fill, no grace, no cap, never blank, system-auth, the clock shown.
func TestLockDefaults(t *testing.T) {
	l := Defaults().Lock
	if !l.Enabled || l.LockOnStart {
		t.Errorf("enabled/lock-on-start = %v/%v, want true/false", l.Enabled, l.LockOnStart)
	}
	if l.Background.Mode != BackgroundColor || l.Background.Color.String() != "#000000" {
		t.Errorf("background = %v %q, want color #000000", l.Background.Mode, l.Background.Color)
	}
	if l.GracePeriodMS != 0 || l.MaxAttempts != 0 || l.BlankTimeoutMS != 0 || l.Blur != 0 {
		t.Errorf("grace/cap/blank/blur = %d/%d/%d/%d, want all 0", l.GracePeriodMS, l.MaxAttempts, l.BlankTimeoutMS, l.Blur)
	}
	if l.PamService != "system-auth" || !l.ShowFailedAttempts {
		t.Errorf("pam-service/show-failed = %q/%v", l.PamService, l.ShowFailedAttempts)
	}
	at := time.Date(2026, 3, 5, 9, 7, 0, 0, time.UTC)
	if !l.Clock.Show || l.Clock.Time.Format(at) != "09:07" || l.Clock.Date.Format(at) != "Thursday, March 5" {
		t.Errorf("clock = %v %q %q", l.Clock.Show, l.Clock.Time.Format(at), l.Clock.Date.Format(at))
	}
}

func TestLockSectionApplies(t *testing.T) {
	path := writeConfig(t, `
[lock]
enabled = false
lock-on-start = true
background-mode = "image"
background-image = "/srv/lock.png"
background-color = "#1e1e2e80"
blur = 12
show-clock = false
clock-format = "%I:%M %p"
date-format = "%F"
grace-period-ms = 5000
max-attempts = 3
show-failed-attempts = false
blank-timeout-ms = 30000
pam-service = "login"

[wallpaper]
wallpaper = "/srv/wall.jpg"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	l := cfg.Lock
	if l.Enabled || !l.LockOnStart || l.Blur != 12 || l.Clock.Show {
		t.Errorf("flags: %+v", l)
	}
	if l.Background.Mode != BackgroundImage || l.Background.Image != "/srv/lock.png" || l.Background.Color.String() != "#1e1e2e80" {
		t.Errorf("background: %+v", l.Background)
	}
	if l.GracePeriodMS != 5000 || l.MaxAttempts != 3 || l.ShowFailedAttempts || l.BlankTimeoutMS != 30000 || l.PamService != "login" {
		t.Errorf("auth knobs: %+v", l)
	}
	if got := l.Clock.Time.String(); got != "%I:%M %p" {
		t.Errorf("clock-format = %q", got)
	}
	if got := l.Background.ImagePath(cfg.Wallpaper.Wallpaper); got != "/srv/lock.png" {
		t.Errorf("image mode path = %q", got)
	}
	l.Background.Mode = BackgroundWallpaper
	if got := l.Background.ImagePath(cfg.Wallpaper.Wallpaper); got != "/srv/wall.jpg" {
		t.Errorf("wallpaper mode path = %q", got)
	}
	l.Background.Mode = BackgroundColor
	if got := l.Background.ImagePath(cfg.Wallpaper.Wallpaper); got != "" {
		t.Errorf("color mode path = %q, want none", got)
	}
}

// TestLockBadValuesAreLoadErrors: a bad enum, color, format, or a
// negative count fails the load instead of silently defaulting.
func TestLockBadValuesAreLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"mode":     `background-mode = "video"`,
		"color":    `background-color = "black"`,
		"hexlen":   `background-color = "#12345"`,
		"hexchar":  `background-color = "#12345g"`,
		"format":   `clock-format = "%Q"`,
		"negative": `max-attempts = -1`,
		"type":     `enabled = "yes"`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, "[lock]\n"+body+"\n"))
			if err == nil {
				t.Fatalf("[lock] %s loaded without error", body)
			}
			if !strings.Contains(err.Error(), "lock") && name != "negative" && name != "type" {
				t.Errorf("error does not name the section: %v", err)
			}
			if cfg.Lock.Background.Color.String() != "#000000" || !cfg.Lock.Enabled {
				t.Errorf("a failed [lock] leaked partial values: %+v", cfg.Lock)
			}
		})
	}
}

func TestHexColor(t *testing.T) {
	for in, want := range map[string][4]uint8{
		"#000000":   {0, 0, 0, 255},
		"#fff":      {255, 255, 255, 255},
		"#1234":     {0x11, 0x22, 0x33, 0x44},
		"#1e1e2e80": {0x1e, 0x1e, 0x2e, 0x80},
		"#AbCdEf":   {0xab, 0xcd, 0xef, 255},
	} {
		c, err := ParseHexColor(in)
		if err != nil {
			t.Errorf("ParseHexColor(%q): %v", in, err)
			continue
		}
		r, g, b, a := c.RGBA()
		if got := [4]uint8{r, g, b, a}; got != want {
			t.Errorf("%q RGBA = %v, want %v", in, got, want)
		}
		if c.String() != in {
			t.Errorf("String = %q, want %q", c.String(), in)
		}
	}
	for _, bad := range []string{"", "000000", "#", "#12", "#12345", "#1234567", "#xyz"} {
		if _, err := ParseHexColor(bad); err == nil {
			t.Errorf("ParseHexColor(%q) accepted", bad)
		}
	}
	var zero HexColor
	if r, g, b, a := zero.RGBA(); r|g|b != 0 || a != 255 {
		t.Error("the zero HexColor must read as opaque black")
	}
}

func TestGreeterDefaults(t *testing.T) {
	g := Defaults().Greeter
	if g.Background.Mode != BackgroundColor || g.Background.Color.String() != "#000000" {
		t.Errorf("background = %+v", g.Background)
	}
	if !g.Clock.Show || !g.ShowUserList || !g.ShowPowerButtons {
		t.Errorf("toggles = %+v", g)
	}
	if g.CursorSize != 24 || g.CursorTheme != "" || g.CursorSizeExplicit || g.CursorThemeExplicit {
		t.Errorf("cursor = %q %d explicit %v/%v", g.CursorTheme, g.CursorSize, g.CursorThemeExplicit, g.CursorSizeExplicit)
	}
}

// TestLoadGreeterLayers pins config::load: config.toml over defaults,
// then runtime.toml's [greeter] key by key, and a key either layer set
// counts as explicit for cursor resolution.
func TestLoadGreeterLayers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("config.toml", `
[greeter]
background-mode = "image"
background-image = "/etc/wayle/admin.png"
show-user-list = false
cursor-theme = "Adwaita"
`)
	cfg, err := LoadGreeter(cfgPath)
	if err != nil {
		t.Fatalf("LoadGreeter without an overlay: %v", err)
	}
	if !cfg.Greeter.CursorThemeExplicit || cfg.Greeter.CursorSizeExplicit {
		t.Errorf("explicit flags = %v/%v, want theme only", cfg.Greeter.CursorThemeExplicit, cfg.Greeter.CursorSizeExplicit)
	}

	write("runtime.toml", `
[greeter]
background-image = "/etc/wayle/greeter-background.png"
cursor-size = 32

[general]
font-sans = "Ignored"
`)
	cfg, err = LoadGreeter(cfgPath)
	if err != nil {
		t.Fatalf("LoadGreeter: %v", err)
	}
	g := cfg.Greeter
	if g.Background.Mode != BackgroundImage {
		t.Error("runtime.toml reset a key it did not set (background-mode)")
	}
	if g.Background.Image != "/etc/wayle/greeter-background.png" {
		t.Errorf("runtime.toml did not win: image = %q", g.Background.Image)
	}
	if g.ShowUserList {
		t.Error("runtime.toml reset show-user-list")
	}
	if g.CursorTheme != "Adwaita" || g.CursorSize != 32 || !g.CursorSizeExplicit {
		t.Errorf("cursor = %q %d explicit-size %v", g.CursorTheme, g.CursorSize, g.CursorSizeExplicit)
	}
	if cfg.General.FontSans == "Ignored" {
		t.Error("the overlay applies only [greeter]")
	}

	write("runtime.toml", "[greeter]\nbackground-mode = \"slideshow\"\n")
	cfg, err = LoadGreeter(cfgPath)
	if err == nil {
		t.Fatal("a bad runtime.toml value loaded without error")
	}
	if cfg.Greeter.Background.Mode != BackgroundImage {
		t.Error("a failed overlay must leave the config.toml layer in place")
	}
}

func TestLoadGreeterMissingEverythingIsDefaults(t *testing.T) {
	cfg, err := LoadGreeter(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("missing config and overlay: %v", err)
	}
	if !cfg.Greeter.ShowUserList || cfg.Greeter.CursorSize != 24 {
		t.Errorf("not defaults: %+v", cfg.Greeter)
	}
}
