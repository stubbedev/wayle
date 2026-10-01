package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sysinfo"
)

func TestNetstatLabelPlaceholders(t *testing.T) {
	rate := sysinfo.Rate{
		Interface: "wlan0",
		RxPerSec:  1536,  // 1.5 KB, 2 KiB, 1.5 MiB/1024...
		TxPerSec:  10240, // 10.0 KB, 10 KiB
	}
	cfg := config.DefaultsNetstat()
	if got := netstatLabel(cfg.Format, rate); got != "1.5 KB 10.2 KB" {
		t.Errorf("default = %q", got)
	}
	got := netstatLabel("{{ down_kib }}/{{ down_mib }}/{{ down_gib }} {{ interface }}", rate)
	// KiB rounds down like {:.0}; MiB keeps one decimal.
	if got != "1/0.0/0.00 wlan0" {
		t.Errorf("kib/mib/gib = %q", got)
	}
	got = netstatLabel("{{ up_kib }} {{ up_mib }} {{ up_gib }}", rate)
	if got != "10 0.0 0.00" {
		t.Errorf("up = %q", got)
	}
	// Zero traffic renders the auto units as plain bytes.
	if got := netstatLabel("{{ down_auto }}", sysinfo.Rate{}); got != "0 B" {
		t.Errorf("zero = %q", got)
	}
}

func TestLoadFileAppliesNetstat(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	good := "[modules.netstat]\ninterface = \"eth0\"\nformat = \"{{ down_mib }} down\"\npoll-interval-ms = 5000\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Netstat.Interface != "eth0" || c.Netstat.PollIntervalMs != 5000 || c.Netstat.Format != "{{ down_mib }} down" {
		t.Errorf("netstat = %+v", c.Netstat)
	}

	if err := osWrite(path, "[modules.netstat]\npoll-interval-ms = -1\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Error("negative interval: want a load error")
	}
}
