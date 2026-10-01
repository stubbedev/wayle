package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/hyprland"
)

func TestKeybindModeLabel(t *testing.T) {
	if got := keybindModeLabel("{{ mode }}", "resize"); got != "resize" {
		t.Errorf("= %q", got)
	}
	// An empty submap renders the "default" vocabulary word.
	if got := keybindModeLabel("{{ mode }}", ""); got != i18n.T("bar-keybind-mode-default") {
		t.Errorf("= %q, want default", got)
	}
	if got := keybindModeLabel("[{{ mode }}]", "move"); got != "[move]" {
		t.Errorf("= %q", got)
	}
}

func TestKeybindModeVisibility(t *testing.T) {
	if !keybindModeVisible("resize", true) {
		t.Error("auto-hide hid a real submap")
	}
	if keybindModeVisible("", true) {
		t.Error("auto-hide kept the default submap visible")
	}
	if !keybindModeVisible("", false) {
		t.Error("auto-hide off must always show")
	}
}

func TestParseEventSubmap(t *testing.T) {
	event, ok := hyprland.ParseEvent("submap>>resize")
	if !ok || event.Kind != hyprland.EventSubmap || event.Name != "resize" {
		t.Errorf("= %+v ok=%v", event, ok)
	}
	// Empty payload: the default submap.
	event, ok = hyprland.ParseEvent("submap>>")
	if !ok || event.Name != "" {
		t.Errorf("= %+v ok=%v, want an empty submap", event, ok)
	}
}

func TestLoadFileAppliesKeybindMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.keybind-mode]\nformat = \"[{plugin}] {{ mode }}\"\nauto-hide = true\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.KeybindMode.Format != "[{plugin}] {{ mode }}" || !c.KeybindMode.AutoHide {
		t.Errorf("config = %+v", c.KeybindMode)
	}
	if c.KeybindMode.Icon().Name != "ld-layers-symbolic" {
		t.Errorf("icon = %+v", c.KeybindMode.Icon())
	}

	if err := osWrite(path, "[modules.keybind-mode]\nformat = \"\"\n"); err != nil {
		t.Fatal(err)
	}
	// The schema puts no constraint on the format string.
	if _, err := config.LoadFile(path); err != nil {
		t.Errorf("empty format: accepted by the schema, got %v", err)
	}
}

func TestKeybindModeModuleRequiresHyprland(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = nil
	if _, err := Create("keybind-mode", ctx); err == nil {
		t.Fatal("no Hyprland: want an error, got a module")
	}
}

func TestKeybindModeModuleFollowsSubmaps(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	eventLn := startFakeEventStream(t, []string{"submap>>resize\n"})
	ctx.Hyprland = hyprland.ConnectTo(filepath.Join(t.TempDir(), "unused.sock"), eventLn)

	module, err := Create("keybind-mode", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The icon defaults on, so the root is the bar button carrying both.
	row, ok := module.Root().(*barButton)
	if !ok || row.icon == nil || row.label == nil {
		t.Fatalf("Root = %T, want the bar button with its icon and label", module.Root())
	}
	label := findLabel(row)
	waitForText(t, label, "resize")
}

// findLabel walks a module root for its first label.
func findLabel(w widget.Widget) *widget.Label {
	if label, ok := w.(*widget.Label); ok {
		return label
	}
	if b, ok := w.(*barButton); ok && b.label != nil {
		return b.label
	}
	if box, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, kid := range box.Children() {
			if label := findLabel(kid); label != nil {
				return label
			}
		}
	}
	return nil
}
