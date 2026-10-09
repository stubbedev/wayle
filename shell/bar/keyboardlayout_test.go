package bar

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
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

// clearCompositorEnv hides every compositor socket the host session
// advertises, so each test names the one it fakes.
func clearCompositorEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HYPRLAND_INSTANCE_SIGNATURE", niri.SocketEnv, mango.SocketEnv, "SWAYSOCK"} {
		t.Setenv(name, "")
	}
}

// waitLabel polls the label until it reads want.
func waitLabel(t *testing.T, label *widget.Label, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		headlessLoop.Lock()
		got := label.Text()
		headlessLoop.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("label = %q, want %q", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestKeyboardLayout(t *testing.T, cfg *config.Config) *widget.Label {
	t.Helper()
	module, err := Create("keyboard-input", newTestContext(t, cfg))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label, ok := module.Root().(*widget.Label)
	if !ok {
		t.Fatalf("Root = %T, want the label", module.Root())
	}
	return label
}

func TestNewKeyboardLayoutRefusesAnUnknownCompositor(t *testing.T) {
	clearCompositorEnv(t)
	ctx := newTestContext(t, config.Defaults())
	if _, err := Create("keyboard-input", ctx); err == nil {
		t.Fatal("no supported compositor: want an error, got a module")
	}
}

// fakeSwayInputs answers GET_INPUTS (100) with the current inputs and,
// once subscribed, sends one input event per value on events after
// switching the inputs to it.
func fakeSwayInputs(t *testing.T, inputs string, events chan string) {
	t.Helper()
	var mu sync.Mutex
	frame := func(conn net.Conn, msgType uint32, body string) {
		out := make([]byte, 14, 14+len(body))
		copy(out, "i3-ipc")
		binary.LittleEndian.PutUint32(out[6:], uint32(len(body)))
		binary.LittleEndian.PutUint32(out[10:], msgType)
		_, _ = conn.Write(append(out, body...))
	}
	path := serveUnix(t, "sway.sock", func(conn net.Conn) {
		for {
			head := make([]byte, 14)
			if _, err := io.ReadFull(conn, head); err != nil {
				return
			}
			payload := make([]byte, binary.LittleEndian.Uint32(head[6:]))
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			switch msgType := binary.LittleEndian.Uint32(head[10:]); msgType {
			case 100:
				mu.Lock()
				body := inputs
				mu.Unlock()
				frame(conn, msgType, body)
			case 2:
				if string(payload) != `["input"]` {
					frame(conn, msgType, `{"success": false}`)
					return
				}
				frame(conn, msgType, `{"success": true}`)
				for next := range events {
					mu.Lock()
					inputs = next
					mu.Unlock()
					frame(conn, 0x80000015, `{"change":"xkb_layout"}`)
				}
				return
			default:
				frame(conn, msgType, "[]")
			}
		}
	})
	t.Setenv("SWAYSOCK", path)
}

func TestKeyboardLayoutFollowsSwayInputEvents(t *testing.T) {
	clearCompositorEnv(t)
	events := make(chan string, 1)
	t.Cleanup(func() { close(events) })
	fakeSwayInputs(t, `[{"type":"pointer"},{"type":"keyboard","xkb_active_layout_name":"German"}]`, events)
	cfg := config.Defaults()
	cfg.KeyboardInput.LayoutAliasMap = map[string]string{"German": "DE"}
	label := newTestKeyboardLayout(t, cfg)
	if got := label.Text(); got != "DE" {
		t.Errorf("label = %q, want the German alias", got)
	}
	// An input event re-reads the inputs.
	events <- `[{"type":"keyboard","xkb_active_layout_name":"English (US)"}]`
	waitLabel(t, label, "English (US)")
	// A keyboard without a layout blanks the label rather than keeping
	// the stale one.
	events <- `[{"type":"keyboard"}]`
	waitLabel(t, label, "")
}

// fakeNiriLayouts answers KeyboardLayouts with the current index and,
// on the event stream, sends one KeyboardLayoutSwitched per index on
// switches after switching to it.
func fakeNiriLayouts(t *testing.T, names string, switches chan int) {
	t.Helper()
	var mu sync.Mutex
	current := 0
	path := serveUnix(t, "niri.sock", func(conn net.Conn) {
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch line {
			case "\"KeyboardLayouts\"\n":
				mu.Lock()
				idx := current
				mu.Unlock()
				_, _ = fmt.Fprintf(conn, `{"Ok":{"KeyboardLayouts":{"names":%s,"current_idx":%d}}}`+"\n", names, idx)
			case "\"EventStream\"\n":
				_, _ = conn.Write([]byte("{\"Ok\":\"Handled\"}\n"))
				for idx := range switches {
					mu.Lock()
					current = idx
					mu.Unlock()
					_, _ = fmt.Fprintf(conn, `{"KeyboardLayoutSwitched":{"idx":%d}}`+"\n", idx)
				}
				return
			}
		}
	})
	t.Setenv(niri.SocketEnv, path)
}

func TestKeyboardLayoutFollowsNiriLayoutSwitches(t *testing.T) {
	clearCompositorEnv(t)
	switches := make(chan int, 1)
	t.Cleanup(func() { close(switches) })
	fakeNiriLayouts(t, `["English (US)","German"]`, switches)
	label := newTestKeyboardLayout(t, config.Defaults())
	if got := label.Text(); got != "English (US)" {
		t.Errorf("label = %q, want the first layout", got)
	}
	switches <- 1
	waitLabel(t, label, "German")
	// An index past the names is no layout.
	switches <- 5
	waitLabel(t, label, "")
}

func TestKeyboardLayoutFollowsMangosActiveMonitor(t *testing.T) {
	clearCompositorEnv(t)
	fakeMangoSocket(t,
		`[{"name":"DP-1","active":false,"tags":[],"active_tags":[],"keyboardlayout":"German"},`+
			`{"name":"eDP-1","active":true,"tags":[],"active_tags":[],"keyboardlayout":"English (US)"}]`,
		`[]`)
	label := newTestKeyboardLayout(t, config.Defaults())
	// The frames stream in after the module mounts.
	waitLabel(t, label, "English (US)")
}

func TestKeyboardLayoutHidesWithLabelShowOff(t *testing.T) {
	clearCompositorEnv(t)
	events := make(chan string)
	t.Cleanup(func() { close(events) })
	fakeSwayInputs(t, `[{"type":"keyboard","xkb_active_layout_name":"German"}]`, events)
	cfg := config.Defaults()
	cfg.KeyboardInput.LabelShow = false
	if got := newTestKeyboardLayout(t, cfg).Text(); got != "" {
		t.Errorf("label = %q, want none with label-show off", got)
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
