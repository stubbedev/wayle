package bar

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/upower"
)

func TestDashboardBatteryHelpers(t *testing.T) {
	for _, tc := range []struct {
		pct      float64
		charging bool
		want     string
	}{
		{90, false, "ld-battery-full-symbolic"},
		{75, false, "ld-battery-medium-symbolic"},
		{51, false, "ld-battery-medium-symbolic"},
		{50, false, "ld-battery-low-symbolic"},
		{25, false, "ld-battery-warning-symbolic"},
		{10, true, "ld-battery-charging-symbolic"},
	} {
		if got := dashboardBatteryIcon(tc.pct, tc.charging); got != tc.want {
			t.Errorf("%v%% charging=%v = %q, want %q", tc.pct, tc.charging, got, tc.want)
		}
	}
	if got := dashboardBatteryTime(3900); got != i18n.T("dropdown-dashboard-battery-time-hm", i18n.Str("hours", "1"), i18n.Str("minutes", "5")) {
		t.Errorf("1h5m = %q", got)
	}
	if got := dashboardBatteryTime(300); got != i18n.T("dropdown-dashboard-battery-time-m", i18n.Str("minutes", "5")) {
		t.Errorf("5m = %q", got)
	}
	if dashboardBatteryTime(0) != "" {
		t.Error("no estimate rendered a time")
	}
	if label, icon := dashboardProfile(powerprofiles.ProfilePowerSaver); icon != "ld-leaf-symbolic" || label != i18n.T("dropdown-dashboard-battery-profile-saver") {
		t.Errorf("saver = %q %q", label, icon)
	}
	if _, icon := dashboardProfile("weird"); icon != "ld-scale-symbolic" {
		t.Errorf("unknown profile icon = %q, want balanced's", icon)
	}
}

func TestDashboardSpeedAndThresholds(t *testing.T) {
	if v, mega := dashboardSpeed(512); v != "0.5" || mega {
		t.Errorf("512 B/s = %q %v", v, mega)
	}
	if v, mega := dashboardSpeed(1536 * 1024); v != "1.5" || !mega {
		t.Errorf("1.5 MiB/s = %q %v", v, mega)
	}
	if dashboardThresholdClass(95, 70, 90) != "error" ||
		dashboardThresholdClass(70, 70, 90) != "warning" ||
		dashboardThresholdClass(69.9, 70, 90) != "success" {
		t.Error("threshold classes")
	}
}

// The dashboard paints from the stylesheet: the battery gauge and its
// status read threshold state classes instead of fills and tints, and
// the quick-action icons follow the cascade.
func TestDashboardPaintsFromTheStylesheet(t *testing.T) {
	cfg := config.Defaults()
	ctx := styledContext(t, cfg)
	ctx.Media = newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.x", Title: "Song", Artist: "Band", State: mpris.StatePlaying})
	ctx.Battery = newFakeBattery(upower.Device{Percentage: 8, State: upower.StateDischarging, IsPresent: true, TimeToEmpty: 20 * time.Minute})
	v := dashboardDropdown(ctx).(*dashboardView)

	var gauge *widget.ProgressBar
	var percent *widget.Label
	walkTree(v, func(w widget.Widget) bool {
		switch w := w.(type) {
		case *widget.ProgressBar:
			if w.HasClass("progress-bar") {
				gauge = w
			}
		case *widget.Label:
			if w.HasClass("battery-percent") {
				percent = w
			}
		}
		return true
	})
	if gauge == nil || percent == nil {
		t.Fatalf("the battery card = gauge %v, percent %v; want a classed gauge and percent", gauge, percent)
	}
	if gauge.Fill != 0 {
		t.Errorf("the gauge carries a fill %#08x", uint32(gauge.Fill))
	}
	if !gauge.HasClass("error") || gauge.HasClass("success") || gauge.HasClass("warning") {
		t.Error("a critical battery did not put the gauge in the error state")
	}
	if got := percent.Color(); got != 0 {
		t.Errorf("the percent carries a programmatic color %#08x", uint32(got))
	}
	if !percent.HasClass("critical") {
		t.Error("a critical battery did not mark the percent")
	}

	// The quick-action icons have no tint: .quick-action-icon image
	// inks them, .active inverts to fg-on-accent.
	walkTree(v, func(w widget.Widget) bool {
		if b, ok := w.(*widget.Button); ok && b.HasClass("quick-action") {
			walkTree(b, func(c widget.Widget) bool {
				if icon, ok := c.(*widget.Icon); ok && icon.Tint() != 0 {
					t.Errorf("a quick-action icon carries a tint %#08x", uint32(icon.Tint()))
				}
				return true
			})
		}
		return true
	})

	// No network: the speeds read muted through the class, uncolored.
	walkTree(v, func(w widget.Widget) bool {
		if l, ok := w.(*widget.Label); ok && l.HasClass("speed-value") {
			if got := l.Color(); got != 0 || !l.HasClass("muted") {
				t.Errorf("a disconnected speed = color %#08x muted %v", uint32(l.Color()), l.HasClass("muted"))
			}
		}
		return true
	})
}

func TestDashboardQuickActionsDriveTheServices(t *testing.T) {
	// DND persists to the state dir; keep it out of the real one.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Notifications = notifications.NewService()
	ctx.Notifications.SetDND(false)
	ctx.IdleInhibit = idleinhibit.NewState(0)
	ctx.Media = newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.x", Title: "Song", Artist: "Band", State: mpris.StatePlaying, CanGoNext: true})
	v := dashboardDropdown(ctx).(*dashboardView)
	if len(v.refreshers) == 0 {
		t.Fatal("no sections refresh")
	}
	// The quick actions in grid order: wifi, bluetooth, airplane, dnd,
	// idle inhibit, power saver.
	var actions []*widget.Button
	walkTree(v, func(w widget.Widget) bool {
		if b, ok := w.(*widget.Button); ok && b.HasClass("quick-action") {
			actions = append(actions, b)
		}
		return true
	})
	if len(actions) != 6 {
		t.Fatalf("quick actions = %d, want 6", len(actions))
	}
	wifi, dnd, idle := actions[0], actions[3], actions[4]
	dnd.OnClick()
	if !ctx.Notifications.DND() {
		t.Error("the DND toggle did not reach the service")
	}
	v.refreshAll()
	if !dnd.HasClass("active") {
		t.Error("DND on did not mark its button active")
	}
	idle.OnClick()
	if !ctx.IdleInhibit.Active() {
		t.Error("the idle toggle did not enable inhibition")
	}
	// No wifi device: the wifi toggle is insensitive.
	if wifi.Enabled() {
		t.Error("wifi without a device is clickable")
	}
}

// walkTree visits w and its descendants depth-first while visit
// returns true.
func walkTree(w widget.Widget, visit func(widget.Widget) bool) {
	if w == nil || !visit(w) {
		return
	}
	if p, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, c := range p.Children() {
			walkTree(c, visit)
		}
	}
}

func TestDashboardMediaAndBatterySections(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Media = newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.x", Title: "Song", Artist: "Band", State: mpris.StatePlaying})
	ctx.Battery = newFakeBattery(upower.Device{Percentage: 8, State: upower.StateDischarging, IsPresent: true, TimeToEmpty: 20 * time.Minute})
	v := dashboardDropdown(ctx).(*dashboardView)
	texts := map[string]bool{}
	walkTree(v, func(w widget.Widget) bool {
		if l, ok := w.(*widget.Label); ok {
			texts[l.Text()] = true
		}
		return true
	})
	for _, want := range []string{"Song", "Band", "8%", i18n.T("dropdown-dashboard-battery-time-m", i18n.Str("minutes", "20")), i18n.T("dropdown-dashboard-now-playing")} {
		if !texts[want] {
			t.Errorf("dashboard is missing %q", want)
		}
	}
	// Without a player the media card shows the empty state.
	ctx.Media = newFakeMedia()
	v = dashboardDropdown(ctx).(*dashboardView)
	found := false
	walkTree(v, func(w widget.Widget) bool {
		if s, ok := w.(*widget.Stack); ok && s.Visible() == "empty" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("no player did not show the empty state")
	}
}
