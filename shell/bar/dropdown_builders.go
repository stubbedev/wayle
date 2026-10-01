package bar

import (
	"log"
	"os/exec"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
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
