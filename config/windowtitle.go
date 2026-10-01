package config

// WindowTitleConfig is ported from crates/wayle-config/src/schemas/modules/window_title/mod.rs.
//
// Active window title with optional app-icon prefix.
type WindowTitleConfig struct {
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ title }}` - Window title
	// - `{{ app }}` - Application name (WM_CLASS on Hyprland)
	//
	// ## Examples
	//
	// - `"{{ title }}"` - "README.md - VSCode"
	// - `"{{ app }}: {{ title }}"` - "firefox: GitHub"
	Format string `cfg:"format"`
	// Fallback icon when no mapping matches.
	IconName string `cfg:"icon-name"`
	// Icon mappings. Glob patterns to icon names.
	//
	// Keys are patterns matching the window class (default) or title (when
	// prefixed with `title:`). Values are icon names from the installed icon
	// set. User mappings are checked before built-in mappings.
	//
	// ## Example
	//
	// ```toml
	// [modules.window-title.icon-mappings]
	// "*firefox*" = "ld-globe-symbolic"
	// "org.mozilla.*" = "ld-globe-symbolic"
	// "title:*YouTube*" = "ld-youtube-symbolic"
	// ```
	IconMappings map[string]string `cfg:"icon-mappings"`
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

// DefaultsWindowTitle returns the schema defaults.
func DefaultsWindowTitle() WindowTitleConfig {
	return WindowTitleConfig{
		Format:         "{{ title }}",
		IconName:       "ld-app-window-symbolic",
		IconMappings:   map[string]string{},
		BorderShow:     false,
		BorderColor:    mustColor("blue"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("blue"),
		LabelShow:      true,
		LabelColor:     mustColor("blue"),
		LabelMaxLength: 50,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ClickAction{},
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c WindowTitleConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
