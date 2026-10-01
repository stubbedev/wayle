package bar

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
)

func TestMainKeyboardLayoutPicksMainNonVirtual(t *testing.T) {
	devices := hyprland.Devices{Keyboards: []hyprland.KeyboardDevice{
		{Name: "hl-virtual-keyboard-1", Main: true, ActiveKeymap: "us"},
		{Name: "at-translated-set-2-keyboard", Main: false, ActiveKeymap: "de"},
	}}
	if got := mainKeyboardLayout(devices); got != "de" {
		t.Errorf("layout = %q, want de (virtual keyboards never win)", got)
	}
}

func TestMainKeyboardLayoutPrefersMainFlag(t *testing.T) {
	devices := hyprland.Devices{Keyboards: []hyprland.KeyboardDevice{
		{Name: "usb-keyboard", Main: false, ActiveKeymap: "us"},
		{Name: "at-translated-set-2-keyboard", Main: true, ActiveKeymap: "de(nodeadkeys)"},
	}}
	if got := mainKeyboardLayout(devices); got != "de(nodeadkeys)" {
		t.Errorf("layout = %q, want the main keyboard's", got)
	}
}

func TestMainKeyboardLayoutSkipsPlaceholderKeymaps(t *testing.T) {
	devices := hyprland.Devices{Keyboards: []hyprland.KeyboardDevice{
		{Name: "at-translated-set-2-keyboard", Main: true, ActiveKeymap: "error"},
		{Name: "usb-keyboard", Main: false, ActiveKeymap: "none"},
		{Name: "third", Main: false, ActiveKeymap: "us"},
	}}
	if got := mainKeyboardLayout(devices); got != "us" {
		t.Errorf("layout = %q, want the first keyboard with a real keymap", got)
	}
	// All placeholders: no layout at all.
	devices.Keyboards = devices.Keyboards[:2]
	if got := mainKeyboardLayout(devices); got != "" {
		t.Errorf("layout = %q, want empty", got)
	}
}

func TestKeyboardLayoutLabelMatchesRustAssertions(t *testing.T) {
	// helpers.rs's tests.
	if got := keyboardLayoutLabel("us", "{{ layout }}", nil); got != "us" {
		t.Errorf("= %q, want us", got)
	}
	if got := keyboardLayoutLabel("de", "KB: {{ layout }}", nil); got != "KB: de" {
		t.Errorf("= %q, want the prefixed layout", got)
	}
	aliasMap := map[string]string{"us": "EN"}
	if got := keyboardLayoutLabel("us", "{{ alias }}", aliasMap); got != "EN" {
		t.Errorf("= %q, want the alias", got)
	}
	if got := keyboardLayoutLabel("de", "{{ layout }} | {{ alias }}", aliasMap); got != "de | de" {
		t.Errorf("= %q, want layout twice (alias falls back)", got)
	}
}

// newFakeDevicesConn serves j/devices through a fake command socket
// plus an (empty) event socket the module can subscribe to.
func newFakeDevicesConn(t *testing.T, devicesJSON string) *hyprland.Connection {
	t.Helper()
	dir := t.TempDir()
	commandPath := filepath.Join(dir, ".socket.sock")
	eventPath := filepath.Join(dir, ".socket2.sock")
	ln, err := net.Listen("unix", commandPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	eventLn, err := net.Listen("unix", eventPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eventLn.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				if string(buf[:n]) == "j/devices" {
					c.Write([]byte(devicesJSON))
					c.Write([]byte{'\x04'})
				}
			}(conn)
		}
	}()
	return hyprland.ConnectTo(commandPath, eventPath)
}

func TestKeyboardLayoutModuleRenders(t *testing.T) {
	devicesJSON := `{"keyboards":[{"name":"at-translated-set-2-keyboard","main":true,"active_keymap":"de"},{"name":"hl-virtual-keyboard-x","main":true,"active_keymap":"none"}]}`
	cfg := config.Defaults()
	cfg.KeyboardInput.LayoutAliasMap = map[string]string{"de": "DE"}
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = newFakeDevicesConn(t, devicesJSON)

	module, err := Create("keyboard-input", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label, ok := module.Root().(*widget.Label)
	if !ok {
		t.Fatalf("Root = %T, want the label", module.Root())
	}
	if got := label.Text(); got != "DE" {
		t.Errorf("label = %q, want the de alias", got)
	}
}

func TestNewKeyboardLayoutRequiresHyprland(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = nil
	if _, err := Create("keyboard-input", ctx); err == nil {
		t.Fatal("no Hyprland: want an error, got a module")
	}
}

func TestLoadFileAppliesKeyboardLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.keyboard-input]\nformat = \"KB {{ layout }}\"\nlabel-show = false\n\n[modules.keyboard-input.layout-alias-map]\nus = \"EN\"\nde = \"DE\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.KeyboardInput.Format != "KB {{ layout }}" || c.KeyboardInput.LabelShow {
		t.Errorf("config = %+v", c.KeyboardInput)
	}
	if c.KeyboardInput.LayoutAliasMap["us"] != "EN" || c.KeyboardInput.LayoutAliasMap["de"] != "DE" {
		t.Errorf("alias map = %+v", c.KeyboardInput.LayoutAliasMap)
	}
}

func TestLoadFileRejectsBadKeyboardLayout(t *testing.T) {
	for _, content := range []string{
		"[modules.keyboard-input]\n[modules.keyboard-input.layout-alias-map]\nus = 3\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}
