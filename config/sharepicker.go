package config

// The rem bases the share picker's Size multipliers resolve against
// (share_picker/mod.rs): Scale(1) is the default pixel size.
const (
	SharePickerWidthBaseRem          = 62.5
	SharePickerHeightBaseRem         = 31.25
	SharePickerWidgetBaseRem         = 9.375
	SharePickerWindowsSpacingBaseRem = 0.75
	SharePickerOutputsSpacingBaseRem = 0.375
)

// SharePickerConfig is the [share-picker] section
// (crates/wayle-config/src/schemas/share_picker/mod.rs).
//
// Screen-share picker shown by xdg-desktop-portal when an app requests a
// window, output, or region to capture.
type SharePickerConfig struct {
	// Page selected when the picker opens.
	DefaultPage SharePickerPage `cfg:"default-page"`
	// Hide the "allow a restore token" checkbox.
	HideTokenRestore bool `cfg:"hide-token-restore"`
	// Picker window width: a multiplier of the default 1000px (`1.0` = default)
	// or absolute pixels (e.g. `"1200px"`).
	Width Size `cfg:"width"`
	// Picker window height: a multiplier of the default 500px (`1.0` = default)
	// or absolute pixels.
	Height Size `cfg:"height"`
	// Downscale every captured frame to at most this height in pixels.
	ResizeSize uint32 `cfg:"resize-size"`
	// Height of each card's preview image: a multiplier of the default 150px
	// (`1.0` = default) or absolute pixels.
	WidgetSize Size `cfg:"widget-size"`
	// Spacing between window cards: a multiplier of the default 12px
	// (`1.0` = default) or absolute pixels.
	WindowsSpacing Size `cfg:"windows-spacing"`
	// Minimum window cards per row.
	WindowsMinPerRow uint32 `cfg:"windows-min-per-row"`
	// Maximum window cards per row.
	WindowsMaxPerRow uint32 `cfg:"windows-max-per-row"`
	// Spacing between output cards (applied per side): a multiplier of the
	// default 6px (`1.0` = default) or absolute pixels.
	OutputsSpacing Size `cfg:"outputs-spacing"`
	// Show the output name label under each output card.
	OutputsShowLabel bool `cfg:"outputs-show-label"`
	// Scale output cards by their fractional scale.
	OutputsRespectScaling bool `cfg:"outputs-respect-scaling"`
}

// DefaultsSharePicker returns the schema defaults.
func DefaultsSharePicker() SharePickerConfig {
	return SharePickerConfig{
		DefaultPage:           SharePickerWindows,
		Width:                 Scale(1),
		Height:                Scale(1),
		ResizeSize:            640,
		WidgetSize:            Scale(1),
		WindowsSpacing:        Scale(1),
		WindowsMinPerRow:      3,
		WindowsMaxPerRow:      4,
		OutputsSpacing:        Scale(1),
		OutputsRespectScaling: true,
	}
}

// SharePickerPage is the page the share picker opens on
// (share_picker/types.rs).
//
// Page shown when the screen-share picker opens.
type SharePickerPage string

// The picker pages, in notebook order.
const (
	// Per-window previews.
	SharePickerWindows SharePickerPage = "windows"
	// Per-output (monitor) previews.
	SharePickerOutputs SharePickerPage = "outputs"
	// Region selection via an external tool (e.g. `slurp`).
	SharePickerRegion SharePickerPage = "region"
)

var _ = registerEnum(SharePickerWindows, SharePickerOutputs, SharePickerRegion)
