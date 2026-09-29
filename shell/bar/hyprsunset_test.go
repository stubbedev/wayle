package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestHyprsunsetLabel(t *testing.T) {
	// Running: the live temp and gamma.
	got := hyprsunsetLabel("{{ status }} {{ temp }}K {{ gamma }}%", true, 4500, 90, 5000, 100)
	if got != "On 4500K 90%" {
		t.Errorf("= %q, want On 4500K 90%%", got)
	}
	// Off: the status word with -- placeholders.
	got = hyprsunsetLabel("{{ status }} {{ temp }} {{ gamma }}", false, 4500, 90, 5000, 100)
	if got != "Off -- --" {
		t.Errorf("= %q, want Off -- --", got)
	}
	// The configured values land on their own placeholders.
	got = hyprsunsetLabel("set {{ config_temp }}/{{ config_gamma }}", false, 0, 0, 3500, 80)
	if got != "set 3500/80" {
		t.Errorf("= %q", got)
	}
}

func TestLoadFileAppliesHyprsunset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.hyprsunset]\ntemperature = 3500\ngamma = 80\nformat = \"{{ status }}\"\nicon-on = \"ld-moon-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Hyprsunset.Temperature != 3500 || c.Hyprsunset.Gamma != 80 {
		t.Errorf("config = %+v", c.Hyprsunset)
	}
	if c.Hyprsunset.IconOn.Name != "ld-moon-symbolic" {
		t.Errorf("icon-on = %q", c.Hyprsunset.IconOn.Name)
	}
	if c.Hyprsunset.Click.LeftClick.String() != ":toggle" {
		t.Errorf("left-click = %q", c.Hyprsunset.Click.LeftClick.String())
	}

	for _, bad := range []string{
		"[modules.hyprsunset]\ntemperature = 500\n",
		"[modules.hyprsunset]\ntemperature = 20000\n",
		"[modules.hyprsunset]\ngamma = 300\n",
		"[modules.hyprsunset]\nformat = \"\"\n",
	} {
		if err := osWrite(path, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}

func TestHyprsunsetToggleTransitions(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	module, err := Create("hyprsunset", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	hs, ok := module.(*hyprsunsetModule)
	if !ok {
		t.Fatalf("Create = %T", module)
	}
	// Off at rest, with the off icon.
	if hs.enabled {
		t.Error("module starts enabled")
	}
	// The toggle handler recognizes :toggle and ignores other actions
	// (starting hyprsunset in tests would spawn the real binary, so the
	// enabled path is exercised by the state machine instead).
	hs.RunAction(config.MustClickAction("dropdown:hyprsunset"))
	if hs.enabled {
		t.Error("a dropdown action toggled the module")
	}
}
