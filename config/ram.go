package config

// RamConfig is ported from crates/wayle-config/src/schemas/modules/ram/mod.rs.
//
// Memory and swap usage.
type RamConfig struct {
	// Polling interval in milliseconds.
	//
	// Faster polling increases CPU usage.
	PollIntervalMs uint64 `cfg:"poll-interval-ms"`
	// Format string for the label.
	//
	// ## Memory Placeholders
	//
	// - `{{ percent }}` - Memory usage as integer (0-100)
	// - `{{ used_gib }}` - Used memory in GiB (e.g., "7.2")
	// - `{{ total_gib }}` - Total memory in GiB (e.g., "16.0")
	// - `{{ available_gib }}` - Available memory in GiB (e.g., "8.8")
	//
	// ## Swap Placeholders
	//
	// - `{{ swap_percent }}` - Swap usage as integer (0-100)
	// - `{{ swap_used_gib }}` - Used swap in GiB
	// - `{{ swap_total_gib }}` - Total swap in GiB
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "45%"
	// - `"{{ used_gib }}/{{ total_gib }} GiB"` - "7.2/16.0 GiB"
	// - `"{{ percent }}% (Swap: {{ swap_percent }}%)"` - "45% (Swap: 12%)"
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
	// Dynamic color thresholds based on RAM usage percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., high memory usage).
	//
	// ## Example
	//
	// ```toml
	// [[modules.ram.thresholds]]
	// above = 80
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.ram.thresholds]]
	// above = 95
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsRam returns the schema defaults.
func DefaultsRam() RamConfig {
	return RamConfig{
		PollIntervalMs: 5000,
		Format:         "{{ percent }}%",
		IconName:       "ld-memory-stick-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("green"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("green"),
		LabelShow:      true,
		LabelColor:     mustColor("green"),
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
func (c RamConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
