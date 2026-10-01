package config

// KeybindModeConfig is ported from crates/wayle-config/src/schemas/modules/keybind_mode/mod.rs.
//
// Current keybind-mode indicator for modal compositors.
type KeybindModeConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ mode }}` - Current keybind mode name (shows "default" when inactive)
	//
	// ## Examples
	//
	// - `"{{ mode }}"` - "resize"
	// - `"Mode: {{ mode }}"` - "Mode: resize"
	// - `"[{{ mode }}]"` - "[resize]"
	Format string `cfg:"format"`
	// Symbolic icon name.
	IconName string `cfg:"icon-name"`
	// Automatically hide module when no mode is active.
	AutoHide bool `cfg:"auto-hide"`
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

// DefaultsKeybindMode returns the schema defaults.
func DefaultsKeybindMode() KeybindModeConfig {
	return KeybindModeConfig{
		Format:         "{{ mode }}",
		IconName:       "ld-layers-symbolic",
		AutoHide:       false,
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
	}
}

// Clicks returns the five input bindings.
func (c KeybindModeConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
