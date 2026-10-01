package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// dropdownBuilders maps the Rust registry's names onto content
// builders; each returns the popover's card tree.
func dropdownBuilders() map[string]func(ctx ModuleContext) widget.Widget {
	return map[string]func(ctx ModuleContext) widget.Widget{
		"calendar":     calendarDropdown,
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
