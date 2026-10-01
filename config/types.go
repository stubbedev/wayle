package config

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Location is the bar's screen edge.
//
// Bar position on screen.
type Location string

// Bar locations.
const (
	// Top edge of the screen.
	LocationTop Location = "top"
	// Bottom edge of the screen.
	LocationBottom Location = "bottom"
	// Left edge of the screen.
	LocationLeft Location = "left"
	// Right edge of the screen.
	LocationRight Location = "right"
)

var _ = registerEnum(LocationTop, LocationBottom, LocationLeft, LocationRight)

// Layer is the layer-shell layer a surface is placed on.
//
// Layer-shell layer a window is placed on, from furthest back to furthest front.
type Layer string

// Layers.
const (
	// Below everything else, used for wallpapers and ambient surfaces.
	LayerBackground Layer = "background"
	// Behind regular application windows.
	LayerBottom Layer = "bottom"
	// Above regular application windows.
	LayerTop Layer = "top"
	// Above everything, including fullscreen application windows.
	LayerOverlay Layer = "overlay"
)

var _ = registerEnum(LayerBackground, LayerBottom, LayerTop, LayerOverlay)

// RoundingLevel is the corner-rounding preference; the radius each
// level resolves to lives in styling.RoundingRadiusPx.
//
// Global rounding preference for UI components.
type RoundingLevel string

// Rounding levels.
const (
	// Sharp corners (no rounding).
	RoundingNone RoundingLevel = "none"
	// Subtle rounding.
	RoundingSm RoundingLevel = "sm"
	// Moderate rounding (default).
	RoundingMd RoundingLevel = "md"
	// Pronounced rounding.
	RoundingLg RoundingLevel = "lg"
	// Pill shape (fully rounded ends).
	RoundingFull RoundingLevel = "full"
)

var _ = registerEnum(RoundingNone, RoundingSm, RoundingMd, RoundingLg, RoundingFull)

// BorderLocation is the placement of a border: one edge, all edges,
// or none.
//
// Border placement for bar buttons.
type BorderLocation string

// Border locations.
const (
	// No border.
	BorderNone BorderLocation = "none"
	// Border on top edge only.
	BorderTop BorderLocation = "top"
	// Border on bottom edge only.
	BorderBottom BorderLocation = "bottom"
	// Border on left edge only.
	BorderLeft BorderLocation = "left"
	// Border on right edge only.
	BorderRight BorderLocation = "right"
	// Border on all edges.
	BorderAll BorderLocation = "all"
)

var _ = registerEnum(BorderNone, BorderTop, BorderBottom, BorderLeft, BorderRight, BorderAll)

// SizeUnit distinguishes a scale multiplier from absolute pixels.
type SizeUnit int

// Size units. A multiplier scales with the rem base and the surface's
// scale factor; a pixel size is absolute.
const (
	SizeMultiplier SizeUnit = iota
	SizePixels
)

// sizePxMax is the largest absolute size (Size::px clamps to it).
const sizePxMax = 10_000

// Size is a wayle config size: a bare number is a multiplier, a string
// like "4px" is absolute pixels
// (crates/wayle-config/src/schemas/styling/types/validated/size.rs).
// Like the Rust type it never fails to load: a negative multiplier
// clamps to 0, pixels clamp to 0-10000, and an unparseable string
// warns and falls back to the 1.0 multiplier.
type Size struct {
	Value float32
	Unit  SizeUnit
}

// Scale builds a multiplier size, clamped to non-negative.
func Scale(v float32) Size { return Size{Value: max(v, 0), Unit: SizeMultiplier} }

// Px builds an absolute size, clamped to 0-10000.
func Px(v float32) Size { return Size{Value: min(max(v, 0), sizePxMax), Unit: SizePixels} }

// ResolvePx converts the size to pixels: multipliers scale with
// remBase and the surface scale factor, pixel values are taken
// literally ignoring both. This is the Rust Size::resolve_px.
func (s Size) ResolvePx(remBase, scale float64) float64 {
	if s.Unit == SizePixels {
		return float64(s.Value)
	}
	return float64(s.Value) * remBase * scale
}

// IsZero reports whether the size carries no value.
func (s Size) IsZero() bool { return s.Value == 0 }

// String is the config form: "1.5" or "24px".
func (s Size) String() string {
	v := strconv.FormatFloat(float64(s.Value), 'f', -1, 32)
	if s.Unit == SizePixels {
		return v + "px"
	}
	return v
}

// ParseSize is Size::parse: "Npx" is pixels, a bare number a
// multiplier; anything else is not a size.
func ParseSize(raw string) (Size, bool) {
	trimmed := strings.TrimSpace(raw)
	if px, ok := strings.CutSuffix(trimmed, "px"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(px), 32)
		if err != nil {
			return Size{}, false
		}
		return Px(float32(v)), true
	}
	v, err := strconv.ParseFloat(trimmed, 32)
	if err != nil {
		return Size{}, false
	}
	return Scale(float32(v)), true
}

// UnmarshalConfig implements Unmarshaler.
func (s *Size) UnmarshalConfig(v any) error {
	switch t := v.(type) {
	case float64:
		*s = Scale(float32(t))
	case int64:
		*s = Scale(float32(t))
	case string:
		parsed, ok := ParseSize(t)
		if !ok {
			log.Printf("config: invalid size %q, falling back to scale 1.0", t)
			parsed = Scale(1)
		}
		*s = parsed
	default:
		return errUntagged("RawSize")
	}
	return nil
}

// MarshalConfig implements Marshaler: a multiplier is a number, pixels
// a string.
func (s Size) MarshalConfig() any {
	if s.Unit == SizePixels {
		return s.String()
	}
	return s.Value
}

func (Size) configSchema(*schemaGen) Schema {
	return Schema{
		"description": "Size as a scale multiplier (number) or absolute pixels (e.g. \"24px\")",
		"anyOf": []any{
			Schema{"type": "number", "minimum": 0.0},
			Schema{"type": "string", "pattern": `^[0-9]+(\.[0-9]+)?px$`},
		},
	}
}

// errUntagged is serde's failure for an untagged enum no variant took.
func errUntagged(name string) error {
	return fmt.Errorf("data did not match any variant of untagged enum %s", name)
}

// TimeFormat is ported from crates/wayle-config/src/schemas/modules/types.rs.
//
// Time display format.
type TimeFormat string

// TimeFormat values.
const (
	// 12-hour format with AM/PM (e.g., "6:30 AM").
	TimeFormat12h TimeFormat = "12h"
	// 24-hour format (e.g., "06:30").
	TimeFormat24h TimeFormat = "24h"
)

var _ = registerEnum(TimeFormat12h, TimeFormat24h)

// IsVertical reports whether a bar at the location runs top to bottom.
func (l Location) IsVertical() bool { return l == LocationLeft || l == LocationRight }
