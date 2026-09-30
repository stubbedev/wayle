package bar

import (
	"context"
	"errors"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/styling"
)

// networkLabel follows helpers.rs's wifi_label/wired_label: the SSID
// when connected (the _bar.ftl fallback when hidden), then the
// connecting/disconnected vocabulary, with wifi preferred while
// enabled.
func networkLabel(snap network.Snapshot, cfg config.NetworkConfig) string {
	if snap.WifiEnabled {
		switch {
		case snap.WifiConnected:
			if snap.WifiSSID != "" {
				return snap.WifiSSID
			}
			return cfg.WifiFallback
		case snap.WifiConnecting:
			return cfg.Connecting
		case snap.WiredConnected:
			return cfg.Wired
		}
		return cfg.Disconnected
	}
	switch {
	case snap.WiredConnected:
		return cfg.Wired
	case snap.WiredConnecting:
		return cfg.Connecting
	}
	return cfg.Disconnected
}

// networkColor dims the label when nothing is connected.
func networkColor(snap network.Snapshot, palette *styling.Palette, fallback render.Color) render.Color {
	connected := snap.WifiConnected || snap.WiredConnected
	if !connected {
		return mutedFg(palette)
	}
	return fallback
}

// networkIconName follows helpers.rs's wifi_icon/wired_icon: the wifi
// states win while the radio is on (acquiring, offline, or the signal
// list bucketed over the strength, connected as the fallback), then
// the wired states. VPN state does not change the glyph.
func networkIconName(cfg config.NetworkConfig, snap network.Snapshot) string {
	if snap.WifiEnabled {
		switch {
		case snap.WifiConnecting:
			return cfg.WifiAcquiringIcon
		case snap.WifiConnected:
			if len(cfg.WifiSignalIcons) > 0 {
				return cfg.WifiSignalIcons[network.SignalIndex(snap.WifiStrength, len(cfg.WifiSignalIcons))]
			}
			return cfg.WifiConnectedIcon
		case snap.WifiSSID != "" || snap.WifiStrength > 0:
			return cfg.WifiConnectedIcon
		}
		return cfg.WifiOfflineIcon
	}
	switch {
	case snap.WiredConnected:
		return cfg.WiredConnectedIcon
	case snap.WiredConnecting:
		return cfg.WiredAcquiringIcon
	}
	return cfg.WiredDisconnectedIcon
}

// network is the module: the wifi/wired label.
type networkModule struct {
	ctx    ModuleContext
	source network.Source
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
}

func newNetwork(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("network: requires the application loop")
	}
	if ctx.Network == nil {
		return nil, errors.New("network: no NetworkManager source available")
	}
	m := &networkModule{ctx: ctx, source: ctx.Network}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.Network.Icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Network.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
		stop()
	}()
	return m, nil
}

// refresh re-reads and restyles the label and state icon.
func (m *networkModule) refresh() error {
	snap, err := m.source.Read(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Network
	label := ""
	if cfg.LabelShow {
		label = networkLabel(snap, cfg)
	}
	m.label.SetText(label)
	m.label.SetColor(networkColor(snap, m.ctx.Style.palette, m.ctx.Style.fg))
	if setter := m.icon; setter != nil {
		setter.SetThemeName(networkIconName(cfg, snap))
	}
	return nil
}

func (m *networkModule) Root() widget.Widget { return m.root }
