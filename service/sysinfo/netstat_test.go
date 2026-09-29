package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadNetTotals(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("wlan0/statistics/rx_bytes", "1000\n")
	write("wlan0/statistics/tx_bytes", "2000\n")
	write("lo/statistics/rx_bytes", "1\n")
	write("lo/statistics/tx_bytes", "1\n")
	write("broken/statistics/rx_bytes", "not-a-number\n")

	old := SysClassNet
	SysClassNet = dir
	t.Cleanup(func() { SysClassNet = old })

	totals, err := ReadNetTotals()
	if err != nil {
		t.Fatalf("ReadNetTotals: %v", err)
	}
	if totals["wlan0"] != (NetTotals{Rx: 1000, Tx: 2000}) {
		t.Errorf("wlan0 = %+v", totals["wlan0"])
	}
	// A bad counter skips the interface instead of failing the read.
	if _, ok := totals["broken"]; ok {
		t.Error("broken interface matched")
	}
}

func TestSelectInterface(t *testing.T) {
	totals := map[string]NetTotals{
		"lo":    {Rx: 999999, Tx: 999999},
		"wlan0": {Rx: 100, Tx: 50},
		"eth0":  {Rx: 10, Tx: 20},
	}
	if got, ok := SelectInterface(totals, "auto"); !ok || got != "wlan0" {
		t.Errorf("auto = %q ok=%v, want wlan0", got, ok)
	}
	if got, ok := SelectInterface(totals, "eth0"); !ok || got != "eth0" {
		t.Errorf("exact = %q ok=%v", got, ok)
	}
	if _, ok := SelectInterface(totals, "missing"); ok {
		t.Error("unknown interface matched")
	}
	// Loopback never wins auto even at full traffic.
	onlyLo := map[string]NetTotals{"lo": {Rx: 1, Tx: 1}}
	if _, ok := SelectInterface(onlyLo, "auto"); ok {
		t.Error("loopback won auto")
	}
}

func TestNetRate(t *testing.T) {
	rate, err := NetRate("wlan0", NetTotals{Rx: 1000, Tx: 500}, NetTotals{Rx: 3000, Tx: 1500}, 2)
	if err != nil {
		t.Fatalf("NetRate: %v", err)
	}
	if rate.RxPerSec != 1000 || rate.TxPerSec != 500 {
		t.Errorf("rate = %+v, want 1000/500", rate)
	}
	// A counter reset (reboot) reads as zero traffic, not negative.
	rate, err = NetRate("wlan0", NetTotals{Rx: 5000}, NetTotals{Rx: 10}, 1)
	if err != nil {
		t.Fatalf("reset NetRate: %v", err)
	}
	if rate.RxPerSec != 0 {
		t.Errorf("reset rate = %d, want 0", rate.RxPerSec)
	}
	// No elapsed time is an error, not a divide by zero.
	if _, err := NetRate("wlan0", NetTotals{}, NetTotals{}, 0); err == nil {
		t.Error("zero elapsed: want an error")
	}
}

func TestAutoBytes(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{
		{999, "999 B"},
		{1000, "1.0 KB"},
		{1536, "1.5 KB"},
		{1500 * 1000, "1.5 MB"},
		{2 * 1000 * 1000 * 1000, "2.0 GB"},
	} {
		if got := AutoBytes(tc.bytes); got != tc.want {
			t.Errorf("AutoBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestReadNetDevTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "net/dev")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "Inter-|   Receive\n face |bytes    packets\n" +
		"    lo: 100 1 0 0 0 0 0 0 10 2 0 0 0 0 0 0\n" +
		" wlan0: 2048 5 0 0 0 0 0 0 4096 6 0 0 0 0 0 0\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	totals, err := ReadNetDevTotals(path)
	if err != nil {
		t.Fatalf("ReadNetDevTotals: %v", err)
	}
	if totals["wlan0"] != (NetTotals{Rx: 2048, Tx: 4096}) {
		t.Errorf("wlan0 = %+v", totals["wlan0"])
	}
	if totals["lo"] != (NetTotals{Rx: 100, Tx: 10}) {
		t.Errorf("lo = %+v", totals["lo"])
	}
	if _, err := ReadNetDevTotals(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing file: want an error")
	}
}
