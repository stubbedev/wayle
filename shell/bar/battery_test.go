package bar

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/upower"
	"github.com/stubbedev/wayle/styling"
)

func TestBatteryLabelMatchesRustAssertions(t *testing.T) {
	// The exact cases from helpers.rs's format_label tests.
	if got := batteryLabel("{{ percent }}%", 75.0, true); got != "75%" {
		t.Errorf("= %q, want 75%%", got)
	}
	if got := batteryLabel("{{ percent }}", 100.0, true); got != "100" {
		t.Errorf("= %q, want 100", got)
	}
	if got := batteryLabel("Bat: {{ percent }}", 0.4, true); got != "Bat: 0" {
		t.Errorf("= %q, want rounding down to 0", got)
	}
	if got := batteryLabel("{{ percent }}%", 50.0, false); got != i18n.T("bar-battery-unavailable") {
		t.Errorf("absent battery = %q, want N/A", got)
	}
	if got := batteryLabel("{{percent}}%", 75.0, true); got != "75%" {
		t.Errorf("no-space braces = %q, want 75%%", got)
	}
	if got := batteryLabel("{{ percent }} / {{percent}}", 42.6, true); got != "43 / 43" {
		t.Errorf("repeated var = %q, want 43 twice", got)
	}
}

func TestThresholdMatching(t *testing.T) {
	// threshold.rs matches: value >= above, value <= below, inclusive.
	above := config.ThresholdEntry{Above: ptrF(70)}
	if !above.Matches(80) || !above.Matches(70) {
		t.Error("above=70: 80 and 70 should match (inclusive)")
	}
	if above.Matches(69.9) {
		t.Error("above=70: 69.9 should not match")
	}
	both := config.ThresholdEntry{Above: ptrF(10), Below: ptrF(20)}
	if !both.Matches(15) || !both.Matches(10) || !both.Matches(20) {
		t.Error("10..20: 10, 15, and 20 should match")
	}
	if both.Matches(5) || both.Matches(25) {
		t.Error("10..20: 5 and 25 should not match")
	}
	if (config.ThresholdEntry{}).Matches(50) {
		t.Error("a threshold without bounds never matches")
	}
}

func TestEvaluateThresholdsLastMatchWinsPerColor(t *testing.T) {
	warn, _ := config.ParseColorValue("status-warning")
	errc, _ := config.ParseColorValue("status-error")
	entries := []config.ThresholdEntry{
		{Above: ptrF(50), IconColor: &warn, LabelColor: &warn},
		{Above: ptrF(90), IconColor: &errc},
	}
	got := config.EvaluateThresholds(95, entries)
	if got.IconColor == nil || *got.IconColor != errc {
		t.Errorf("icon = %v, want the later error override", got.IconColor)
	}
	if got.LabelColor == nil || *got.LabelColor != warn {
		t.Errorf("label = %v, want the warning (the later entry sets no label color)", got.LabelColor)
	}
	if none := config.EvaluateThresholds(10, entries); none.IconColor != nil || none.LabelColor != nil {
		t.Errorf("below every bound = %+v, want no overrides", none)
	}
}

func ptrF(v float64) *float64 {
	p := new(float64)
	*p = v
	return p
}

// fakeBattery is a scriptable upower.Source, safe across a follow
// goroutine and the test.
type fakeBattery struct {
	mu        sync.Mutex
	dev       upower.Device
	ticks     chan struct{}
	read      chan struct{}
	threshold []bool
}

func newFakeBattery(dev upower.Device) *fakeBattery {
	return &fakeBattery{dev: dev, ticks: make(chan struct{}, 4), read: make(chan struct{}, 1)}
}

func (f *fakeBattery) Read(context.Context) (upower.Device, error) {
	select {
	case f.read <- struct{}{}:
	default:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dev, nil
}

// update edits the scripted device.
func (f *fakeBattery) update(edit func(*upower.Device)) {
	f.mu.Lock()
	edit(&f.dev)
	f.mu.Unlock()
}

func (f *fakeBattery) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	return f.ticks, func() {}, nil
}

func (f *fakeBattery) EnableChargeThreshold(_ context.Context, enabled bool) error {
	f.mu.Lock()
	f.threshold = append(f.threshold, enabled)
	f.mu.Unlock()
	return nil
}

// thresholdCalls are the EnableChargeThreshold arguments so far.
func (f *fakeBattery) thresholdCalls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.threshold)
}

func (f *fakeBattery) Push() { f.ticks <- struct{}{} }

func TestBatteryModuleRendersAndRestyles(t *testing.T) {
	cfg := config.Defaults()
	cv, err := config.ParseColorValue("status-error")
	if err != nil {
		t.Fatal(err)
	}
	// label-color recolors the label; icon-color the icon.
	cfg.Battery.Thresholds = []config.ThresholdEntry{{Below: ptrF(20), LabelColor: &cv, IconColor: &cv}}
	cfg.Bar.Layout = []config.BarLayout{{Monitor: "*"}}

	source := newFakeBattery(upower.Device{Percentage: 75, State: upower.StateDischarging, IsPresent: true})
	style := computeStyle(cfg, styling.Default())
	ctx := ModuleContext{Config: cfg, Font: testFont(t), Style: &style, Battery: source}
	// App is required by the module; Invoke is what the subscription
	// pumps through. Build with a nil App is refused, so run the module
	// lifecycle by hand here: construct, read, restyle.
	if _, err := Create("battery", ctx); err == nil {
		t.Fatal("nil App: want an error (no loop to subscribe on)")
	}

	// The refresh path itself is App-independent; drive it directly.
	m := &battery{ctx: ctx, source: source}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, cfg.Battery.Icon())
	btn := newBarButton(ctx, m.icon, m.label)
	m.setButton(btn)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "75%" {
		t.Errorf("label = %q, want 75%%", got)
	}
	if strings.Contains(btn.InlineStyle(), "--bar-btn-icon-color: "+cv.ToCSS()) {
		t.Errorf("the threshold colored the button at 75%%: %q", btn.InlineStyle())
	}

	// Drop below the threshold: same template, error color.
	source.update(func(d *upower.Device) { d.Percentage = 12 })
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "12%" {
		t.Errorf("label = %q, want 12%%", got)
	}
	if !strings.Contains(btn.InlineStyle(), "--bar-btn-icon-color: "+cv.ToCSS()) {
		t.Errorf("at 12%% the button vars = %q, want the status-error threshold", btn.InlineStyle())
	}
	// Back above: the defaults return.
	source.update(func(d *upower.Device) { d.Percentage = 60 })
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(btn.InlineStyle(), cv.ToCSS()) {
		t.Errorf("above the bound: button vars = %q, want the threshold gone", btn.InlineStyle())
	}
}

func TestBatteryStateIcon(t *testing.T) {
	cfg := config.Defaults()
	source := newFakeBattery(upower.Device{Percentage: 75, State: upower.StateDischarging, IsPresent: true})
	style := computeStyle(cfg, styling.Default())
	ctx := ModuleContext{Config: cfg, Font: testFont(t), Style: &style, Battery: source}
	m := &battery{ctx: ctx, source: source}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, cfg.Battery.Icon())
	if m.icon == nil {
		t.Fatal("the battery icon defaults on")
	}
	icon := m.icon

	// Discharging: the level list bucketed over the percentage.
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != "md-battery_android_frame_6-symbolic" {
		t.Errorf("icon at 75%% = %q", got)
	}
	// Charging overrides the level list.
	source.update(func(d *upower.Device) { d.State = upower.StateCharging })
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != cfg.Battery.ChargingIcon {
		t.Errorf("charging icon = %q", got)
	}
	// Absent falls back to the alert icon.
	source.update(func(d *upower.Device) { d.State, d.Percentage, d.IsPresent = upower.StateUnknown, 0, false })
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := icon.Name(); got != cfg.Battery.AlertIcon {
		t.Errorf("absent icon = %q", got)
	}
}

func TestBatteryAbsentBatteryShowsNA(t *testing.T) {
	cfg := config.Defaults()
	source := newFakeBattery(upower.Device{})
	style := computeStyle(cfg, styling.Default())
	m := &battery{ctx: ModuleContext{Config: cfg, Font: testFont(t), Style: &style, Battery: source}, source: source}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != i18n.T("bar-battery-unavailable") {
		t.Errorf("label = %q, want N/A", got)
	}
}

func TestLoadFileAppliesBattery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[modules.battery]
format = "Bat {{ percent }}"
label-show = false

[[modules.battery.thresholds]]
below = 15
icon-color = "status-error"

[[modules.battery.thresholds]]
above = 90
icon-color = "status-success"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Battery.Format != "Bat {{ percent }}" || c.Battery.LabelShow {
		t.Errorf("format/label-show = %q/%v", c.Battery.Format, c.Battery.LabelShow)
	}
	if len(c.Battery.Thresholds) != 2 {
		t.Fatalf("thresholds = %+v, want 2", c.Battery.Thresholds)
	}
	if c.Battery.Thresholds[0].Below == nil || *c.Battery.Thresholds[0].Below != 15 {
		t.Errorf("threshold[0] = %+v", c.Battery.Thresholds[0])
	}
	if c.Battery.Thresholds[1].IconColor == nil || c.Battery.Thresholds[1].IconColor.Token != config.TokenStatusSuccess || c.Battery.Thresholds[1].LabelColor != nil {
		t.Errorf("threshold[1] = %+v", c.Battery.Thresholds[1])
	}
}

func TestLoadFileRejectsBadBattery(t *testing.T) {
	for _, content := range []string{
		"[modules.battery]\n[[modules.battery.thresholds]]\nbelow = 10\nicon-color = \"nope\"\n",
		"[modules.battery]\n[[modules.battery.thresholds]]\nbelow = \"low\"\n",
		"[modules.battery]\n[[modules.battery.thresholds]]\nbelow = 10\nlabel-color = \"nope\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}

func TestStateFromUint32(t *testing.T) {
	for _, tc := range []struct {
		wire uint32
		want upower.DeviceState
	}{
		{1, upower.StateCharging},
		{2, upower.StateDischarging},
		{4, upower.StateFullyCharged},
		{7, upower.StateUnknown},
	} {
		if got := upower.StateFromUint32(tc.wire); got != tc.want {
			t.Errorf("StateFromUint32(%d) = %v, want %v", tc.wire, got, tc.want)
		}
	}
}

func testFont(t *testing.T) render.Font {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

// The dropdown hero follows methods.rs's state_label and time_display.
func TestBatteryDropdownStateAndTime(t *testing.T) {
	for state, id := range map[upower.DeviceState]string{
		upower.StateCharging:         "dropdown-battery-charging",
		upower.StatePendingCharge:    "dropdown-battery-charging",
		upower.StateFullyCharged:     "dropdown-battery-plugged-in",
		upower.StateDischarging:      "dropdown-battery-on-battery",
		upower.StateUnknown:          "dropdown-battery-on-battery",
		upower.StatePendingDischarge: "dropdown-battery-on-battery",
	} {
		if got := batteryStateLabel(upower.Device{State: state}); got != i18n.T(id) {
			t.Errorf("state %d = %q, want %s", state, got, id)
		}
	}
	hm := i18n.T("dropdown-battery-duration-hm", i18n.Str("hours", "3"), i18n.Str("minutes", "04"))
	discharging := upower.Device{State: upower.StateDischarging, TimeToEmpty: 3*time.Hour + 4*time.Minute, TimeToFull: time.Hour}
	if got := batteryTimeDisplay(discharging); got != i18n.T("dropdown-battery-time-remaining", i18n.Str("duration", hm)) {
		t.Errorf("discharging = %q", got)
	}
	m := i18n.T("dropdown-battery-duration-m", i18n.Str("minutes", "18"))
	charging := upower.Device{State: upower.StateCharging, TimeToEmpty: 5 * time.Hour, TimeToFull: 18 * time.Minute}
	if got := batteryTimeDisplay(charging); got != i18n.T("dropdown-battery-time-until-full", i18n.Str("duration", m)) {
		t.Errorf("charging = %q", got)
	}
	// Unknown times (0) show nothing, whichever side the state reads.
	if got := batteryTimeDisplay(upower.Device{State: upower.StateCharging, TimeToEmpty: time.Hour}); got != "" {
		t.Errorf("charging without time-to-full = %q, want empty", got)
	}
}
