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
	style := computeStyle(cfg, styling.Default()).forModules()
	m := &networkModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: &fakeNetworkSource{snap: snap, ticks: make(chan struct{}, 2)},
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	return m
}

// compute_link_display: the primary connection's device decides, a
// missing device says so, no primary connection is offline, and other
// primaries prefer wifi to wired.
func TestLinkDisplayFollowsThePrimaryConnection(t *testing.T) {
	cfg := config.Defaults().Network
	wifiUp := network.Snapshot{WifiEnabled: true, HasWifi: true, Wifi: network.StatusConnected, WifiSSID: "home", WifiStrength: 90}
	wiredUp := network.Snapshot{HasWired: true, Wired: network.StatusConnected}
	both := wifiUp
	both.HasWired, both.Wired = true, network.StatusConnected
	strongest := cfg.WifiSignalIcons[len(cfg.WifiSignalIcons)-1]
	for _, tc := range []struct {
		name        string
		primary     network.ConnectionType
		snap        network.Snapshot
		icon, label string
	}{
		{"wifi primary", network.ConnectionWifi, both, strongest, "home"},
		{"wired primary over a live wifi", network.ConnectionWired, both, cfg.WiredConnectedIcon, i18n.T("bar-network-wired")},
		{"wifi primary without a card", network.ConnectionWifi, wiredUp, cfg.WifiOfflineIcon, i18n.T("bar-network-no-wifi")},
		{"wired primary without a port", network.ConnectionWired, wifiUp, cfg.WiredDisconnectedIcon, i18n.T("bar-network-no-ethernet")},
		{"no primary", network.ConnectionNone, both, cfg.WifiOfflineIcon, i18n.T("bar-network-offline")},
		{"vpn primary prefers wifi", network.ConnectionVPN, both, strongest, "home"},
		{"vpn primary on wired only", network.ConnectionVPN, wiredUp, cfg.WiredConnectedIcon, i18n.T("bar-network-wired")},
		{"other primary, no devices", network.ConnectionOther, network.Snapshot{}, cfg.WifiOfflineIcon, i18n.T("bar-network-offline")},
	} {
		tc.snap.Primary = tc.primary
		icon, label := linkDisplay(cfg, tc.snap)
		if icon != tc.icon || label != tc.label {
			t.Errorf("%s: %q %q, want %q %q", tc.name, icon, label, tc.icon, tc.label)
		}
	}
}

// wifi_icon and wifi_label over the radio and device states; a
// disconnected card reads disconnected, never connecting.
func TestWifiStates(t *testing.T) {
	cfg := config.Defaults().Network
	for _, tc := range []struct {
		name        string
		snap        network.Snapshot
		icon, label string
	}{
		{"radio off", network.Snapshot{HasWifi: true}, cfg.WifiDisabledIcon, i18n.T("bar-network-disconnected")},
		{"disconnected", network.Snapshot{WifiEnabled: true, HasWifi: true}, cfg.WifiOfflineIcon, i18n.T("bar-network-disconnected")},
		{"connecting", network.Snapshot{WifiEnabled: true, HasWifi: true, Wifi: network.StatusConnecting}, cfg.WifiAcquiringIcon, i18n.T("bar-network-connecting")},
		{"hidden ssid", network.Snapshot{WifiEnabled: true, HasWifi: true, Wifi: network.StatusConnected}, cfg.WifiSignalIcons[0], i18n.T("bar-network-wifi-fallback")},
	} {
		tc.snap.Primary = network.ConnectionWifi
		icon, label := linkDisplay(cfg, tc.snap)
		if icon != tc.icon || label != tc.label {
			t.Errorf("%s: %q %q, want %q %q", tc.name, icon, label, tc.icon, tc.label)
		}
	}
	cfg.WifiSignalIcons = nil
	if got := wifiIcon(cfg, network.Snapshot{WifiEnabled: true, Wifi: network.StatusConnected}); got != cfg.WifiConnectedIcon {
		t.Errorf("no signal icons: %q, want the connected icon", got)
	}
}

// The VPN icon takes the icon over per vpn-show (auto waits for a
// profile); the label stays the link's.
func TestVPNOverlaysTheIcon(t *testing.T) {
	cfg := config.Defaults().Network
	snap := network.Snapshot{Primary: network.ConnectionWired, HasWired: true, Wired: network.StatusConnected}
	for _, tc := range []struct {
		show config.VpnShow
		vpn  vpnView
		icon string
	}{
		{config.VpnShowAuto, vpnView{}, cfg.WiredConnectedIcon},
		{config.VpnShowAuto, vpnView{configured: true, state: network.VPNConnected}, cfg.VpnConnectedIcon},
		{config.VpnShowAuto, vpnView{configured: true, state: network.VPNConnecting}, cfg.VpnConnectingIcon},
		{config.VpnShowAuto, vpnView{configured: true, state: network.VPNFailed}, cfg.VpnDisconnectedIcon},
		{config.VpnShowAlways, vpnView{}, cfg.VpnDisconnectedIcon},
		{config.VpnShowNever, vpnView{configured: true, state: network.VPNConnected}, cfg.WiredConnectedIcon},
	} {
		cfg.VpnShow = tc.show
		icon, label := networkDisplay(cfg, snap, tc.vpn)
		if icon != tc.icon || label != i18n.T("bar-network-wired") {
			t.Errorf("%s %+v: %q %q, want %q and the wired label", tc.show, tc.vpn, icon, label, tc.icon)
		}
	}
}

// The label takes the stylesheet's ink (label-color), connected or
// not, and label-show off empties it.
func TestNetworkModuleLabel(t *testing.T) {
	cfg := config.Defaults()
	offline := newTestNetworkModule(t, cfg, network.Snapshot{})
	if err := offline.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := offline.label.Text(); got != i18n.T("bar-network-offline") || offline.label.Color() != 0 {
		t.Errorf("offline label %q ink %#08x, want the offline text in the stylesheet's ink", got, offline.label.Color())
	}
	cfg.Network.LabelShow = false
	hidden := newTestNetworkModule(t, cfg, network.Snapshot{Primary: network.ConnectionWifi, WifiEnabled: true, HasWifi: true, Wifi: network.StatusConnected, WifiSSID: "x"})
	if err := hidden.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := hidden.label.Text(); got != "" {
		t.Errorf("label-show off: %q", got)
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
