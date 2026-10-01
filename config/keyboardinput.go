package config

// KeyboardInputConfig is ported from crates/wayle-config/src/schemas/modules/keyboard_input/mod.rs.
//
// Active keyboard layout indicator.
type KeyboardInputConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ layout }}` - Raw layout name from the compositor (e.g., "English (US)")
	// - `{{ alias }}` - User-defined alias from `layout-alias-map`, falls back to `{{ layout }}`
	//
	// ## Examples
	//
	// - `"{{ layout }}"` - "English (US)"
	// - `"{{ alias }}"` - "EN" (with alias map configured)
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
	// Language name mapping.
	//
	// ## Example
	//
	// ```toml
	// [modules.keyboard-input.layout-alias-map]
	// "English (US)" = "EN"
	// "Czech (QWERTY)" = "Czech"
	// ```
	LayoutAliasMap map[string]string `cfg:"layout-alias-map"`
}

// DefaultsKeyboardInput returns the schema defaults.
func DefaultsKeyboardInput() KeyboardInputConfig {
	return KeyboardInputConfig{
		Format:         "{{ alias }}",
		IconName:       "ld-keyboard-symbolic",
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
		LayoutAliasMap: map[string]string{},
	}
}

// Clicks returns the five input bindings.
func (c KeyboardInputConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
