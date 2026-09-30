package bar

import (
	"time"

	"github.com/stubbedev/gelm/widget"
)

// dropdownBuilders maps the Rust registry's names onto content
// builders; each returns the popover's card tree.
func dropdownBuilders() map[string]func(ctx ModuleContext) widget.Widget {
	return map[string]func(ctx ModuleContext) widget.Widget{
		"calendar":     calendarDropdown,
		"battery":      simpleStatusDropdown("Battery"),
		"brightness":   simpleStatusDropdown("Brightness"),
		"volume":       simpleStatusDropdown("Volume"),
		"microphone":   simpleStatusDropdown("Microphone"),
		"network":      simpleStatusDropdown("Network"),
		"bluetooth":    simpleStatusDropdown("Bluetooth"),
		"media":        simpleStatusDropdown("Media"),
		"audio":        simpleStatusDropdown("Audio"),
		"notification": simpleStatusDropdown("Notifications"),
		"recorder":     simpleStatusDropdown("Recorder"),
		"power":        simpleStatusDropdown("Power"),
		"treeman":      simpleStatusDropdown("Treeman"),
		"mail":         simpleStatusDropdown("Mail"),
		"weather":      simpleStatusDropdown("Weather"),
		"dashboard":    simpleStatusDropdown("Dashboard"),
	}
}

// calendarDropdown is the calendar card: the live clock hero above the
// month grid (the Rust CalendarDropdown's two halves).
func calendarDropdown(ctx ModuleContext) widget.Widget {
	font := ctx.Font
	px := 14.0
	if ctx.Style != nil {
		px = ctx.Style.labelPx
	}
	col := widget.NewBox(widget.Column, 8, 14)
	// The hero: HH:MM at display size.
	now := time.Now()
	hero := widget.NewLabel(font, px*2.6, now.Format("15:04"), ctx.Style.fg)
	col.Append(hero, false)
	if ctx.Style != nil {
		hero.SetColor(ctx.Style.fg)
	}
	// The month grid; gelm's calendar carries its own day names.
	cal := widget.NewCalendar(font, px, now)
	col.Append(cal, false)
	return col
}

// simpleStatusDropdown builds the placeholder cards: a titled panel
// the real per-module content lands in one dropdown at a time. The
// placeholder keeps every binding live instead of erroring.
func simpleStatusDropdown(title string) func(ctx ModuleContext) widget.Widget {
	return func(ctx ModuleContext) widget.Widget {
		px := 14.0
		if ctx.Style != nil {
			px = ctx.Style.labelPx
		}
		head := widget.NewLabel(ctx.Font, px*1.2, title, ctx.Style.fg)
		body := widget.NewLabel(ctx.Font, px, "Coming soon", mutedFg(ctx.Style.palette))
		col := widget.NewBox(widget.Column, 6, 16)
		col.Append(head, false)
		col.Append(body, false)
		return col
	}
}
