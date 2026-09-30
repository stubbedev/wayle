package bar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/bluetooth"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

func TestBluetoothLabelMatchesRustAssertions(t *testing.T) {
	// helpers.rs's cases: disabled, none, one alias, count.
	if got := bluetoothLabel(bluetooth.Snapshot{Available: true}); got != "Off" {
		t.Errorf("unpowered = %q, want Off", got)
	}
	if got := bluetoothLabel(bluetooth.Snapshot{}); got != "Off" {
		t.Errorf("no adapter = %q, want Off", got)
	}
	if got := bluetoothLabel(bluetooth.Snapshot{Available: true, Enabled: true}); got != "Disconnected" {
		t.Errorf("idle = %q, want Disconnected", got)
	}
	one := bluetooth.Snapshot{Available: true, Enabled: true, Connected: []string{"WH-1000XM5"}}
	if got := bluetoothLabel(one); got != "WH-1000XM5" {
		t.Errorf("one device = %q, want the alias", got)
	}
	many := bluetooth.Snapshot{Available: true, Enabled: true, Connected: []string{"a", "b"}}
	if got := bluetoothLabel(many); got != "2 Connected" {
		t.Errorf("two devices = %q, want the count", got)
	}
}

// btState builds a powered single-adapter state with the given
// connected aliases.
func btState(connected ...string) bluetooth.State {
	st := bluetooth.State{Available: true, Enabled: true, Primary: &bluetooth.Adapter{Powered: true}}
	for _, alias := range connected {
		st.Devices = append(st.Devices, bluetooth.Device{Alias: alias, Connected: true, Paired: true})
	}
	return st
}

func TestBluetoothModuleDimsWhenIdle(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())
	source := newFakeBluetooth(btState("Headset"))
	m := &bluetoothModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	m.refresh()
	if got := m.label.Text(); got != "Headset" {
		t.Errorf("label = %q, want the alias", got)
	}
	if m.label.Color() != style.fg {
		t.Errorf("connected color = %#08x, want the default fg", m.label.Color())
	}

	source.setState(btState())
	m.refresh()
	if got := m.label.Text(); got != "Disconnected" {
		t.Errorf("idle label = %q", got)
	}
	if m.label.Color() != mutedFg(style.palette) {
		t.Errorf("idle color = %#08x, want fg-muted", m.label.Color())
	}
}

func TestBluetoothStateIcon(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())
	source := newFakeBluetooth(btState("Headset"))
	m := &bluetoothModule{ctx: ModuleContext{Config: cfg, Font: testFont(t), Style: &style}, source: source}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(m.ctx, cfg.Bluetooth.Icon)
	if m.icon == nil {
		t.Fatal("the bluetooth icon defaults on")
	}
	icon := m.icon.(*widget.Icon)

	// The connected state first.
	m.refresh()
	if got := icon.Name(); got != cfg.Bluetooth.ConnectedIcon {
		t.Errorf("connected icon = %q", got)
	}
	// Idle, searching, and disabled follow select_icon's order.
	source.setState(btState())
	m.refresh()
	if got := icon.Name(); got != cfg.Bluetooth.DisconnectedIcon {
		t.Errorf("idle icon = %q", got)
	}
	searching := btState()
	searching.Primary.Discovering = true
	source.setState(searching)
	m.refresh()
	if got := icon.Name(); got != cfg.Bluetooth.SearchingIcon {
		t.Errorf("searching icon = %q", got)
	}
	source.setState(bluetooth.State{Available: true, Primary: &bluetooth.Adapter{}})
	m.refresh()
	if got := icon.Name(); got != cfg.Bluetooth.DisabledIcon {
		t.Errorf("disabled icon = %q", got)
	}
}

func TestMicrophoneStateIcon(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())
	source := &fakePulseSource{dev: pulse.Device{Volume: pct(40)}}
	m := &microphoneModule{ctx: ModuleContext{Config: cfg, Font: testFont(t), Style: &style}, source: source}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(m.ctx, cfg.Microphone.Icon)
	if m.icon == nil {
		t.Fatal("the microphone icon defaults on")
	}
	icon := m.icon.(*widget.Icon)

	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != cfg.Microphone.Icon.Name {
		t.Errorf("active icon = %q", got)
	}
	source.dev.Muted = true
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != cfg.Microphone.IconMuted {
		t.Errorf("muted icon = %q", got)
	}
}

func TestNewBluetoothRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Bluetooth = nil
	if _, err := Create("bluetooth", ctx); err == nil {
		t.Fatal("nil bluetooth source: want an error, got a module")
	}
}

func TestLoadFileAppliesBluetooth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.bluetooth]\nlabel-show = false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Bluetooth.LabelShow {
		t.Errorf("config = %+v", c.Bluetooth)
	}
}
