package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/powermenu"
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

type powerMenuWindow struct{}

func (powerMenuWindow) Close() {}

// TestPowerMenuVerbOpensTheMenu pins show_power_menu: :menu opens the
// native power menu, and with none it does nothing (no dropdown).
func TestPowerMenuVerbOpensTheMenu(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	opened := 0
	ctx.PowerMenu = powermenu.New(powermenu.Deps{
		Config: func() *config.Config { return cfg },
		Open: func(app.LayerConfig) (powermenu.Window, error) {
			opened++
			return powerMenuWindow{}, nil
		},
		Run:  func(string) error { return nil },
		Font: ctx.Font,
	})
	mod, err := Create("power", ctx)
	if err != nil {
		t.Fatal(err)
	}
	menu := config.ClickAction{Kind: config.ClickShell, Command: ":menu"}
	mod.(interface{ RunAction(config.ClickAction) }).RunAction(menu)
	if opened != 1 || !ctx.PowerMenu.Open() {
		t.Errorf(":menu opened %d menus", opened)
	}
	ctx.PowerMenu = nil
	mod, _ = Create("power", ctx)
	mod.(interface{ RunAction(config.ClickAction) }).RunAction(menu) // no menu: nothing
}
