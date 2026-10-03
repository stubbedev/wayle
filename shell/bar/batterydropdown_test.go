package bar

import (
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/upower"
)

func TestBatteryFormatters(t *testing.T) {
	for in, want := range map[float64]string{8.2: "8.2W", 45: "45W", 9.96: "10.0W", 10: "10W"} {
		if got := formatWatts(in); got != want {
			t.Errorf("formatWatts(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{7.2: "7.2 Wh", 60: "60 Wh"} {
		if got := formatWattHours(in); got != want {
			t.Errorf("formatWattHours(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{92: "92%", 0: "--", -1: "--"} {
		if got := healthValue(in); got != want {
			t.Errorf("healthValue(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[uint32]uint32{80: 75, 5: 0, 3: 0} {
		if got := resumeThreshold(in); got != want {
			t.Errorf("resumeThreshold(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestBatteryClasses(t *testing.T) {
	crit := upower.Device{Percentage: 12, WarningLevel: upower.WarningCritical}
	low := upower.Device{Percentage: 15, WarningLevel: upower.WarningNone}
	normal := upower.Device{Percentage: 76, WarningLevel: upower.WarningNone}
	if got := batteryLevelClass(crit, "good"); got != "crit" {
		t.Errorf("gauge critical = %q", got)
	}
	if got := batteryLevelClass(low, "good"); got != "warn" {
		t.Errorf("gauge low = %q", got)
	}
	if got := batteryLevelClass(normal, "good"); got != "good" {
		t.Errorf("gauge normal = %q", got)
	}
	if got := batteryLevelClass(normal, ""); got != "" {
		t.Errorf("hero normal = %q, want none", got)
	}
	if got := heroStateClass(upower.Device{State: upower.StateCharging}); got != "good" {
		t.Errorf("hero charging = %q", got)
	}
	if got := heroStateClass(upower.Device{State: upower.StateCharging, WarningLevel: upower.WarningAction}); got != "crit" {
		t.Errorf("hero critical beats charging: %q", got)
	}
	if got := heroStateClass(upower.Device{State: upower.StateDischarging}); got != "" {
		t.Errorf("hero discharging = %q, want none", got)
	}
	for in, want := range map[float64]string{92: "good", 80: "good", 68: "fair", 50: "fair", 30: "poor", 0: "unknown"} {
		if got := healthClass(in); got != want {
			t.Errorf("healthClass(%v) = %q, want %q", in, got, want)
		}
	}
	if got := batteryStateLabel(upower.Device{State: upower.StateCharging, WarningLevel: upower.WarningLow}); got != i18n.T("dropdown-battery-critical") {
		t.Errorf("low while charging = %q, want critical", got)
	}
}

func presentBattery() upower.Device {
	return upower.Device{
		Percentage: 64, State: upower.StateDischarging, IsPresent: true,
		TimeToEmpty: 2 * time.Hour, EnergyRate: 8.2, EnergyFull: 55, Capacity: 91,
		WarningLevel: upower.WarningNone, ChargeEndThreshold: 80,
	}
}

func TestBatteryDropdownEmptyWithoutBattery(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := batteryDropdown(ctx).(*batteryView)
	if v.body.Visible() != "empty" {
		t.Errorf("no service: page %q", v.body.Visible())
	}
	ctx.Battery = newFakeBattery(upower.Device{Percentage: 50, State: upower.StateDischarging})
	v = batteryDropdown(ctx).(*batteryView)
	defer v.dropdownClosed()
	if v.body.Visible() != "empty" {
		t.Errorf("IsPresent false: page %q, want empty", v.body.Visible())
	}
}

func TestBatteryDropdownSection(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	src := newFakeBattery(presentBattery())
	ctx.Battery = src
	v := batteryDropdown(ctx).(*batteryView)
	defer v.dropdownClosed()
	if v.body.Visible() != "battery" {
		t.Fatalf("page %q, want battery", v.body.Visible())
	}
	if v.heroPct.Text() != "64%" || v.heroState.Text() != i18n.T("dropdown-battery-on-battery") {
		t.Errorf("hero = %q / %q", v.heroPct.Text(), v.heroState.Text())
	}
	if !v.heroTime.Visible() || v.heroInput.Visible() {
		t.Errorf("discharging: time shown %v, input shown %v", v.heroTime.Visible(), v.heroInput.Visible())
	}
	if v.drawValue.Text() != "8.2W" || v.drawLabel.Text() != i18n.T("dropdown-battery-draw") {
		t.Errorf("draw = %q %q", v.drawValue.Text(), v.drawLabel.Text())
	}
	if v.capacityValue.Text() != "55 Wh" || v.healthText.Text() != "91%" || !v.healthDot.HasClass("good") {
		t.Errorf("capacity %q health %q", v.capacityValue.Text(), v.healthText.Text())
	}
	if !v.gauge.HasClass("good") || v.gauge.Value() != 0.64 {
		t.Errorf("gauge %v good=%v", v.gauge.Value(), v.gauge.HasClass("good"))
	}
	if v.chargeCard.Visible() || !v.chargeUnsupported.Visible() {
		t.Error("unsupported threshold: want the note, not the card")
	}

	// Charging at a critical level with a supported, enabled limit.
	src.update(func(d *upower.Device) {
		d.State, d.WarningLevel, d.Percentage = upower.StateCharging, upower.WarningCritical, 9
		d.TimeToFull, d.ChargeThresholdSupported, d.ChargeThresholdEnabled = time.Hour, true, true
	})
	v.applyDevice(v.readDevice(t.Context()))
	if v.heroState.Text() != i18n.T("dropdown-battery-critical") || !v.heroState.HasClass("crit") || !v.heroPct.HasClass("crit") {
		t.Errorf("critical hero = %q", v.heroState.Text())
	}
	if v.heroPct.HasClass("good") || !v.gauge.HasClass("crit") || v.gauge.HasClass("good") {
		t.Error("variant classes did not swap")
	}
	if !v.heroInput.Visible() || v.drawLabel.Text() != i18n.T("dropdown-battery-input") {
		t.Error("charging with a rate: want the input line and label")
	}
	if !v.chargeCard.Visible() || v.chargeUnsupported.Visible() || !v.chargeSwitch.On() {
		t.Error("supported threshold: want the card, switched on")
	}
	if v.chargeSubtitle.Text() != i18n.T("dropdown-battery-resumes-at", i18n.Str("threshold", "75")) {
		t.Errorf("resume = %q", v.chargeSubtitle.Text())
	}
	if calls := src.thresholdCalls(); len(calls) != 0 {
		t.Errorf("a backend state change wrote the threshold: %v", calls)
	}

	// The user toggling writes it.
	v.chargeSwitch.Toggle()
	waitHeadless(t, "the threshold write", func() bool { return len(src.thresholdCalls()) == 1 })
	if calls := src.thresholdCalls(); calls[0] {
		t.Errorf("toggle off wrote %v", calls)
	}
}

func TestBatteryDropdownProfiles(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := batteryDropdown(ctx).(*batteryView)
	if !v.profilesUnavailable.Visible() {
		t.Error("no daemon: want the note")
	}
	for name, b := range v.profileButtons {
		if b.Enabled() {
			t.Errorf("no daemon: %s enabled", name)
		}
	}

	pp := &fakePPSource{snap: powerprofiles.Snapshot{
		Available: true, Active: powerprofiles.ProfileBalanced,
		Profiles: []string{powerprofiles.ProfilePowerSaver, powerprofiles.ProfileBalanced},
	}, ticks: make(chan struct{}, 1)}
	ctx.PowerProfiles = pp
	v = batteryDropdown(ctx).(*batteryView)
	defer v.dropdownClosed()
	if v.profilesUnavailable.Visible() {
		t.Error("daemon up: the note shows")
	}
	if !v.profileButtons[powerprofiles.ProfileBalanced].HasState(widget.StateChecked) {
		t.Error("balanced is not marked active")
	}
	if v.profileButtons[powerprofiles.ProfilePerformance].Enabled() {
		t.Error("performance enabled though the daemon lacks it")
	}

	// Selecting the active segment writes nothing; another one does.
	onHeadlessLoop(func() bool { v.selectProfile(powerprofiles.ProfileBalanced); return true })
	onHeadlessLoop(func() bool { v.selectProfile(powerprofiles.ProfilePowerSaver); return true })
	waitHeadless(t, "the profile write", func() bool { return len(pp.sets()) == 1 })
	if got := pp.sets(); !slices.Equal(got, []string{powerprofiles.ProfilePowerSaver}) {
		t.Errorf("sets = %v", got)
	}
	waitHeadless(t, "the saver mark", func() bool {
		return v.profileButtons[powerprofiles.ProfilePowerSaver].HasState(widget.StateChecked) &&
			!v.profileButtons[powerprofiles.ProfileBalanced].HasState(widget.StateChecked)
	})

	// A daemon-side change follows while open.
	pp.mu.Lock()
	pp.snap.Active = powerprofiles.ProfileBalanced
	pp.mu.Unlock()
	pp.ticks <- struct{}{}
	waitHeadless(t, "the followed profile", func() bool {
		return v.profileButtons[powerprofiles.ProfileBalanced].HasState(widget.StateChecked)
	})
}
