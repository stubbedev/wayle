package bar

import (
	"errors"
	"fmt"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/bluetooth"
)

// bluetoothLabel is helpers.rs's format_label: Off when unavailable or
// disabled, Disconnected with no devices, one device's alias, else the
// count.
func bluetoothLabel(snap bluetooth.Snapshot) string {
	if !snap.Available || !snap.Enabled {
		return "Off"
	}
	switch len(snap.Connected) {
	case 0:
		return "Disconnected"
	case 1:
		return snap.Connected[0]
	default:
		return fmt.Sprintf("%d Connected", len(snap.Connected))
	}
}

// bluetoothIconName is helpers.rs's select_icon: disabled when the
// adapter is absent or off, searching while discovering, then the
// connected states.
func bluetoothIconName(cfg config.BluetoothConfig, snap bluetooth.Snapshot) string {
	if !snap.Available || !snap.Enabled {
		return cfg.DisabledIcon
	}
	if snap.Discovering {
		return cfg.SearchingIcon
	}
	if len(snap.Connected) == 0 {
		return cfg.DisconnectedIcon
	}
	return cfg.ConnectedIcon
}

// bluetooth is the module: the BlueZ status label.
type bluetoothModule struct {
	ctx    ModuleContext
	source bluetooth.Source
	label  *widget.Label
	icon   widget.Widget
	root   widget.Widget
}

func newBluetooth(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("bluetooth: requires the application loop")
	}
	if ctx.Bluetooth == nil {
		return nil, errors.New("bluetooth: no BlueZ source available")
	}
	m := &bluetoothModule{ctx: ctx, source: ctx.Bluetooth}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.Bluetooth.Icon)
	m.root = assembleModule(ctx, ctx.Config.Bluetooth.Icon, m.label)
	m.refresh()
	startBtPairingNotifier(ctx.Bluetooth)
	// The module lives as long as the bar; the subscription ends with
	// the service.
	ticks, _ := ctx.Bluetooth.Subscribe()
	go func() {
		for range ticks {
			m.ctx.Invoke(m.refresh)
		}
	}()
	return m, nil
}

// refresh restyles the label and state icon from the service state.
func (m *bluetoothModule) refresh() {
	snap := m.source.State().Snapshot()
	cfg := m.ctx.Config.Bluetooth
	label := ""
	if cfg.LabelShow {
		label = bluetoothLabel(snap)
	}
	color := m.ctx.Style.fg
	if !snap.Available || !snap.Enabled || len(snap.Connected) == 0 {
		color = mutedFg(m.ctx.Style.palette)
	}
	m.label.SetText(label)
	m.label.SetColor(color)
	if setter, ok := m.icon.(interface{ SetThemeName(name string) }); ok {
		setter.SetThemeName(bluetoothIconName(cfg, snap))
	}
}

func (m *bluetoothModule) Root() widget.Widget { return m.root }
