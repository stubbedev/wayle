package config

// ScreenshotConfig is ported from crates/wayle-config/src/schemas/modules/screenshot/mod.rs.
//
// Screenshot capture button.
//
// Click the bar button to capture a region, output, or window. Controllable
// from the CLI / RPC socket: `wayle screenshot region|output|window`.
type ScreenshotConfig struct {
	// Bar button icon.
	Icon string `cfg:"icon"`
	// Output directory for screenshots. Empty uses the XDG Pictures directory.
	OutputDirectory string `cfg:"output-directory"`
	// Saved file name, formatted with `chrono`/`strftime` specifiers.
	FilenameFormat StrftimeFormat `cfg:"filename-format"`
	// Copy the captured image to the clipboard.
	CopyToClipboard bool `cfg:"copy-to-clipboard"`
	// Show a desktop notification after capturing.
	Notify bool `cfg:"notify"`
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
	// Static label text shown beside the icon.
	Label string `cfg:"label"`
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Default captures a region.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click. Default captures the focused output.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click. Default captures the active window.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsScreenshot returns the schema defaults.
func DefaultsScreenshot() ScreenshotConfig {
	return ScreenshotConfig{
		Icon:            "ld-camera-symbolic",
		OutputDirectory: "",
		FilenameFormat:  mustStrftime("Screenshot_%Y-%m-%d_%H-%M-%S.png"),
		CopyToClipboard: true,
		Notify:          true,
		BorderShow:      false,
		BorderColor:     mustColor("accent"),
		IconShow:        true,
		IconColor:       mustColor("auto"),
		IconBgColor:     mustColor("accent"),
		Label:           "",
		LabelShow:       false,
		LabelColor:      mustColor("accent"),
		LabelMaxLength:  0,
		ButtonBgColor:   mustColor("bg-surface-elevated"),
		LeftClick:       ParseClickAction("wayle screenshot region"),
		RightClick:      ParseClickAction("wayle screenshot output"),
		MiddleClick:     ParseClickAction("wayle screenshot window"),
		ScrollUp:        ClickAction{},
		ScrollDown:      ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c ScreenshotConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
