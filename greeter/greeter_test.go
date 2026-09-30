package greeter

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/config"
)

func TestParseOptions(t *testing.T) {
	o, err := ParseOptions([]string{"--", "niri", "--session"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Command, []string{"niri", "--session"}) || o.ConfigPath != config.GreeterConfigPath {
		t.Errorf("command after --: %+v", o)
	}
	if len(o.SessionDirs) != 2 || len(o.XSessionDirs) != 2 {
		t.Errorf("default dirs: %v %v", o.SessionDirs, o.XSessionDirs)
	}

	o, err = ParseOptions([]string{"--config", "/tmp/c.toml", "--env", "XDG_SESSION_TYPE=wayland",
		"--sessions", "/a", "--sessions", "/b", "--xsessions", "/x", "--state", "/run/g/last", "--", "sway"})
	if err != nil {
		t.Fatal(err)
	}
	if o.ConfigPath != "/tmp/c.toml" || !reflect.DeepEqual(o.Env, []string{"XDG_SESSION_TYPE=wayland"}) ||
		!reflect.DeepEqual(o.SessionDirs, []string{"/a", "/b"}) || !reflect.DeepEqual(o.XSessionDirs, []string{"/x"}) ||
		o.StatePath != "/run/g/last" || !reflect.DeepEqual(o.Command, []string{"sway"}) {
		t.Errorf("full parse: %+v", o)
	}
	if LastUserPath(o.StatePath) != "/run/g/last-user" {
		t.Errorf("last-user path = %s", LastUserPath(o.StatePath))
	}

	for _, bad := range [][]string{{"--config"}, {"--env", "NOEQUALS"}, {"--bogus"}, {"--sessions"}, {"--state"}} {
		_, err := ParseOptions(bad)
		var usage UsageError
		if !errors.As(err, &usage) || !strings.Contains(err.Error(), "usage: wayle-greeter") {
			t.Errorf("ParseOptions(%q) = %v, want a usage error", bad, err)
		}
	}

	t.Setenv("XDG_STATE_HOME", "/state")
	if o, _ := ParseOptions(nil); o.StatePath != "/state/wayle-greeter/last-session" {
		t.Errorf("XDG_STATE_HOME default = %s", o.StatePath)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/var/lib/greetd")
	if o, _ := ParseOptions(nil); o.StatePath != "/var/lib/greetd/.local/state/wayle-greeter/last-session" {
		t.Errorf("HOME default = %s", o.StatePath)
	}
	t.Setenv("HOME", "")
	if o, _ := ParseOptions(nil); o.StatePath != defaultStatePath {
		t.Errorf("fallback = %s", o.StatePath)
	}
}

func TestLastRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last-session")
	if LoadLast(path) != "" {
		t.Error("a missing state file reads as a value")
	}
	if err := SaveLast(path, "sway"); err != nil {
		t.Fatal(err)
	}
	if LoadLast(path) != "sway" {
		t.Errorf("round trip = %q", LoadLast(path))
	}
	_ = os.WriteFile(path, []byte("  \n"), 0o600)
	if LoadLast(path) != "" {
		t.Error("a blank state file reads as a value")
	}
}

func TestParseDesktop(t *testing.T) {
	s, ok := parseDesktop("[Desktop Entry]\nName=Sway\nComment=tiling wm\nExec=sway\nType=Application\n", "sway")
	if !ok || s.Name != "Sway" || !reflect.DeepEqual(s.Exec, []string{"sway"}) {
		t.Errorf("basic: %+v %v", s, ok)
	}
	s, _ = parseDesktop("[Desktop Entry]\nName=Plasma\nExec=startplasma-wayland --foo %U\n", "plasma")
	if !reflect.DeepEqual(s.Exec, []string{"startplasma-wayland", "--foo"}) {
		t.Errorf("field codes: %v", s.Exec)
	}
	for _, hidden := range []string{"Hidden=true", "NoDisplay=true"} {
		if _, ok := parseDesktop("[Desktop Entry]\nName=X\nExec=x\n"+hidden+"\n", "x"); ok {
			t.Errorf("%s kept", hidden)
		}
	}
	if _, ok := parseDesktop("[Desktop Entry]\nName=X\n", "x"); ok {
		t.Error("no Exec kept")
	}
	if _, ok := parseDesktop("[Desktop Entry]\nName=X\nExec=%u\n", "x"); ok {
		t.Error("an Exec of only field codes kept")
	}
	if s, _ := parseDesktop("[Desktop Entry]\nExec=hyprland\n", "hyprland"); s.Name != "hyprland" {
		t.Errorf("name default = %q", s.Name)
	}
	s, _ = parseDesktop("[Desktop Entry]\nName=Sway\nExec=sway\n[Desktop Action new]\nExec=sway -c other\n", "sway")
	if !reflect.DeepEqual(s.Exec, []string{"sway"}) {
		t.Errorf("action group leaked: %v", s.Exec)
	}
	x := applyKind(Session{ID: "plasma-x11", Name: "Plasma", Exec: []string{"startplasma-x11"}}, X11)
	if x.Name != "Plasma (X11)" || !reflect.DeepEqual(x.Exec, []string{"startx", "/usr/bin/env", "startplasma-x11"}) {
		t.Errorf("x11 kind: %+v", x)
	}
}

func TestDiscoverPrecedenceAndSort(t *testing.T) {
	hi, lo := t.TempDir(), t.TempDir()
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(hi, "sway.desktop", "[Desktop Entry]\nName=Sway (local)\nExec=/usr/local/bin/sway\n")
	write(lo, "sway.desktop", "[Desktop Entry]\nName=Sway\nExec=sway\n")
	write(lo, "niri.desktop", "[Desktop Entry]\nName=niri\nExec=niri --session\n")
	write(lo, "README", "not a session")
	got := Discover([]string{hi, lo, "/nonexistent"}, Wayland)
	got = append(got, Discover([]string{lo}, X11)...)
	SortSessions(got)
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID+"="+s.Name)
	}
	want := []string{"niri=niri", "niri-x11=niri (X11)", "sway=Sway (local)", "sway-x11=Sway (X11)"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("sessions = %v, want %v", ids, want)
	}
}

const passwd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
alice:x:1000:1000:Alice Wonder,,,:/home/alice:/bin/zsh
bob:x:1001:1001::/home/bob:/bin/bash
svc:x:998:998:service:/var/lib/svc:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
carol:x:1002:1002:Carol:/home/carol:/bin/false
garbage
`

func TestParsePasswd(t *testing.T) {
	users := parsePasswd(passwd)
	if len(users) != 2 || users[0].Name != "alice" || users[1].Name != "bob" {
		t.Fatalf("users = %+v, want alice and bob", users)
	}
	if users[0].DisplayName != "Alice Wonder" || users[1].DisplayName != "bob" || users[0].Home != "/home/alice" {
		t.Errorf("fields: %+v", users)
	}
	if len(parsePasswd("garbage\nno:fields\n")) != 0 {
		t.Error("malformed lines kept")
	}
}

func TestFindAvatar(t *testing.T) {
	home := t.TempDir()
	if findAvatar("wayle-no-such-user", home) != "" {
		t.Error("an avatar found where none exists")
	}
	face := filepath.Join(home, ".face")
	_ = os.WriteFile(face, []byte("x"), 0o600)
	if got := findAvatar("wayle-no-such-user", home); got != face {
		t.Errorf("~/.face = %q", got)
	}
}

func TestCursorParsers(t *testing.T) {
	c, sources := parseHyprland("monitor=,preferred,auto,1\nenv = XCURSOR_THEME,Bibata-Modern-Ice # my cursor\nenv=XCURSOR_SIZE,20\nsource = ~/.config/hypr/extra.conf\n")
	if c != (Cursor{"Bibata-Modern-Ice", 20}) || !reflect.DeepEqual(sources, []string{"~/.config/hypr/extra.conf"}) {
		t.Errorf("hyprland env: %+v %v", c, sources)
	}
	if c, _ := parseHyprland("exec-once = hyprctl setcursor Adwaita 32\n"); c != (Cursor{"Adwaita", 32}) {
		t.Errorf("hyprctl setcursor: %+v", c)
	}
	if c := parseNiri("cursor {\n    xcursor-theme \"Bibata\" // comment\n    xcursor-size 28\n}\n"); c != (Cursor{"Bibata", 28}) {
		t.Errorf("niri: %+v", c)
	}
	if c := parseSway("seat * xcursor_theme \"Adwaita\" 48\n"); c != (Cursor{"Adwaita", 48}) {
		t.Errorf("sway: %+v", c)
	}
	if c := parseGTKSettings("[Settings]\ngtk-cursor-theme-name = Vimix\ngtk-cursor-theme-size=22\n"); c != (Cursor{"Vimix", 22}) {
		t.Errorf("gtk: %+v", c)
	}
	if c := parseIndexTheme("[Icon Theme]\nInherits=phinger-cursors-light, other\n"); c != (Cursor{Theme: "phinger-cursors-light"}) {
		t.Errorf("index.theme: %+v", c)
	}
	if c := parseRecorded("theme=Bibata-Modern-Ice\nsize=32\n"); c != (Cursor{"Bibata-Modern-Ice", 32}) {
		t.Errorf("record: %+v", c)
	}
	if c := parseRecorded(""); c != (Cursor{}) {
		t.Errorf("blank record: %+v", c)
	}
	if got := (Cursor{Theme: "A"}).or(Cursor{"B", 24}); got != (Cursor{"A", 24}) {
		t.Errorf("or: %+v", got)
	}
}

// TestDetectCursorOrder: the record wins; else the remembered
// session's compositor is consulted first.
func TestDetectCursorOrder(t *testing.T) {
	home := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(home, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".config/hypr/hyprland.conf", "env = XCURSOR_THEME,HyprTheme\nenv = XCURSOR_SIZE,30\nsource = extra.conf\n")
	write(".config/hypr/extra.conf", "")
	write(".config/niri/config.kdl", "xcursor-theme \"NiriTheme\"\nxcursor-size 40\n")
	if got := DetectCursor(home, "hyprland"); got != (Cursor{"HyprTheme", 30}) {
		t.Errorf("hyprland session: %+v", got)
	}
	if got := DetectCursor(home, "niri"); got != (Cursor{"NiriTheme", 40}) {
		t.Errorf("niri session: %+v", got)
	}
	write(recordedRel, "theme=Recorded\nsize=36\n")
	if got := DetectCursor(home, "niri"); got != (Cursor{"Recorded", 36}) {
		t.Errorf("record must win: %+v", got)
	}
	if got := DetectCursor(filepath.Join(home, "absent"), ""); got != (Cursor{}) {
		t.Errorf("unreadable home: %+v", got)
	}
}

// TestResolveCursor pins install_cursor's order: explicit config,
// detection, environment, the size default.
func TestResolveCursor(t *testing.T) {
	t.Setenv("XCURSOR_THEME", "EnvTheme")
	t.Setenv("XCURSOR_SIZE", "18")
	g := config.DefaultsGreeter()
	if got := ResolveCursor(g, Cursor{Theme: "Detected"}); got != (Cursor{"Detected", 18}) {
		t.Errorf("defaults are not explicit: %+v", got)
	}
	g.CursorTheme, g.CursorThemeExplicit = "Configured", true
	g.CursorSize, g.CursorSizeExplicit = 48, true
	if got := ResolveCursor(g, Cursor{"Detected", 30}); got != (Cursor{"Configured", 48}) {
		t.Errorf("explicit config wins: %+v", got)
	}
	t.Setenv("XCURSOR_THEME", "")
	t.Setenv("XCURSOR_SIZE", "")
	if got := ResolveCursor(config.DefaultsGreeter(), Cursor{}); got != (Cursor{Size: 24}) {
		t.Errorf("nothing anywhere: %+v, want the 24px size default", got)
	}
}

func TestRecordCursor(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XCURSOR_THEME", "")
	t.Setenv("XCURSOR_SIZE", "")
	t.Setenv("HYPRCURSOR_SIZE", "")
	if err := RecordCursor(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "wayle", recordFile)); err == nil {
		t.Error("a blank environment wrote a record (it would clobber a good one)")
	}
	t.Setenv("XCURSOR_THEME", "Bibata")
	t.Setenv("HYPRCURSOR_SIZE", "28")
	if err := RecordCursor(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(state, "wayle", recordFile))
	if string(data) != "theme=Bibata\nsize=28\n" {
		t.Errorf("record = %q", data)
	}
	if got := parseRecorded(string(data)); got != (Cursor{"Bibata", 28}) {
		t.Errorf("the greeter does not read back what the shell wrote: %+v", got)
	}
}

func readRuntime(t *testing.T, path string) map[string]any {
	t.Helper()
	var out map[string]any
	if _, err := toml.DecodeFile(path, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func directRead(read func() ([]byte, error)) ([]byte, error) { return read() }

func TestApplyConfigAllowlist(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.toml")
	_ = os.WriteFile(staged, []byte("[greeter]\nbackground-color = \"#123456\"\nevil = \"x\"\n[general]\nfoo = 1\n"), 0o600)
	cfg := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(filepath.Join(dir, "runtime.toml"), []byte("[styling]\ntheme = \"kept\"\n"), 0o600)
	dest, err := applyConfig(staged, cfg, directRead)
	if err != nil {
		t.Fatal(err)
	}
	out := readRuntime(t, dest)
	g := out["greeter"].(map[string]any)
	if g["background-color"] != "#123456" || g["evil"] != nil || out["general"] != nil {
		t.Errorf("runtime.toml = %v", out)
	}
	if out["styling"].(map[string]any)["theme"] != "kept" {
		t.Error("other runtime.toml tables must survive")
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("apply-config touched config.toml")
	}

	_ = os.WriteFile(staged, []byte("[general]\nfoo = 1\n"), 0o600)
	if _, err := applyConfig(staged, cfg, directRead); err == nil {
		t.Error("a file without greeter keys applied")
	}
	_ = os.WriteFile(staged, []byte("show-clock = false\n"), 0o600)
	dest, err = applyConfig(staged, cfg, directRead)
	if err != nil || readRuntime(t, dest)["greeter"].(map[string]any)["show-clock"] != false {
		t.Errorf("bare top-level keys: %v", err)
	}
}

// TestApplyConfigCopiesImage: image mode copies the background next to
// the config through the caller-rights read; color mode leaves a home
// path alone and reads nothing.
func TestApplyConfigCopiesImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "wall.png")
	_ = os.WriteFile(src, []byte("PNGDATA"), 0o600)
	staged := filepath.Join(dir, "staged.toml")
	_ = os.WriteFile(staged, []byte("[greeter]\nbackground-mode = \"image\"\nbackground-image = \""+src+"\"\n"), 0o600)
	cfg := filepath.Join(dir, "config.toml")
	reads := 0
	dest, err := applyConfig(staged, cfg, func(read func() ([]byte, error)) ([]byte, error) {
		reads++
		return read()
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "greeter-background.png")
	if got := readRuntime(t, dest)["greeter"].(map[string]any)["background-image"]; got != want {
		t.Errorf("background-image = %v, want %s", got, want)
	}
	if data, _ := os.ReadFile(want); string(data) != "PNGDATA" || reads != 1 {
		t.Errorf("copied %q with %d caller-rights reads", data, reads)
	}
	if fi, _ := os.Stat(want); fi.Mode().Perm() != 0o644 {
		t.Errorf("copy mode = %v, want world-readable", fi.Mode().Perm())
	}

	_ = os.WriteFile(staged, []byte("[greeter]\nbackground-mode = \"color\"\nbackground-image = \"/home/nobody/x.png\"\n"), 0o600)
	reads = 0
	dest, err = applyConfig(staged, cfg, func(read func() ([]byte, error)) ([]byte, error) { reads++; return read() })
	if err != nil || reads != 0 {
		t.Fatalf("color mode: err %v, reads %d", err, reads)
	}
	if got := readRuntime(t, dest)["greeter"].(map[string]any)["background-image"]; got != "/home/nobody/x.png" {
		t.Errorf("color mode rewrote the path: %v", got)
	}

	_ = os.WriteFile(staged, []byte("[greeter]\nbackground-mode = \"image\"\nbackground-image = \"/root/secret\"\n"), 0o600)
	if _, err := applyConfig(staged, cfg, func(func() ([]byte, error)) ([]byte, error) {
		return nil, os.ErrPermission
	}); err == nil {
		t.Error("an image the caller cannot read was copied")
	}
}

func TestRunApplyConfigUsage(t *testing.T) {
	var out, errOut strings.Builder
	if code := RunApplyConfig(nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "usage") {
		t.Errorf("no staged file: %d %q", code, errOut.String())
	}
	errOut.Reset()
	if code := RunApplyConfig([]string{"a", "b"}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unexpected extra argument") {
		t.Errorf("two staged files: %d %q", code, errOut.String())
	}
	dir := t.TempDir()
	staged := filepath.Join(dir, "s.toml")
	_ = os.WriteFile(staged, []byte("show-clock = true\n"), 0o600)
	out.Reset()
	if code := RunApplyConfig([]string{"--config", filepath.Join(dir, "config.toml"), staged}, &out, &errOut); code != 0 ||
		out.String() != "wrote "+filepath.Join(dir, "runtime.toml")+"\n" {
		t.Errorf("apply: %d %q", code, out.String())
	}
}
