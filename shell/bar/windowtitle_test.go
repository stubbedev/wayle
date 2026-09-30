package bar

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/hyprland"
)

// startFakeEventStream serves one burst of socket2 lines and keeps the
// connection open.
func startFakeEventStream(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".socket2.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(20 * time.Millisecond)
		for _, line := range lines {
			if _, err := conn.Write([]byte(line)); err != nil {
				return
			}
		}
		select {}
	}()
	return path
}

// waitForText polls the label until the expected text lands or the
// deadline passes (the event goroutine schedules through the loop).
func waitForText(t *testing.T, label *widget.Label, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if label.Text() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("label = %q, want %q", label.Text(), want)
}

func TestWindowTitleLabel(t *testing.T) {
	if got := windowTitleLabel("{{ title }}", "vim ~/.zshrc", "foot"); got != "vim ~/.zshrc" {
		t.Errorf("= %q", got)
	}
	if got := windowTitleLabel("{{ app }}: {{ title }}", "doc.txt", "nvim"); got != "nvim: doc.txt" {
		t.Errorf("= %q, want the combined render", got)
	}
	// A blank render (no window, empty format) falls back to Desktop.
	if got := windowTitleLabel("{{ title }}", "", ""); got != i18n.T("bar-window-title-empty") {
		t.Errorf("= %q, want Desktop", got)
	}
}

func TestParseEventActiveWindow(t *testing.T) {
	event, ok := hyprland.ParseEvent("activewindow>>foot,tmux a -t work, more")
	if !ok {
		t.Fatal("parse failed")
	}
	if event.Class != "foot" || event.Title != "tmux a -t work, more" {
		t.Errorf("class=%q title=%q, want the title run to end-of-line", event.Class, event.Title)
	}
	// Focus lost: hyprland emits an empty pair.
	event, ok = hyprland.ParseEvent("activewindow>>,")
	if !ok || event.Class != "" || event.Title != "" {
		t.Errorf("= %+v ok=%v, want an empty window", event, ok)
	}
	// No comma at all fails like the Rust dispatcher's parse error.
	if _, ok := hyprland.ParseEvent("activewindow>>"); ok {
		t.Error("missing comma: want ok=false")
	}
}

func TestWindowTitleModuleFollowsEvents(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	eventLn := startFakeEventStream(t, []string{
		"activewindow>>foot,neovim\n",
	})
	ctx.Hyprland = hyprland.ConnectTo(filepath.Join(t.TempDir(), "unused.sock"), eventLn)

	module, err := Create("window-title", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label, ok := module.Root().(*widget.Label)
	if !ok {
		t.Fatalf("Root = %T, want the label", module.Root())
	}
	waitForText(t, label, "neovim")
}

func TestNewWindowTitleRequiresHyprland(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = nil
	if _, err := Create("window-title", ctx); err == nil {
		t.Fatal("no Hyprland: want an error, got a module")
	}
}

func TestLoadFileAppliesWindowTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.window-title]\nformat = \"{{ app }} | {{ title }}\"\nlabel-max-length = 12\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.WindowTitle.Format != "{{ app }} | {{ title }}" || c.WindowTitle.LabelMaxLength != 12 {
		t.Errorf("config = %+v", c.WindowTitle)
	}

	if err := os.WriteFile(path, []byte("[modules.window-title]\nlabel-max-length = -3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Error("negative max length: want a load error")
	}
}
