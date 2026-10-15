package bar

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
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

// The module reads the focused window at start, follows focus and
// title changes, reads blank (Desktop and icon-name) with nothing
// focused, and resolves the class's icon.
func TestWindowTitleModuleFollowsTheFocusedWindow(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	f, conn := startFakeHypr(t, map[string]string{
		"j/activewindow": `{"address":"0xaa","class":"foot","title":"neovim"}`,
	})
	ctx.Hyprland = conn
	module, err := Create("window-title", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*windowTitleModule)
	label := findLabel(module.Root())
	icon := func() string { return onHeadlessLoop(func() string { return m.icon.Name() }) }
	if label.Text() != "neovim" || icon() != "ld-terminal-symbolic" {
		t.Fatalf("start: label %q icon %q, want the snapshot and foot's icon", label.Text(), icon())
	}

	f.set("j/activewindow", `{"address":"0xbb","class":"firefox","title":"Mozilla"}`)
	f.emit(t, "activewindow>>firefox,Mozilla")
	waitForText(t, label, "Mozilla")
	if got := icon(); got != "si-firefox-symbolic" {
		t.Errorf("firefox icon = %q", got)
	}

	f.set("j/activewindow", `{"address":"0xbb","class":"firefox","title":"Docs"}`)
	f.emit(t, "windowtitlev2>>bb,Docs")
	waitForText(t, label, "Docs")

	f.set("j/activewindow", `{}`)
	f.emit(t, "activewindow>>,")
	waitForText(t, label, i18n.T("bar-window-title-empty"))
	if got := icon(); got != cfg.WindowTitle.IconName {
		t.Errorf("nothing focused icon = %q, want icon-name", got)
	}
}

// resolve_icon's order: the user's title: patterns against the title,
// the user's class patterns (pattern order) and then the built-ins
// against the class, then icon-name.
func TestWindowTitleIcon(t *testing.T) {
	cfg := config.DefaultsWindowTitle()
	cfg.IconMappings = map[string]string{
		"title:*YouTube*": "yt",
		"*fire*":          "user-fire",
		"*firefox*":       "user-firefox",
	}
	for _, tc := range []struct{ title, app, want string }{
		{"Cats - YouTube", "firefox", "yt"},
		{"Docs", "firefox", "user-fire"},
		{"term", "foot", "ld-terminal-symbolic"},
		{"x", "unknown-app", cfg.IconName},
		{"YouTube", "", "yt"},
	} {
		if got := windowTitleIcon(cfg, tc.title, tc.app); got != tc.want {
			t.Errorf("%q/%q = %q, want %q", tc.title, tc.app, got, tc.want)
		}
	}
}

// Without a supported compositor the module is skipped with an error.
func TestNewWindowTitleNeedsACompositor(t *testing.T) {
	for _, env := range []string{niri.SocketEnv, mango.SocketEnv, "SWAYSOCK"} {
		t.Setenv(env, "")
	}
	ctx := newTestContext(t, config.Defaults())
	ctx.Hyprland = nil
	if _, err := Create("window-title", ctx); err == nil {
		t.Fatal("no compositor: want an error, got a module")
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
