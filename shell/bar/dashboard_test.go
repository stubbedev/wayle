package bar

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestParseOSRelease(t *testing.T) {
	info, ok := parseOSRelease("NAME=\"Arch Linux\"\nID=arch\nLOGO='archlinux-logo'\n")
	if !ok || info.id != "arch" || info.logo != "archlinux-logo" {
		t.Errorf("= %+v, %v; want arch with the unquoted logo", info, ok)
	}
	if info, ok := parseOSRelease("ID=\"nixos\"\n"); !ok || info.id != "nixos" || info.logo != "" {
		t.Errorf("quoted id = %+v, %v", info, ok)
	}
	// No ID line: no distro (detect_distro's id?).
	if _, ok := parseOSRelease("NAME=Something\nLOGO=x\n"); ok {
		t.Error("an os-release without ID must not detect a distro")
	}
}

func TestDashboardIconOrder(t *testing.T) {
	distro := func(id, logo string) func() (distroInfo, bool) {
		return func() (distroInfo, bool) { return distroInfo{id: id, logo: logo}, true }
	}
	none := func() (distroInfo, bool) { return distroInfo{}, false }
	have := func(names ...string) func(string) bool {
		return func(n string) bool { return slices.Contains(names, n) }
	}
	for _, tc := range []struct {
		name     string
		override string
		distro   func() (distroInfo, bool)
		exists   func(string) bool
		want     string
	}{
		{"override wins", "custom-icon", distro("arch", ""), have(), "custom-icon"},
		{"no os-release", "", none, have("si-archlinux-symbolic"), dashboardFallbackIcon},
		{"bundled icon", "", distro("arch", "arch-logo"), have("si-archlinux-symbolic", "arch-logo"), "si-archlinux-symbolic"},
		{"opensuse variants share one", "", distro("opensuse-leap", ""), have("si-opensuse-symbolic"), "si-opensuse-symbolic"},
		{"bundled missing falls to LOGO", "", distro("arch", "arch-logo"), have("arch-logo"), "arch-logo"},
		{"then distributor-logo", "", distro("weird", "nope"), have("distributor-logo-weird"), "distributor-logo-weird"},
		{"nothing resolves", "", distro("weird", "nope"), have(), dashboardFallbackIcon},
	} {
		if got := dashboardIcon(tc.override, tc.distro, tc.exists); got != tc.want {
			t.Errorf("%s: icon = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDashboardModuleIsTheIcon(t *testing.T) {
	cfg := config.Defaults()
	cfg.Dashboard.IconOverride = "my-distro-symbolic"
	ctx := newTestContext(t, cfg)
	module, err := Create("dashboard", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	icon, ok := module.Root().(*widget.Icon)
	if !ok || icon.Name() != "my-distro-symbolic" {
		t.Fatalf("root = %T, want the override icon", module.Root())
	}
	// The default binding opens the dashboard dropdown.
	if got := moduleBinding("dashboard", cfg).LeftClick.String(); got != "dropdown:dashboard" {
		t.Errorf("left-click = %q, want dropdown:dashboard", got)
	}
}

func TestLoadFileAppliesDashboard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.dashboard]\nicon-override = \"x\"\ndropdown-lock-command = \"swaylock\"\nusage-error = 90\n" +
		"right-click = \"dropdown:calendar\"\n[modules.dashboard.user-session]\nactions = [\"reboot\", \"lock\"]\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	d := c.Dashboard
	if d.IconOverride != "x" || d.LockCommand != "swaylock" || d.UsageError != 90 {
		t.Errorf("dashboard = %+v", d)
	}
	if d.LogoutCommand != "loginctl terminate-session $XDG_SESSION_ID" || d.UsageWarning != 60 {
		t.Errorf("unset keys lost their defaults: %+v", d)
	}
	if len(d.SessionActions) != 2 || d.SessionActions[0] != config.SessionReboot || d.SessionActions[1] != config.SessionLock {
		t.Errorf("actions = %v, want [reboot lock]", d.SessionActions)
	}
	if d.Click.RightClick.String() != "dropdown:calendar" || d.Click.LeftClick.String() != "dropdown:dashboard" {
		t.Errorf("clicks = %+v", d.Click)
	}

	// The defaults carry all four actions in schema order.
	def := config.DefaultsDashboard().SessionActions
	if len(def) != 4 || def[0] != config.SessionLock || def[3] != config.SessionPowerOff {
		t.Errorf("default actions = %v", def)
	}

	for _, bad := range []string{
		"[modules.dashboard.user-session]\nactions = [\"logout\"]\n",
		"[modules.dashboard]\nusage-warning = \"high\"\n",
		"[modules.dashboard]\nleft-click = 3\n",
	} {
		if err := osWrite(path, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}

func TestSessionButtonsFollowTheActions(t *testing.T) {
	cfg := config.DefaultsDashboard()
	cfg.RebootCommand = "my-reboot"
	b := sessionButtonFor(config.SessionReboot, cfg)
	if b.icon != "ld-refresh-cw-symbolic" || b.command != "my-reboot" {
		t.Errorf("reboot button = %+v", b)
	}
	if b := sessionButtonFor(config.SessionLogout, cfg); b.icon != "ld-log-out-symbolic" || b.command != cfg.LogoutCommand {
		t.Errorf("log-out button = %+v", b)
	}
}

func TestUserSessionSectionRendersConfiguredActions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "alice")
	cfg := config.Defaults()
	cfg.Dashboard.SessionActions = []config.SessionAction{config.SessionLock, config.SessionPowerOff}
	ctx := newTestContext(t, cfg)
	row, ok := userSessionSection(ctx).(*widget.Box)
	if !ok {
		t.Fatal("section is not a row")
	}
	kids := row.Children()
	if len(kids) != 3 {
		t.Fatalf("row children = %d, want avatar, name, actions", len(kids))
	}
	// No ~/.face: the user glyph stands in.
	if glyph, ok := kids[0].(*widget.Icon); !ok || glyph.Name() != "ld-user-symbolic" {
		t.Errorf("avatar = %T, want the ld-user-symbolic glyph", kids[0])
	}
	if name := findLabel(kids[1]); name == nil || name.Text() != "alice" {
		t.Errorf("username label wrong")
	}
	actions, ok := kids[2].(*widget.Box)
	if !ok || len(actions.Children()) != 2 {
		t.Fatalf("actions = %T, want two buttons", kids[2])
	}
}

func TestUserSessionUsesTheFaceImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(0, 0, color.NRGBA{A: 255})
	f, err := os.Create(filepath.Join(home, ".face"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	face := loadAvatar(facePath())
	if face == nil {
		t.Fatal("a decodable ~/.face did not load")
	}
	if w, h := face.Size(); w != avatarPx || h != avatarPx {
		t.Errorf("avatar = %dx%d, want the %dpx square", w, h, avatarPx)
	}
	// Garbage is no avatar, not a crash.
	if err := os.WriteFile(filepath.Join(home, ".face"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loadAvatar(facePath()) != nil {
		t.Error("an undecodable ~/.face produced an avatar")
	}
}
