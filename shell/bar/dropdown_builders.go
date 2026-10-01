package bar

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/sysinfo"
	"github.com/stubbedev/wayle/service/upower"
)

// dropdownBuilders maps the Rust registry's names onto content
// builders; each returns the popover's card tree.
func dropdownBuilders() map[string]func(ctx ModuleContext) widget.Widget {
	return map[string]func(ctx ModuleContext) widget.Widget{
		"calendar":     calendarDropdown,
		"power":        powerDropdown,
		"audio":        audioDropdown,
		"volume":       audioDropdown,
		"microphone":   audioDropdown,
		"brightness":   brightnessDropdown,
		"battery":      batteryDropdown,
		"network":      networkDropdown,
		"bluetooth":    bluetoothDropdown,
		"media":        mediaDropdown,
		"notification": notificationDropdown,
		"recorder":     recorderDropdown,
		"treeman":      treemanDropdown,
		"mail":         mailDropdown,
		"weather":      weatherDropdown,
		"dashboard":    dashboardDropdown,
	}
}

// dropdownFont resolves the card's label size from the bar style.
func dropdownFont(ctx ModuleContext) (render.Font, float64) {
	if ctx.Style != nil {
		return ctx.Font, ctx.Style.labelPx
	}
	return ctx.Font, 14
}

// calendarDropdown is the calendar card: the live clock hero above the
// month grid (the Rust CalendarDropdown's two halves).
func calendarDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 8, 14)
	// The hero: HH:MM at display size.
	now := time.Now()
	hero := widget.NewLabel(font, px*2.6, now.Format("15:04"), ctx.Style.fg)
	col.Append(hero, false)
	// The month grid; gelm's calendar carries its own day names.
	cal := widget.NewCalendar(font, px, now)
	col.Append(cal, false)
	return col
}

// powerDropdown is the session menu: one row per configured command,
// in the schema's order (lock, logout, suspend, reboot, shutdown).
// Rows run through the same spawn path as the button bindings.
func powerDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	cfg := ctx.Config.Power
	col := widget.NewBox(widget.Column, 4, 10)
	for _, row := range []struct {
		label   string
		icon    string
		command string
		show    bool
	}{
		{i18n.T("dropdown-dashboard-lock"), "ld-lock-symbolic", cfg.LockCommand, cfg.ShowLock},
		{i18n.T("dropdown-dashboard-logout"), "ld-log-out-symbolic", cfg.LogoutCommand, cfg.ShowLogout},
		{"Suspend", "ld-moon-symbolic", cfg.SuspendCommand, cfg.ShowSuspend},
		{i18n.T("dropdown-dashboard-reboot"), "ld-refresh-cw-symbolic", cfg.RebootCommand, cfg.ShowReboot},
		{i18n.T("dropdown-dashboard-power-off"), "ld-power-symbolic", cfg.ShutdownCommand, cfg.ShowShutdown},
	} {
		if !row.show || row.command == "" {
			continue
		}
		col.Append(dropdownRow(ctx, font, px, row.label, row.icon, func() {
			spawnCommand(row.command)
		}), false)
	}
	return col
}

// spawnCommand runs one config command detached; failures log.
func spawnCommand(command string) {
	if command == "" {
		return
	}
	if err := exec.Command("sh", "-c", command).Start(); err != nil { //nolint:gosec // the command comes from the user's own config
		log.Printf("command %q: %v", command, err)
	}
}

// audioDropdown is the audio card: the output and input rows, each a
// device label, a mute toggle, and a volume slider wired to the pulse
// service's setters.
func audioDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 10, 14)
	if ctx.Pulse == nil {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-audio-no-devices-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	bctx := context.Background()
	if sink, err := ctx.Pulse.DefaultSink(bctx); err == nil {
		col.Append(audioDeviceRow(ctx, font, px, i18n.T("dropdown-audio-output"), sink.Device,
			func(v float64) { _ = ctx.Pulse.SetVolume(bctx, v) },
			func(m bool) { _ = ctx.Pulse.SetMuted(bctx, m) }), false)
	}
	if source, err := ctx.Pulse.DefaultSource(bctx); err == nil {
		col.Append(audioDeviceRow(ctx, font, px, i18n.T("dropdown-audio-input"), source.Device,
			func(float64) {},
			func(m bool) { _ = ctx.Pulse.SetSourceMuted(bctx, m) }), false)
	}
	return col
}

// audioDeviceRow builds one device's row: name + mute toggle + slider.
func audioDeviceRow(ctx ModuleContext, font render.Font, px float64, title string, dev pulse.Device, setVolume func(float64), setMuted func(bool)) widget.Widget {
	col := widget.NewBox(widget.Column, 4, 0)
	row := widget.NewBox(widget.Row, 8, 0)
	row.Append(widget.NewThemeIcon(volumeIconName(ctx.Config.Volume, dev), int(px)), false)
	row.Append(widget.NewLabel(font, px, title, ctx.Style.fg), true)
	muteIcon := widget.NewThemeIcon("ld-volume-x-symbolic", int(px))
	muteIcon.SetTint(mutedFg(ctx.Style.palette))
	muteButton := widget.NewButton(muteIcon, 2, 4)
	muteButton.OnClick = func() { setMuted(!dev.Muted) }
	row.Append(muteButton, false)
	col.Append(row, false)
	slider := widget.NewSlider(0, 100, 1, dev.Volume.AveragePercentage())
	slider.OnChanged = func(v float64) { setVolume(v) }
	col.Append(slider, false)
	return col
}

// brightnessDropdown is the backlight card: a slider per backlight
// device, writing through the service's clamped Set.
func brightnessDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 10, 14)
	if ctx.Brightness == nil {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-brightness-empty-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	bctx := context.Background()
	devices, err := ctx.Brightness.Devices(bctx)
	if err != nil || len(devices) == 0 {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-brightness-empty-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	for _, dev := range devices {
		percent := dev.Percentage()
		row := widget.NewBox(widget.Row, 8, 0)
		row.Append(widget.NewThemeIcon(brightnessOsdIcon(percent), int(px)), false)
		row.Append(widget.NewLabel(font, px, dev.Name, ctx.Style.fg), true)
		col.Append(row, false)
		slider := widget.NewSlider(0, 100, 1, percent)
		name := dev.Name
		slider.OnChanged = func(v float64) {
			if err := ctx.Brightness.Set(bctx, name, v); err != nil {
				log.Printf("brightness %s: %v", name, err)
			}
		}
		col.Append(slider, false)
	}
	return col
}

// batteryDropdown is the battery card: percentage, state, and the
// time-to-empty when the daemon reports one.
func batteryDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 8, 14)
	if ctx.Battery == nil {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-battery-no-battery-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	dev, err := ctx.Battery.Read(context.Background())
	if err != nil {
		col.Append(widget.NewLabel(font, px, i18n.T("dropdown-battery-no-battery-title"), mutedFg(ctx.Style.palette)), false)
		return col
	}
	col.Append(widget.NewLabel(font, px*1.6, batteryLabel("{{ percent }}%", dev.Percentage, dev.Present()), ctx.Style.fg), false)
	col.Append(widget.NewLabel(font, px, batteryStateLabel(dev.State), mutedFg(ctx.Style.palette)), false)
	if text := batteryTimeDisplay(dev); text != "" {
		col.Append(widget.NewLabel(font, px, text, mutedFg(ctx.Style.palette)), false)
	}
	return col
}

// batteryCharging is battery_section/methods.rs's is_charging.
func batteryCharging(state upower.DeviceState) bool {
	return state == upower.StateCharging || state == upower.StatePendingCharge
}

// batteryStateLabel is methods.rs's state_label. The critical state
// keys off UPower's WarningLevel, which the Go service does not read
// yet, so the label starts at charging.
func batteryStateLabel(state upower.DeviceState) string {
	switch {
	case batteryCharging(state):
		return i18n.T("dropdown-battery-charging")
	case state == upower.StateFullyCharged:
		return i18n.T("dropdown-battery-plugged-in")
	default:
		return i18n.T("dropdown-battery-on-battery")
	}
}

// batteryTimeDisplay is methods.rs's time_display: the time until full
// while charging, the time remaining otherwise, "" when unknown.
func batteryTimeDisplay(dev upower.Device) string {
	remaining, id := dev.TimeToEmpty, "dropdown-battery-time-remaining"
	if batteryCharging(dev.State) {
		remaining, id = dev.TimeToFull, "dropdown-battery-time-until-full"
	}
	seconds := int64(remaining.Seconds())
	if seconds <= 0 {
		return ""
	}
	hours, minutes := seconds/3600, (seconds%3600)/60
	duration := i18n.T("dropdown-battery-duration-m", i18n.Str("minutes", strconv.FormatInt(minutes, 10)))
	if hours > 0 {
		duration = i18n.T("dropdown-battery-duration-hm",
			i18n.Str("hours", strconv.FormatInt(hours, 10)),
			i18n.Str("minutes", fmt.Sprintf("%02d", minutes)))
	}
	return i18n.T(id, i18n.Str("duration", duration))
}

// dashboardDropdown is the dashboard card: the clock hero, the date,
// and the system stats section (battery, CPU, memory), with the
// user-session card at the bottom. The weather section joins the
// weather cache port; the severity colors wait for the status-token
// pass.
func dashboardDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 8, 16)
	now := time.Now()
	col.Append(widget.NewLabel(font, px*2.2, now.Format("15:04"), ctx.Style.fg), false)
	col.Append(widget.NewLabel(font, px, now.Format("Monday, January 2"), mutedFg(ctx.Style.palette)), false)
	if stats := dashboardStats(ctx, font, px); stats != nil {
		col.Append(stats, false)
	}
	col.Append(userSessionSection(ctx), false)
	return col
}

// dashboardCPU is the dashboard's CPU reader, kept across opens.
var dashboardCPU = &sysinfo.CPUReader{Sensor: "auto"}

// dashboardStats builds the system stats lines; nil when nothing reads.
func dashboardStats(ctx ModuleContext, font render.Font, px float64) widget.Widget {
	var lines []string
	if ctx.Battery != nil {
		if dev, err := ctx.Battery.Read(context.Background()); err == nil {
			lines = append(lines, i18n.T("dropdown-dashboard-battery")+": "+batteryLabel("{{ percent }}%", dev.Percentage, dev.Present()))
		}
	}
	// Usage is a delta: the reader keeps the counters between opens (the
	// first open shows the since-boot average, as sysinfo's first refresh).
	if cpu, err := dashboardCPU.Read(); err == nil {
		lines = append(lines, i18n.T("dropdown-dashboard-cpu")+": "+pad2(cpu.UsagePercent)+"%")
	}
	if mem, err := sysinfo.ReadMemory(); err == nil {
		lines = append(lines, i18n.T("dropdown-dashboard-ram")+": "+strconv.FormatFloat(mem.UsagePercent(), 'f', 0, 64)+"%")
	}
	if len(lines) == 0 {
		return nil
	}
	col := widget.NewBox(widget.Column, 4, 0)
	for _, line := range lines {
		col.Append(widget.NewLabel(font, px, line, ctx.Style.fg), false)
	}
	return col
}

// dropdownRow builds one tappable menu row.
func dropdownRow(ctx ModuleContext, font render.Font, px float64, title, iconName string, onRun func()) widget.Widget {
	icon := widget.NewThemeIcon(iconName, int(px))
	label := widget.NewLabel(font, px, title, ctx.Style.fg)
	row := widget.NewBox(widget.Row, 10, 8)
	row.Append(icon, false)
	row.Append(label, true)
	button := widget.NewButton(row, 4, 6)
	if ctx.Style != nil {
		button.BgHover = ctx.Style.buttonBgHover
		button.BgPressed = ctx.Style.buttonBgActive
	}
	button.OnClick = onRun
	return button
}
