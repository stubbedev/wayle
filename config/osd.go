package config

// OsdConfig is the [osd] section
// (crates/wayle-config/src/schemas/osd/mod.rs).
//
// On-screen display overlay for transient events like volume and brightness.
type OsdConfig struct {
	// Show OSD overlays for volume, brightness, and keyboard toggles.
	Enabled bool `cfg:"enabled"`
	// Screen anchor position.
	Position OsdPosition `cfg:"position"`
	// Horizontal alignment of toast and toggle overlay content. Sliders
	// (volume/brightness) keep their own label+value layout.
	TextAlign OsdTextAlign `cfg:"text-align"`
	// Auto-dismiss delay in milliseconds.
	Duration uint32 `cfg:"duration"`
	// Target monitor: "primary" or a connector name like "DP-1".
	Monitor OsdMonitor `cfg:"monitor"`
	// Margin from screen edges: a multiplier of the default 150px (`1.0` =
	// default) or absolute pixels (e.g. `"150px"`).
	Margin Size `cfg:"margin"`
	// Show a border around the OSD.
	Border bool `cfg:"border"`
	// Layer-shell layer the OSD is placed on.
	//
	// When `general.tearing-mode` is enabled, `overlay` is demoted to `top`
	// to allow fullscreen tearing.
	Layer Layer `cfg:"layer"`
	// Reusable toast presets, each triggerable with `wayle toast --preset <id>`.
	Presets []ToastPreset `cfg:"presets"`
}

// OsdMarginBaseRem is the rem the margin multiplier scales (150px).
const OsdMarginBaseRem = 9.375

// DefaultsOsd returns the schema defaults.
func DefaultsOsd() OsdConfig {
	return OsdConfig{
		Enabled:   true,
		Position:  OsdBottom,
		TextAlign: OsdTextAlignCenter,
		Duration:  2500,
		Margin:    Scale(1),
		Border:    true,
		Layer:     LayerOverlay,
		Presets:   []ToastPreset{},
	}
}

// Preset finds a preset by id.
func (o OsdConfig) Preset(id string) (ToastPreset, bool) {
	for _, p := range o.Presets {
		if p.ID == id {
			return p, true
		}
	}
	return ToastPreset{}, false
}

// OsdPosition is the OSD's screen anchor
// (crates/wayle-config/src/schemas/osd/types.rs).
//
// Screen anchor for the OSD overlay.
type OsdPosition string

// OSD positions.
const (
	// Top-left corner.
	OsdTopLeft OsdPosition = "top-left"
	// Top-center edge.
	OsdTop OsdPosition = "top"
	// Top-right corner.
	OsdTopRight OsdPosition = "top-right"
	// Right-center edge.
	OsdRight OsdPosition = "right"
	// Bottom-right corner.
	OsdBottomRight OsdPosition = "bottom-right"
	// Bottom-center edge.
	OsdBottom OsdPosition = "bottom"
	// Bottom-left corner.
	OsdBottomLeft OsdPosition = "bottom-left"
	// Left-center edge.
	OsdLeft OsdPosition = "left"
)

var _ = registerEnum(OsdTopLeft, OsdTop, OsdTopRight, OsdRight, OsdBottomRight, OsdBottom, OsdBottomLeft, OsdLeft)

// OsdTextAlign is the toast/toggle content alignment.
//
// Horizontal alignment of OSD toast/toggle content.
type OsdTextAlign string

// OSD text alignments.
const (
	// Align content to the start (left in LTR layouts).
	OsdTextAlignStart OsdTextAlign = "start"
	// Center content horizontally.
	OsdTextAlignCenter OsdTextAlign = "center"
	// Align content to the end (right in LTR layouts).
	OsdTextAlignEnd OsdTextAlign = "end"
)

var _ = registerEnum(OsdTextAlignStart, OsdTextAlignCenter, OsdTextAlignEnd)

// ToastPreset is one [[osd.presets]] entry.
//
// A reusable toast preset, triggerable by id with `wayle toast --preset <id>`.
//
// A preset captures a toast's text and icon so it can be fired by name. The
// label/icon can still be overridden per invocation, and runtime-only fields
// (`--percentage`, `--duration`, `--class`) are supplied at invoke time, not
// stored on the preset. Duration always follows the OSD config.
//
// ## Example
//
// ```toml
// [[osd.presets]]
// id = "saved"
// label = "Saved"
// icon = "ld-check-symbolic"
//
// # Fire it: wayle toast --preset saved
// # With a progress bar: wayle toast --preset saved --percentage 80
// ```
type ToastPreset struct {
	// Unique identifier. Trigger with `wayle toast --preset <id>`.
	ID string `cfg:"id,required"`
	// Toast text. An explicit label on the command line overrides this.
	Label *string `cfg:"label,default"`
	// Symbolic icon name shown beside the text.
	Icon *string `cfg:"icon,default"`
}

func (ToastPreset) noStructDefault() {}
