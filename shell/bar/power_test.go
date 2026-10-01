package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestLoadFileAppliesPower(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.power]\nlock-command = \"swaylock\"\nshutdown-command = \"\"\nreboot-command = \"\"\nlogout-command = \"loginctl kill-session\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Power.LockCommand != "swaylock" || c.Power.ShutdownCommand != "" || c.Power.LogoutCommand != "loginctl kill-session" {
		t.Errorf("power = %+v", c.Power)
	}
	if c.Power.Icon().Name != "ld-power-symbolic" || !c.Power.Icon().Show {
		t.Errorf("icon = %+v", c.Power.Icon())
	}
	// The schema default left-click is the native :menu.
	if c.Power.Clicks().LeftClick.String() != ":menu" {
		t.Errorf("left-click = %q", c.Power.Clicks().LeftClick.String())
	}
}

func TestNewPowerRequiresAnIcon(t *testing.T) {
	cfg := config.Defaults()
	cfg.Power.IconName = ""
	ctx := newTestContext(t, cfg)
	if _, err := Create("power", ctx); err == nil {
		t.Fatal("icon-show off: want an error, got a module")
	}
}
