package config

// WorldClockConfig is ported from crates/wayle-config/src/schemas/modules/world_clock/mod.rs.
//
// Multiple timezones shown together in a dropdown.
type WorldClockConfig struct {
	// Format string with embedded timezone blocks.
	//
	// Use `{{ tz('timezone', 'strftime') }}` to insert a formatted time.
	// Anything outside a placeholder stays as literal text.
	//
	// ## Examples
	//
	// | Format string | Renders as |
	// |---|---|
	// | `"{{ tz('UTC', '%H:%M %Z') }}"` | `14:30 UTC` |
	// | `"NYC {{ tz('America/New_York', '%H:%M') }}  TYO {{ tz('Asia/Tokyo', '%H:%M') }}"` | `NYC 09:30  TYO 23:30` |
	// | `"{{ tz('America/New_York', '%H:%M %Z') }} \| {{ tz('Europe/London', '%H:%M %Z') }}"` | `09:30 EST \| 14:30 GMT` |
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
}

// DefaultsWorldClock returns the schema defaults.
func DefaultsWorldClock() WorldClockConfig {
	return WorldClockConfig{
		Format:         "{{ tz('UTC', '%H:%M %Z') }}",
		IconName:       "ld-globe-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("yellow"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("yellow"),
		LabelShow:      true,
		LabelColor:     mustColor("yellow"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ClickAction{},
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c WorldClockConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
