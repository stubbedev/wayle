package bar

import (
	"context"
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/network"
)

// vpnView is what the module shows of the VPNs: whether NM holds any
// profile, and their aggregate state.
type vpnView struct {
	configured bool
	state      network.VPNState
}

// networkDisplay is compute_display: the link's icon and label, the VPN
// icon taking the icon over while the VPN is shown.
func networkDisplay(cfg config.NetworkConfig, snap network.Snapshot, vpn vpnView) (icon, label string) {
	icon, label = linkDisplay(cfg, snap)
	if v, ok := vpnIcon(cfg, vpn); ok {
		icon = v
	}
	return icon, label
}

// vpnIcon is the VPN overlay icon; false when vpn-show hides it (auto
// waits for a profile).
func vpnIcon(cfg config.NetworkConfig, vpn vpnView) (string, bool) {
	if cfg.VpnShow == config.VpnShowNever || cfg.VpnShow == config.VpnShowAuto && !vpn.configured {
		return "", false
	}
	switch vpn.state {
	case network.VPNConnected:
		return cfg.VpnConnectedIcon, true
	case network.VPNConnecting:
		return cfg.VpnConnectingIcon, true
	}
	return cfg.VpnDisconnectedIcon, true
}

// linkDisplay is compute_link_display: the primary connection's device,
// "No Wi-Fi"/"No Ethernet" when it has none, offline without a primary
// connection, and otherwise wifi before wired.
func linkDisplay(cfg config.NetworkConfig, snap network.Snapshot) (icon, label string) {
	switch snap.Primary {
	case network.ConnectionWifi:
		if !snap.HasWifi {
			return cfg.WifiOfflineIcon, i18n.T("bar-network-no-wifi")
		}
		return wifiIcon(cfg, snap), wifiLabel(snap)
	case network.ConnectionWired:
		if !snap.HasWired {
			return cfg.WiredDisconnectedIcon, i18n.T("bar-network-no-ethernet")
		}
		return wiredIcon(cfg, snap.Wired), wiredLabel(snap.Wired)
	case network.ConnectionNone:
		return cfg.WifiOfflineIcon, i18n.T("bar-network-offline")
	}
	switch {
	case snap.HasWifi:
		return wifiIcon(cfg, snap), wifiLabel(snap)
	case snap.HasWired:
		return wiredIcon(cfg, snap.Wired), wiredLabel(snap.Wired)
	}
	return cfg.WifiOfflineIcon, i18n.T("bar-network-offline")
}

// wifiIcon is helpers.rs's wifi_icon: disabled, acquiring, offline, or
// the signal list bucketed over the strength.
func wifiIcon(cfg config.NetworkConfig, snap network.Snapshot) string {
	if !snap.WifiEnabled {
		return cfg.WifiDisabledIcon
	}
	switch snap.Wifi {
	case network.StatusConnecting:
		return cfg.WifiAcquiringIcon
	case network.StatusDisconnected:
		return cfg.WifiOfflineIcon
	}
	if len(cfg.WifiSignalIcons) == 0 {
		return cfg.WifiConnectedIcon
	}
	return cfg.WifiSignalIcons[network.SignalIndex(snap.WifiStrength, len(cfg.WifiSignalIcons))]
}

// wifiLabel is wifi_label: the SSID (the _bar.ftl fallback when
// hidden), connecting, or disconnected.
func wifiLabel(snap network.Snapshot) string {
	switch snap.Wifi {
	case network.StatusConnected:
		if snap.WifiSSID != "" {
			return snap.WifiSSID
		}
		return i18n.T("bar-network-wifi-fallback")
	case network.StatusConnecting:
		return i18n.T("bar-network-connecting")
	}
	return i18n.T("bar-network-disconnected")
}

// wiredIcon is wired_icon.
func wiredIcon(cfg config.NetworkConfig, s network.Status) string {
	switch s {
	case network.StatusConnected:
		return cfg.WiredConnectedIcon
	case network.StatusConnecting:
		return cfg.WiredAcquiringIcon
	}
	return cfg.WiredDisconnectedIcon
}

// wiredLabel is wired_label.
func wiredLabel(s network.Status) string {
	switch s {
	case network.StatusConnected:
		return i18n.T("bar-network-wired")
	case network.StatusConnecting:
		return i18n.T("bar-network-connecting")
	}
	return i18n.T("bar-network-disconnected")
}

// network is the module: the wifi/wired label.
type networkModule struct {
	ctx    ModuleContext
	source network.Source
	// vpn is the VPN service the icon overlays; nil without NM.
	vpn   *network.VPNService
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget
}

func newNetwork(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("network: requires the application loop")
	}
	if ctx.Network == nil {
		return nil, errors.New("network: no NetworkManager source available")
	}
	m := &networkModule{ctx: ctx, source: ctx.Network}
	if ctx.Networking != nil {
		m.vpn = ctx.Networking.VPN
	}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.Network.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Network.Subscribe(ctx.Life())
	if err != nil {
		return nil, err
	}
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
		stop()
	}()
	if m.vpn != nil {
		changes := m.vpn.Changes(ctx.Life())
		go func() {
			for range changes {
				m.ctx.Invoke(func() { _ = m.refresh() })
			}
		}()
	}
	return m, nil
}

// refresh re-reads and restyles the label and state icon.
func (m *networkModule) refresh() error {
	snap, err := m.source.Read(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Network
	var vpn vpnView
	if m.vpn != nil {
		vpn = vpnView{configured: !m.vpn.IsEmpty(), state: m.vpn.Aggregate()}
	}
	icon, label := networkDisplay(cfg, snap, vpn)
	if !cfg.LabelShow {
		label = ""
	}
	m.label.SetText(label)
	if m.icon != nil {
		m.icon.SetThemeName(icon)
	}
	return nil
}

func (m *networkModule) Root() widget.Widget { return m.root }
