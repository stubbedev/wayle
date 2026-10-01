package config

// PowerProfilesConfig is ported from crates/wayle-config/src/schemas/modules/power_profiles/mod.rs.
//
// Power profile indicator and switcher (power-profiles-daemon).
//
// Shows the active profile with a per-profile icon and color, and cycles
// through the available profiles on click. Backed by the same
// power-profiles-daemon D-Bus interface as `powerprofilesctl`.
type PowerProfilesConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ profile }}` - Active profile name (power-saver, balanced, performance)
	//
	// ## Examples
	//
	// - `"{{ profile }}"` - "balanced"
	Format string `cfg:"format"`
	// Icon shown while the power-saver profile is active.
	IconPowerSaver string `cfg:"icon-power-saver"`
	// Icon shown while the balanced profile is active.
	IconBalanced string `cfg:"icon-balanced"`
	// Icon shown while the performance profile is active.
	IconPerformance string `cfg:"icon-performance"`
	// Icon/label color while the power-saver profile is active.
	ColorPowerSaver ColorValue `cfg:"color-power-saver"`
	// Icon/label color while the balanced profile is active.
	ColorBalanced ColorValue `cfg:"color-balanced"`
	// Icon/label color while the performance profile is active.
	ColorPerformance ColorValue `cfg:"color-performance"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	//
	// Overridden per active profile by the `color-*` fields.
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
	// Action on left click. Default cycles to the next power profile.
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

// DefaultsPowerProfiles returns the schema defaults.
func DefaultsPowerProfiles() PowerProfilesConfig {
	return PowerProfilesConfig{
		Format:           "{{ profile }}",
		IconPowerSaver:   "ld-leaf-symbolic",
		IconBalanced:     "ld-scale-symbolic",
		IconPerformance:  "ld-rocket-symbolic",
		ColorPowerSaver:  mustColor("green"),
		ColorBalanced:    mustColor("blue"),
		ColorPerformance: mustColor("red"),
		BorderShow:       false,
		BorderColor:      mustColor("blue"),
		IconShow:         true,
		IconColor:        mustColor("auto"),
		IconBgColor:      mustColor("bg-surface-elevated"),
		LabelShow:        false,
		LabelColor:       mustColor("auto"),
		LabelMaxLength:   0,
		ButtonBgColor:    mustColor("bg-surface-elevated"),
		LeftClick:        ParseClickAction(":cycle"),
		RightClick:       ClickAction{},
		MiddleClick:      ClickAction{},
		ScrollUp:         ClickAction{},
		ScrollDown:       ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c PowerProfilesConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
