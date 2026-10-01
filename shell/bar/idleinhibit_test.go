package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/idleinhibit"
)

func TestIdleInhibitLabel(t *testing.T) {
	// Off: dashes for remaining, the stored duration still shows.
	if got := idleInhibitLabel("{{ state }} {{ remaining }} {{ duration }}", false, 60, 0); got != i18n.T("bar-idle-inhibit-off")+" - 60" {
		t.Errorf("= %q", got)
	}
	// On, timed: H:MM:SS above the hour, M:SS within it.
	if got := idleInhibitLabel("{{ state }} {{ remaining }}", true, 60, 3661); got != i18n.T("bar-idle-inhibit-on")+" 1:01:01" {
		t.Errorf("= %q", got)
	}
	if got := idleInhibitLabel("{{ state }} {{ remaining }}", true, 60, 95); got != i18n.T("bar-idle-inhibit-on")+" 1:35" {
		t.Errorf("= %q", got)
	}
	// On, indefinite: the infinity sign for both.
	if got := idleInhibitLabel("{{ state }} {{ remaining }} {{ duration }}", true, 0, 0); got != i18n.T("bar-idle-inhibit-on")+" ∞ ∞" {
		t.Errorf("= %q", got)
	}
}

func TestLoadFileAppliesIdleInhibit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.idle-inhibit]\nformat = \"[{{ state }}]\"\nstartup-duration = 30\nicon-active = \"tb-eye-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.IdleInhibit.Format != "[{{ state }}]" || c.IdleInhibit.StartupDuration != 30 {
		t.Errorf("config = %+v", c.IdleInhibit)
	}
	if got := c.IdleInhibit.Icons()[config.IdleInhibitActive].Name; got != "tb-eye-symbolic" {
		t.Errorf("icon-active = %q", got)
	}
	if got := c.IdleInhibit.Icons()[config.IdleInhibitInactive].Name; got != "tb-coffee-off-symbolic" {
		t.Errorf("icon-inactive default changed: %q", got)
	}

	if err := osWrite(path, "[modules.idle-inhibit]\nformat = \"\"\n"); err != nil {
		t.Fatal(err)
	}
	// The schema puts no constraint on the format string.
	if _, err := config.LoadFile(path); err != nil {
		t.Errorf("empty format: accepted by the schema, got %v", err)
	}
}

func TestIdleInhibitModuleFollowsState(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	state := idleinhibit.NewState(60)
	ctx.IdleInhibit = state

	module, err := Create("idle-inhibit", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label := findLabel(module.Root())
	if label.Text() != i18n.T("bar-idle-inhibit-off") {
		t.Errorf("initial label = %q, want Off", label.Text())
	}

	// Enabling the shared state flips the label (the module's follower
	// goroutine consumes the change ticks; poll the label).
	state.Enable(true)
	waitForText(t, label, i18n.T("bar-idle-inhibit-on"))
	state.Disable()
	waitForText(t, label, i18n.T("bar-idle-inhibit-off"))

	// A module built without the shared state still works standalone.
	standalone, err := Create("idle-inhibit", newTestContext(t, cfg))
	if err != nil {
		t.Fatalf("standalone Create: %v", err)
	}
	if _, ok := standalone.(*idleInhibitModule); !ok {
		t.Errorf("standalone = %T", standalone)
	}
	if label := findLabel(standalone.Root()); label.Text() != i18n.T("bar-idle-inhibit-off") {
		t.Errorf("standalone label = %q", label.Text())
	}
}
