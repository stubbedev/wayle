package config

// CpuConfig is ported from crates/wayle-config/src/schemas/modules/cpu/mod.rs.
//
// CPU usage, frequency, and temperature.
type CpuConfig struct {
	// Polling interval in milliseconds.
	//
	// Faster polling increases CPU usage.
	PollIntervalMs uint64 `cfg:"poll-interval-ms"`
	// Temperature sensor label.
	//
	// Use `"auto"` for automatic detection, or specify a
	// label (e.g., `"Tctl"`, `"Package id 0"`).
	//
	// Run `sensors` to see available labels.
	TempSensor string `cfg:"temp-sensor"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ percent }}` - CPU usage (0-100)
	// - `{{ freq_ghz }}` - Frequency of the busiest core (highest usage)
	// - `{{ avg_freq_ghz }}` - Average frequency across cores
	// - `{{ max_freq_ghz }}` - Maximum frequency among cores
	// - `{{ temp_c }}` - Temperature in Celsius (if available)
	// - `{{ temp_f }}` - Temperature in Fahrenheit (if available)
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "45%"
	// - `"{{ percent }}% @ {{ freq_ghz }}GHz"` - "45% @ 3.2GHz"
	// - `"{{ percent }}% {{ temp_c }}C"` - "45% 62C"
	Format string `cfg:"format"`
	// Icon name.
	IconName string `cfg:"icon-name"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation. Set to 0 to disable.
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
	// Dynamic color thresholds based on CPU usage percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., high CPU usage).
	//
	// ## Example
	//
	// ```toml
	// [[modules.cpu.thresholds]]
	// above = 70
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.cpu.thresholds]]
	// above = 90
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsCpu returns the schema defaults.
func DefaultsCpu() CpuConfig {
	return CpuConfig{
		PollIntervalMs: 2000,
		TempSensor:     "auto",
		Format:         "{{ percent }}%",
		IconName:       "ld-cpu-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("blue"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("blue"),
		LabelShow:      true,
		LabelColor:     mustColor("blue"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ClickAction{},
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
		Thresholds:     []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c CpuConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
