package config

// AppIconSource is ported from crates/wayle-config/src/schemas/modules/volume/mod.rs.
//
// Icon source for app volume entries in the dropdown.
type AppIconSource string

// AppIconSource values.
const (
	// Wayle's curated symbolic icons matched by app name.
	AppIconSourceMapped AppIconSource = "mapped"
	// Native application icons reported by PulseAudio.
	AppIconSourceNative AppIconSource = "native"
)

var _ = registerEnum(AppIconSourceMapped, AppIconSourceNative)

// VolumeConfig is ported from crates/wayle-config/src/schemas/modules/volume/mod.rs.
//
// Output volume control with a dropdown for device and app volumes.
type VolumeConfig struct {
	// Icons for volume levels from low to maximum.
	//
	// The percentage is divided evenly among icons. With 3 icons:
	// 1-33% uses icons\[0\], 34-66% uses icons\[1\], 67-100% uses icons\[2\].
	LevelIcons []string `cfg:"level-icons"`
	// Icon shown when audio output is muted.
	IconMuted string `cfg:"icon-muted"`
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
	// - `{{ percent }}` - Volume (0-100)
	//
	// ## Examples
	//
	// - `"{{ percent }}%"` - "45%"
	Format string `cfg:"format"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Default opens the audio dropdown.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click. Default toggles mute.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Icon source for app volume entries in the audio dropdown.
	DropdownAppIcons AppIconSource `cfg:"dropdown-app-icons"`
	// Dynamic color thresholds based on volume percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., boosted volume).
	//
	// ## Example
	//
	// ```toml
	// [[modules.volume.thresholds]]
	// above = 100
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.volume.thresholds]]
	// above = 130
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsVolume returns the schema defaults.
func DefaultsVolume() VolumeConfig {
	return VolumeConfig{
		LevelIcons: []string{
			"ld-volume-symbolic",
			"ld-volume-1-symbolic",
			"ld-volume-2-symbolic",
		},
		IconMuted:        "ld-volume-x-symbolic",
		BorderShow:       false,
		BorderColor:      mustColor("red"),
		IconShow:         true,
		IconColor:        mustColor("auto"),
		IconBgColor:      mustColor("red"),
		LabelShow:        true,
		LabelColor:       mustColor("red"),
		Format:           "{{ percent }}%",
		LabelMaxLength:   0,
		ButtonBgColor:    mustColor("bg-surface-elevated"),
		LeftClick:        ParseClickAction("dropdown:audio"),
		RightClick:       ClickAction{},
		MiddleClick:      ParseClickAction("wayle audio output-mute"),
		ScrollUp:         ClickAction{},
		ScrollDown:       ClickAction{},
		DropdownAppIcons: AppIconSourceMapped,
		Thresholds:       []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c VolumeConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
