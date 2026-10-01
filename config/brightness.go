package config

// BrightnessConfig is ported from crates/wayle-config/src/schemas/modules/brightness/mod.rs.
//
// Backlight control bar module.
type BrightnessConfig struct {
	// Icons for brightness levels from low to maximum.
	//
	// The percentage is divided evenly among icons. With 3 icons:
	// 0-33% uses icons\[0\], 34-66% uses icons\[1\], 67-100% uses icons\[2\].
	LevelIcons []string `cfg:"level-icons"`
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
	// - `{{ percent }}` - Brightness (0-100)
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "65%"
	Format string `cfg:"format"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Lower bound (percent) for the native brightness-adjust action.
	//
	// Scrolling or clicking down never drops below this, so a dimmer cannot
	// reach a fully dark screen by accident. Use the brightness-toggle action
	// to intentionally blackout to 0%.
	MinBrightness uint32 `cfg:"min-brightness"`
	// Discover and control external monitors over DDC/CI (I²C).
	//
	// External monitors appear as extra sliders in the brightness panel.
	// Requires the `i2c-dev` kernel module and read/write access to
	// `/dev/i2c-*` (usually membership in the `i2c` group). Disable to skip
	// the slow DDC scan if you only have an internal panel.
	EnableExternal bool `cfg:"enable-external"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Default opens the brightness dropdown.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up. Default raises brightness by 5%.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down. Default lowers brightness by 5%, floored at
	// `min-brightness`.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Dynamic color thresholds based on brightness percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `below` for low-brightness warnings.
	//
	// ## Example
	//
	// ```toml
	// [[modules.brightness.thresholds]]
	// below = 20
	// icon-color = "status-warning"
	// label-color = "status-warning"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsBrightness returns the schema defaults.
func DefaultsBrightness() BrightnessConfig {
	return BrightnessConfig{
		LevelIcons: []string{
			"ld-sun-dim-symbolic",
			"ld-sun-medium-symbolic",
			"ld-sun-symbolic",
		},
		BorderShow:     false,
		BorderColor:    mustColor("yellow"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("yellow"),
		LabelShow:      true,
		LabelColor:     mustColor("yellow"),
		Format:         "{{ percent }}%",
		LabelMaxLength: 0,
		MinBrightness:  1,
		EnableExternal: true,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ParseClickAction("dropdown:brightness"),
		RightClick:     ClickAction{},
		MiddleClick:    ClickAction{},
		ScrollUp:       ParseClickAction("brightness:5"),
		ScrollDown:     ParseClickAction("brightness:-5"),
		Thresholds:     []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c BrightnessConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
