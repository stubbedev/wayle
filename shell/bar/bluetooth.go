package bar

import (
	"context"
	"errors"
	"fmt"

	"github.com/stubbedev/gelm/widget"

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

// bluetooth is the module: the BlueZ status label.
type bluetoothModule struct {
	ctx    ModuleContext
	source bluetooth.Source
	label  *widget.Label
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
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Bluetooth.Subscribe(context.Background())
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

// refresh re-reads and restyles the label.
func (m *bluetoothModule) refresh() error {
	snap, err := m.source.Read(context.Background())
	if err != nil {
		return err
	}
	label := ""
	if m.ctx.Config.Bluetooth.LabelShow {
		label = bluetoothLabel(snap)
	}
	color := m.ctx.Style.fg
	if !snap.Available || !snap.Enabled || len(snap.Connected) == 0 {
		color = mutedFg(m.ctx.Style.palette)
	}
	m.label.SetText(label)
	m.label.SetColor(color)
	return nil
}

func (m *bluetoothModule) Root() widget.Widget { return m.label }
