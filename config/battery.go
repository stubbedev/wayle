package config

// BatteryConfig is ported from crates/wayle-config/src/schemas/modules/battery/mod.rs.
//
// Battery level, charging state, and a dropdown with power-profile controls.
//
// ::: warning
//
// This module uses `upower` (D-Bus) for battery information. Ensure `upower` daemon is running and exposes a battery device (verify with `upower --battery` or `upower --dump`).
//
// :::
type BatteryConfig struct {
	// Icons for battery levels from empty to full.
	//
	// The percentage is divided evenly among icons. With 5 icons:
	// 0-20% uses icons\[0\], 21-40% uses icons\[1\], etc.
	LevelIcons []string `cfg:"level-icons"`
	// Icon shown when battery is charging.
	ChargingIcon string `cfg:"charging-icon"`
	// Icon shown when battery is not present or in an error state.
	AlertIcon string `cfg:"alert-icon"`
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
	// Display percentage label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ percent }}` - Battery level (0-100)
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "45%"
	Format string `cfg:"format"`
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
	// Dynamic color thresholds based on battery percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `below` for low-value warnings (e.g., low battery).
	//
	// ## Example
	//
	// ```toml
	// [[modules.battery.thresholds]]
	// below = 40
	// icon-color = "status-warning"
	//
	// [[modules.battery.thresholds]]
	// below = 20
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsBattery returns the schema defaults.
func DefaultsBattery() BatteryConfig {
	return BatteryConfig{
		LevelIcons: []string{
			"md-battery_android_0-symbolic",
			"md-battery_android_frame_1-symbolic",
			"md-battery_android_frame_2-symbolic",
			"md-battery_android_frame_3-symbolic",
			"md-battery_android_frame_4-symbolic",
			"md-battery_android_frame_5-symbolic",
			"md-battery_android_frame_6-symbolic",
			"md-battery_android_frame_full-symbolic",
		},
		ChargingIcon:   "md-battery_android_frame_bolt-symbolic",
		AlertIcon:      "md-battery_android_alert-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("yellow"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("yellow"),
		LabelShow:      true,
		LabelColor:     mustColor("yellow"),
		Format:         "{{ percent }}%",
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ParseClickAction("dropdown:battery"),
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
		Thresholds:     []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c BatteryConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
