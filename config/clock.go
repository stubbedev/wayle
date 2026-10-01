package config

// WeekStart is ported from crates/wayle-config/src/schemas/modules/clock/mod.rs.
//
// Which day appears in the first column of the calendar dropdown.
type WeekStart string

// WeekStart values.
const (
	// Week starts on Monday.
	WeekStartMonday WeekStart = "monday"
	// Week starts on Tuesday.
	WeekStartTuesday WeekStart = "tuesday"
	// Week starts on Wednesday.
	WeekStartWednesday WeekStart = "wednesday"
	// Week starts on Thursday.
	WeekStartThursday WeekStart = "thursday"
	// Week starts on Friday.
	WeekStartFriday WeekStart = "friday"
	// Week starts on Saturday.
	WeekStartSaturday WeekStart = "saturday"
	// Week starts on Sunday (default).
	WeekStartSunday WeekStart = "sunday"
)

var _ = registerEnum(WeekStartMonday, WeekStartTuesday, WeekStartWednesday, WeekStartThursday, WeekStartFriday, WeekStartSaturday, WeekStartSunday)

// ClockConfig is ported from crates/wayle-config/src/schemas/modules/clock/mod.rs.
//
// Clock module configuration.
type ClockConfig struct {
	// Format string using strftime syntax.
	//
	// ## Common Specifiers
	//
	// - `%H` - Hour (00-23)
	// - `%I` - Hour (01-12)
	// - `%M` - Minute (00-59)
	// - `%S` - Second (00-59)
	// - `%p` - AM/PM
	// - `%a` - Abbreviated weekday (Mon, Tue)
	// - `%A` - Full weekday (Monday)
	// - `%b` - Abbreviated month (Jan, Feb)
	// - `%B` - Full month (January)
	// - `%d` - Day of month (01-31)
	// - `%Y` - Year (2024)
	//
	// ## Examples
	//
	// - `"%H:%M"` - "14:30"
	// - `"%I:%M %p"` - "02:30 PM"
	// - `"%a %b %d %I:%M %p"` - "Mon Jan 15 02:30 PM"
	Format string `cfg:"format"`
	// Symbolic icon name.
	IconName string `cfg:"icon-name"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display text label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Show seconds in the calendar dropdown clock display.
	DropdownShowSeconds bool `cfg:"dropdown-show-seconds"`
	// First day of the week in the calendar dropdown.
	CalendarWeekdayStart WeekStart `cfg:"calendar-weekday-start"`
}

// DefaultsClock returns the schema defaults.
func DefaultsClock() ClockConfig {
	return ClockConfig{
		Format:               "%a %b %d %I:%M %p",
		IconName:             "tb-calendar-time-symbolic",
		BorderShow:           false,
		BorderColor:          mustColor("border-accent"),
		IconShow:             true,
		IconColor:            mustColor("auto"),
		IconBgColor:          mustColor("accent"),
		LabelShow:            true,
		LabelColor:           mustColor("accent"),
		LabelMaxLength:       0,
		ButtonBgColor:        mustColor("bg-surface-elevated"),
		LeftClick:            ParseClickAction("dropdown:calendar"),
		RightClick:           ParseClickAction("dropdown:weather"),
		MiddleClick:          ClickAction{},
		ScrollUp:             ClickAction{},
		ScrollDown:           ClickAction{},
		DropdownShowSeconds:  false,
		CalendarWeekdayStart: WeekStartSunday,
	}
}

// Clicks returns the five input bindings.
func (c ClockConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
