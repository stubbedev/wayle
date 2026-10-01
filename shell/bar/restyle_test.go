package bar

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

func withBg(t *testing.T, cfg *config.Config, hex string) *config.Config {
	t.Helper()
	bg, err := config.ParseHexColor(hex)
	if err != nil {
		t.Fatal(err)
	}
	next := *cfg
	next.Styling.Palette.Bg = bg
	return &next
}

// A config reload recompiles the stylesheet for the new snapshot and
// re-derives the palette in place, so every surface holding the shared
// palette sees the new colors.
func TestReloadRecompilesTheThemeAndUpdatesTheSharedPalette(t *testing.T) {
	isolateConfigDir(t)
	cfg := config.Defaults()
	rt := &barRuntime{theme: newBarTheme(cfg), palette: new(styling.Palette)}
	if err := rt.derive(cfg); err != nil {
		t.Fatal(err)
	}
	shared := rt.palette
	before := *shared
	if rt.paletteStale() {
		t.Fatal("a freshly derived palette is stale")
	}

	next := withBg(t, cfg, "#123456")
	rt.theme.setConfig(next)
	if !rt.paletteStale() {
		t.Fatal("a recompiled theme with a new bg is not stale before derive")
	}
	if err := rt.derive(next); err != nil {
		t.Fatal(err)
	}
	if rt.palette != shared {
		t.Fatal("derive replaced the palette pointer; surfaces holding the old one never see the reload")
	}
	if shared.Bg == before.Bg {
		t.Errorf("shared palette bg unchanged (%#08x) after a bg change", uint32(shared.Bg))
	}
	if !strings.Contains(strings.ToLower(rt.theme.bundle()), "#123456") {
		t.Error("the recompiled bundle does not carry the new bg")
	}

	// Without setConfig the theme keeps compiling the old snapshot.
	stale := &barRuntime{theme: newBarTheme(cfg), palette: new(styling.Palette)}
	if err := stale.derive(next); err != nil {
		t.Fatal(err)
	}
	if stale.palette.Bg != before.Bg {
		t.Error("derive alone changed the palette; the theme's compiled palette is the source")
	}
}

// Modules mount with the module style, whose fg is unset so the
// stylesheet inks their labels; the dropdowns keep the full style.
func TestMountGivesModulesTheUnsetInk(t *testing.T) {
	isolateConfigDir(t)
	cfg := config.Defaults()
	rt := &barRuntime{theme: newBarTheme(cfg), palette: new(styling.Palette)}
	if err := rt.derive(cfg); err != nil {
		t.Fatal(err)
	}
	rt.mount(nil, cfg)
	if rt.ctx.Style != &rt.moduleStyle || rt.ctx.Style.fg != 0 {
		t.Errorf("module style fg = %#08x, want 0 (the stylesheet's ink)", uint32(rt.ctx.Style.fg))
	}
	if rt.style.fg == 0 {
		t.Error("the dropdown style lost its default ink")
	}
	if rt.ctx.Font != rt.font {
		t.Error("modules did not mount with the derived font")
	}
}
