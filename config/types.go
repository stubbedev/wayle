package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Location is the screen edge a bar docks to.
type Location string

// Bar locations.
const (
	LocationTop    Location = "top"
	LocationBottom Location = "bottom"
	LocationLeft   Location = "left"
	LocationRight  Location = "right"
)

var validLocations = map[Location]bool{
	LocationTop: true, LocationBottom: true,
	LocationLeft: true, LocationRight: true,
}

// Layer is the layer-shell layer a surface is placed on, furthest back
// to furthest front.
type Layer string

// Layers.
const (
	LayerBackground Layer = "background"
	LayerBottom     Layer = "bottom"
	LayerTop        Layer = "top"
	LayerOverlay    Layer = "overlay"
)

var validLayers = map[Layer]bool{
	LayerBackground: true, LayerBottom: true,
	LayerTop: true, LayerOverlay: true,
}

// RoundingLevel is the global corner-rounding preference. The radius
// each level resolves to lives in styling.RoundingRadiusPx.
type RoundingLevel string

// Rounding levels.
const (
	RoundingNone RoundingLevel = "none"
	RoundingSm   RoundingLevel = "sm"
	RoundingMd   RoundingLevel = "md"
	RoundingLg   RoundingLevel = "lg"
	RoundingFull RoundingLevel = "full"
)

var validRounding = map[RoundingLevel]bool{
	RoundingNone: true, RoundingSm: true, RoundingMd: true,
	RoundingLg: true, RoundingFull: true,
}

// BorderLocation is the placement of a border: one edge, all edges, or
// none.
type BorderLocation string

// Border locations.
const (
	BorderNone   BorderLocation = "none"
	BorderTop    BorderLocation = "top"
	BorderBottom BorderLocation = "bottom"
	BorderLeft   BorderLocation = "left"
	BorderRight  BorderLocation = "right"
	BorderAll    BorderLocation = "all"
)

var validBorderLocations = map[BorderLocation]bool{
	BorderNone: true, BorderTop: true, BorderBottom: true,
	BorderLeft: true, BorderRight: true, BorderAll: true,
}

// SizeUnit distinguishes a scale multiplier from absolute pixels.
type SizeUnit int

// Size units. A multiplier scales with the rem base and the surface's
// scale factor; a pixel size is absolute.
const (
	SizeMultiplier SizeUnit = iota
	SizePixels
)

// Size is a wayle config size: a bare number is a multiplier, a string
// like "4px" is absolute pixels.
type Size struct {
	Value float64
	Unit  SizeUnit
}

// ResolvePx converts the size to pixels: multipliers scale with
// remBase and the surface scale factor, pixel values are taken
// literally ignoring both. This is the Rust Size::resolve_px.
func (s Size) ResolvePx(remBase, scale float64) float64 {
	if s.Unit == SizePixels {
		return s.Value
	}
	return s.Value * remBase * scale
}

// IsZero reports whether the size carries no value.
func (s Size) IsZero() bool { return s.Value == 0 }

func (s *Size) unmarshal(value any, key string) error {
	// TOML integers are numbers too (serde's f32 accepts them).
	if i, ok := value.(int64); ok {
		value = float64(i)
	}
	switch v := value.(type) {
	case float64:
		if v < 0 {
			return fmt.Errorf("config: bar %s: negative size %v", key, v)
		}
		s.Value, s.Unit = v, SizeMultiplier
		return nil
	case string:
		// A bare number string is a scale, as Size::parse reads it.
		if scale, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && scale >= 0 {
			s.Value, s.Unit = scale, SizeMultiplier
			return nil
		}
		v = strings.TrimSpace(v)
		if !strings.HasSuffix(v, "px") {
			return fmt.Errorf("config: bar %s: invalid size %q (want a number or \"Npx\")", key, v)
		}
		px, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, "px")), 64)
		if err != nil || px < 0 {
			return fmt.Errorf("config: bar %s: invalid size %q (want a number or \"Npx\")", key, v)
		}
		s.Value, s.Unit = px, SizePixels
		return nil
	default:
		return fmt.Errorf("config: bar %s: invalid size %v (want a number or \"Npx\")", key, value)
	}
}
