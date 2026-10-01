package config

// SystrayConfig is ported from crates/wayle-config/src/schemas/modules/systray/mod.rs.
//
// System tray icons via the StatusNotifierItem protocol.
type SystrayConfig struct {
	// Tray item icon size. Accepts a scale multiplier or pixels (e.g. `"20px"`).
	IconScale Size `cfg:"icon-scale"`
	// Gap between tray items. Accepts a scale multiplier or pixels (e.g. `"4px"`).
	ItemGap Size `cfg:"item-gap"`
	// Padding at the ends of the container. Accepts a scale multiplier or pixels (e.g. `"8px"`).
	//
	// Applies to left/right edges for horizontal bars, or top/bottom edges
	// for vertical bars.
	InternalPadding Size `cfg:"internal-padding"`
	// Glob patterns for tray items to hide.
	//
	// Matches against item ID or title.
	// Example: `["*discord*", "Steam"]`
	Blacklist []string `cfg:"blacklist"`
	// Custom icon and color overrides.
	//
	// First matching override wins. Supports glob patterns.
	//
	// ```toml
	// [[module.systray.overrides]]
	// name = "*discord*"
	// icon = "si-discord-symbolic"
	// color = "blue"
	// ```
	Overrides []TrayItemOverride `cfg:"overrides"`
	// Display border around container.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Container background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
}

// DefaultsSystray returns the schema defaults.
func DefaultsSystray() SystrayConfig {
	return SystrayConfig{
		IconScale:       Size{Value: 1, Unit: SizeMultiplier},
		ItemGap:         Size{Value: 0.25, Unit: SizeMultiplier},
		InternalPadding: Size{Value: 0.5, Unit: SizeMultiplier},
		Blacklist:       []string{},
		Overrides:       []TrayItemOverride{},
		BorderShow:      false,
		BorderColor:     mustColor("border-accent"),
		ButtonBgColor:   mustColor("bg-surface-elevated"),
	}
}

// TrayItemOverride is ported from crates/wayle-config/src/schemas/modules/systray/mod.rs.
//
// Custom icon and color override for tray items matching a pattern.
type TrayItemOverride struct {
	// Glob pattern to match against item ID or title.
	//
	// Examples: `"discord"`, `"*Discord*"`, `"org.kde.*"`
	Name string `cfg:"name,required"`
	// Custom icon name (symbolic icon).
	Icon *string `cfg:"icon"`
	// Custom icon color.
	Color *ColorValue `cfg:"color"`
}

func (TrayItemOverride) noStructDefault() {}

// SystrayIconBaseRem is the rem the icon-scale multiplier resolves
// against (systray ICON_BASE_REM).
const SystrayIconBaseRem = 1.25
