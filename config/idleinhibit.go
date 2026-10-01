package config

// IdleInhibitConfig is ported from crates/wayle-config/src/schemas/modules/idle_inhibit/mod.rs.
//
// Toggle that prevents screen dim, lock, and suspend while active.
//
// Controllable from the CLI: `wayle idle on|off|duration|remaining|status`.
type IdleInhibitConfig struct {
	// Duration in minutes when service starts. 0 means indefinite.
	StartupDuration uint32 `cfg:"startup-duration"`
	// Icon when idle inhibitor is inactive.
	IconInactive string `cfg:"icon-inactive"`
	// Icon when idle inhibitor is active.
	IconActive string `cfg:"icon-active"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ state }}` - Inhibitor state text (On, Off)
	// - `{{ remaining }}` - Time remaining (e.g., "45m", shows "--" when indefinite)
	// - `{{ duration }}` - Total duration (e.g., "60m", shows "--" when indefinite)
	//
	// ## Examples
	//
	// - `"{{ state }}"` - "On"
	// - `"{{ remaining }}/{{ duration }}"` - "45m/60m"
	// - `"{{ state }} ({{ remaining }})"` - "On (45m)"
	Format string `cfg:"format"`
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
	// Action on left click. Default toggles indefinite idle inhibit.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click. Default toggles timed idle inhibit.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsIdleInhibit returns the schema defaults.
func DefaultsIdleInhibit() IdleInhibitConfig {
	return IdleInhibitConfig{
		StartupDuration: 60,
		IconInactive:    "tb-coffee-off-symbolic",
		IconActive:      "tb-coffee-symbolic",
		Format:          "{{ state }}",
		BorderShow:      false,
		BorderColor:     mustColor("green"),
		IconShow:        true,
		IconColor:       mustColor("auto"),
		IconBgColor:     mustColor("green"),
		LabelShow:       true,
		LabelColor:      mustColor("green"),
		LabelMaxLength:  0,
		ButtonBgColor:   mustColor("bg-surface-elevated"),
		LeftClick:       ParseClickAction("wayle idle toggle --indefinite"),
		RightClick:      ParseClickAction("wayle idle toggle"),
		MiddleClick:     ClickAction{},
		ScrollUp:        ClickAction{},
		ScrollDown:      ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c IdleInhibitConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
