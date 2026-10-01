package config

// HyprsunsetConfig is ported from crates/wayle-config/src/schemas/modules/hyprsunset/mod.rs.
//
// Toggle for Hyprland's blue-light filter.
type HyprsunsetConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ status }}` - Filter status text (On, Off)
	// - `{{ temp }}` - Current temperature in Kelvin (shows "--" when disabled)
	// - `{{ gamma }}` - Current gamma percentage (shows "--" when disabled)
	// - `{{ config_temp }}` - Configured temperature (always available)
	// - `{{ config_gamma }}` - Configured gamma (always available)
	//
	// ## Examples
	//
	// - `"{{ status }}"` - "On"
	// - `"{{ temp }}K {{ gamma }}%"` - "4500K 80%"
	// - `"{{ status }} ({{ temp }}K)"` - "On (4500K)"
	Format string `cfg:"format"`
	// Color temperature in Kelvin when filter is enabled. Range: 1000-20000.
	Temperature uint32 `cfg:"temperature"`
	// Display gamma percentage when filter is enabled. Range: 0-200.
	Gamma uint32 `cfg:"gamma"`
	// Automatically enable the filter at night and disable it during the day,
	// based on local sunrise/sunset computed from `latitude`/`longitude`.
	//
	// While enabled, the module drives the filter on the solar schedule. A
	// manual click toggles an override that lasts until the next sunrise or
	// sunset, after which the schedule resumes.
	AutoSchedule bool `cfg:"auto-schedule"`
	// Latitude for the sunrise/sunset schedule, in decimal degrees
	// (north positive). Range: -90 to 90. Only used when `auto-schedule` is on.
	Latitude float64 `cfg:"latitude"`
	// Longitude for the sunrise/sunset schedule, in decimal degrees
	// (east positive). Range: -180 to 180. Only used when `auto-schedule` is on.
	Longitude float64 `cfg:"longitude"`
	// Icon when filter is disabled (showing normal daylight colors).
	IconOff string `cfg:"icon-off"`
	// Icon when filter is enabled (showing warm night colors).
	IconOn string `cfg:"icon-on"`
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
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Default toggles blue light filter.
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

// DefaultsHyprsunset returns the schema defaults.
func DefaultsHyprsunset() HyprsunsetConfig {
	return HyprsunsetConfig{
		Format:         "{{ status }}",
		Temperature:    5000,
		Gamma:          100,
		AutoSchedule:   false,
		Latitude:       0,
		Longitude:      0,
		IconOff:        "ld-sun-symbolic",
		IconOn:         "ld-moon-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("yellow"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("yellow"),
		LabelShow:      true,
		LabelColor:     mustColor("yellow"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ParseClickAction(":toggle"),
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c HyprsunsetConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
