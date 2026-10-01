package config

// MicrophoneConfig is ported from crates/wayle-config/src/schemas/modules/microphone/mod.rs.
//
// Microphone input level and mute toggle.
type MicrophoneConfig struct {
	// Icon shown when microphone is active (unmuted).
	IconActive string `cfg:"icon-active"`
	// Icon shown when microphone is muted.
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
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click. Default toggles input mute.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Dynamic color thresholds based on microphone volume percentage.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., high input gain).
	//
	// ## Example
	//
	// ```toml
	// [[modules.microphone.thresholds]]
	// above = 70
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.microphone.thresholds]]
	// above = 90
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsMicrophone returns the schema defaults.
func DefaultsMicrophone() MicrophoneConfig {
	return MicrophoneConfig{
		IconActive:     "ld-mic-symbolic",
		IconMuted:      "ld-mic-off-symbolic",
		BorderShow:     false,
		BorderColor:    mustColor("red"),
		IconShow:       true,
		IconColor:      mustColor("auto"),
		IconBgColor:    mustColor("red"),
		LabelShow:      true,
		LabelColor:     mustColor("red"),
		LabelMaxLength: 0,
		ButtonBgColor:  mustColor("bg-surface-elevated"),
		LeftClick:      ParseClickAction("dropdown:audio"),
		RightClick:     ClickAction{},
		MiddleClick:    ParseClickAction("wayle audio input-mute"),
		ScrollUp:       ClickAction{},
		ScrollDown:     ClickAction{},
		Thresholds:     []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c MicrophoneConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
