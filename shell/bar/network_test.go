package bar

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/styling"
)

func TestSignalIndexBucketsLikeRust(t *testing.T) {
	for _, tc := range []struct {
		strength uint8
		icons    int
		want     int
	}{
		// Rust: bucket = 100/icons (integer division), idx =
		// strength/bucket, clamped to icons-1. 33/(100/3=33) = 1;
		// 100/(100/3=33) = 3 → clamped to 2.
		{0, 3, 0},
		{32, 3, 0},
		{33, 3, 1},
		{100, 3, 2},
		{100, 4, 3},
		{255, 5, 4},
		{50, 0, 0},
	} {
		if got := network.SignalIndex(tc.strength, tc.icons); got != tc.want {
			t.Errorf("SignalIndex(%d, %d) = %d, want %d", tc.strength, tc.icons, got, tc.want)
		}
	}
}

// fakeNetworkSource is a scripted network.Source, safe across a follow
// goroutine and the test.
type fakeNetworkSource struct {
	mu    sync.Mutex
	snap  network.Snapshot
	ticks chan struct{}
}

func (f *fakeNetworkSource) Read(context.Context) (network.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, nil
}

func (f *fakeNetworkSource) setSnap(s network.Snapshot) {
	f.mu.Lock()
	f.snap = s
	f.mu.Unlock()
}

func (f *fakeNetworkSource) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	return f.ticks, func() {}, nil
}

func newTestNetworkModule(t *testing.T, cfg *config.Config, snap network.Snapshot) *networkModule {
	t.Helper()
	style := computeStyle(cfg, styling.Default())
	m := &networkModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: &fakeNetworkSource{snap: snap, ticks: make(chan struct{}, 2)},
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	return m
}

func TestNetworkLabelPicksWifiThenWired(t *testing.T) {
	wifi := network.Snapshot{WifiEnabled: true, WifiConnected: true, WifiSSID: "homewifi"}
	if got := networkLabel(wifi); got != "homewifi" {
		t.Errorf("wifi ssid = %q", got)
	}
	hidden := wifi
	hidden.WifiSSID = ""
	if got := networkLabel(hidden); got != i18n.T("bar-network-wifi-fallback") {
		t.Errorf("hidden ssid = %q, want the WiFi fallback", got)
	}
	connecting := network.Snapshot{WifiEnabled: true, WifiConnecting: true}
	if got := networkLabel(connecting); got != i18n.T("bar-network-connecting") {
		t.Errorf("connecting = %q", got)
	}
	wired := network.Snapshot{WifiEnabled: false, WiredConnected: true}
	if got := networkLabel(wired); got != i18n.T("bar-network-wired") {
		t.Errorf("wired = %q", got)
	}
	off := network.Snapshot{}
	if got := networkLabel(off); got != i18n.T("bar-network-disconnected") {
		t.Errorf("offline = %q", got)
	}
	// Wifi preferred over wired while enabled.
	both := network.Snapshot{WifiEnabled: true, WifiConnected: true, WifiSSID: "a", WiredConnected: true}
	if got := networkLabel(both); got != "a" {
		t.Errorf("both = %q, want the wifi ssid", got)
	}
}

func TestNetworkModuleDimsWhenOffline(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())

	offline := newTestNetworkModule(t, cfg, network.Snapshot{})
	if err := offline.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := offline.label.Text(); got != i18n.T("bar-network-disconnected") {
		t.Errorf("label = %q", got)
	}
	muted, _ := styling.Default().Token(config.TokenFgMuted)
	if offline.label.Color() != muted {
		t.Errorf("offline color = %#08x, want fg-muted", offline.label.Color())
	}

	online := newTestNetworkModule(t, cfg, network.Snapshot{WifiEnabled: true, WifiConnected: true, WifiSSID: "x"})
	if err := online.refresh(); err != nil {
		t.Fatal(err)
	}
	if online.label.Color() != style.fg {
		t.Errorf("online color = %#08x, want the default fg", online.label.Color())
	}
}

func TestNewNetworkRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Network = nil
	if _, err := Create("network", ctx); err == nil {
		t.Fatal("nil network source: want an error, got a module")
	}
}

func TestLoadFileAppliesNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.network]\nlabel-show = false\nwifi-offline-icon = \"wifi-x\"\nwired-connected-icon = \"lan\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Network.LabelShow || c.Network.WifiOfflineIcon != "wifi-x" || c.Network.WiredConnectedIcon != "lan" {
		t.Errorf("config = %+v", c.Network)
	}
}

func TestLoadFileRejectsBadNetwork(t *testing.T) {
	for _, content := range []string{} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}
