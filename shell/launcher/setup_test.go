package launcher

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/launcheripc"
)

func TestSessionCSS(t *testing.T) {
	l := config.Defaults().Launcher
	l.Font = ""
	if css := sessionCSS(launcheripc.SessionOptions{}, l); css != "" {
		t.Errorf("neither font nor style added %q", css)
	}
	css := sessionCSS(launcheripc.SessionOptions{Font: new("Inter 12")}, l)
	if !strings.Contains(css, `font-family: "Inter"`) || !strings.Contains(css, "font-size: 12pt") || !strings.Contains(css, ".launcher-surface") {
		t.Errorf("font css = %q (a preset must not reach the rest of the shell)", css)
	}
	l.Styles = map[string]string{"compact": ".launcher-row { padding: 0; }"}
	if got := sessionCSS(launcheripc.SessionOptions{Style: new("compact")}, l); got != ".launcher-row { padding: 0; }" {
		t.Errorf("preset = %q", got)
	}
	if got := sessionCSS(launcheripc.SessionOptions{Style: new("nope")}, l); got != "" {
		t.Errorf("an unknown preset contributed %q", got)
	}
	both := sessionCSS(launcheripc.SessionOptions{Font: new("Inter 12"), Style: new("compact")}, l)
	if !strings.Contains(both, `font-family: "Inter"`) || !strings.Contains(both, ".launcher-row") {
		t.Errorf("font and preset = %q", both)
	}
}

func TestFontProperties(t *testing.T) {
	// `font: Monospace 20` is not CSS (the size goes first there), so the
	// shorthand silently did nothing.
	p := fontProperties("Monospace 20")
	if !strings.Contains(p, `font-family: "Monospace"`) || !strings.Contains(p, "font-size: 20pt") || strings.Contains(p, "font:") {
		t.Errorf("Monospace 20 = %q", p)
	}
	p = fontProperties("Inter Bold Italic 11")
	if !strings.Contains(p, "font-style: italic") || !strings.Contains(p, "font-weight: 700") {
		t.Errorf("bold italic = %q", p)
	}
	if plain := fontProperties("Inter 11"); strings.Contains(plain, "font-style") || strings.Contains(plain, "font-weight") {
		t.Errorf("a plain description overrode the theme: %q", plain)
	}
	if got := fontProperties("Inter"); got != `font-family: "Inter";` {
		t.Errorf("sizeless = %q", got)
	}
	if got := fontProperties("Inter 14px"); !strings.Contains(got, "font-size: 14px") {
		t.Errorf("absolute size = %q", got)
	}
}

func TestBuildSessionResolvesTheUI(t *testing.T) {
	cfg := config.Defaults()
	cfg.Launcher.History.Enable = false
	cfg.Styling.Scale = 2
	lines := uint32(4)
	loc := uint8(1) // rofi's north-west
	opts := launcheripc.SessionOptions{Dmenu: true, Lines: &lines, Location: &loc, KeepRight: true, Prompt: new("pick")}
	rows := make(chan []string)
	close(rows)
	setup := buildSession(opts, cfg, rows, setupDeps{})
	if len(setup.modes) != 1 || setup.modes[0].Name() != "dmenu" {
		t.Fatalf("modes = %v", setup.modes)
	}
	ui := setup.ui
	if ui.lines != 4 || ui.ellipsize != "start" || deref(ui.prompt, "") != "pick" {
		t.Errorf("ui = %+v", ui)
	}
	if want := int(cfg.Launcher.Width.ResolvePx(config.LauncherWidthBaseRem*16, 2)); ui.width != want {
		t.Errorf("width %d, want %d at styling scale 2", ui.width, want)
	}
	if ui.location != config.LauncherNorthWest {
		t.Errorf("location = %s", ui.location)
	}
	// An out-of-range rofi location keeps the configured one.
	bad := uint8(200)
	opts.Location = &bad
	if got := buildSession(opts, cfg, rows, setupDeps{}).ui.location; got != cfg.Launcher.Location {
		t.Errorf("bad location = %s", got)
	}
}
