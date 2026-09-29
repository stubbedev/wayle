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

// RoundingLevel is the global corner-rounding preference.
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

// SizeUnit distinguishes a scale multiplier from absolute pixels.
type SizeUnit int

// Size units. A multiplier scales with the configured font size; a
// pixel size is absolute.
const (
	SizeMultiplier SizeUnit = iota
	SizePixels
)

// Size is a wayle config size: a bare number is a font-size multiplier,
// a string like "4px" is absolute pixels.
type Size struct {
	Value float64
	Unit  SizeUnit
}

// Px resolves the size to pixels against basePx, the font size the
// multipliers scale with.
func (s Size) Px(basePx float64) float64 {
	if s.Unit == SizePixels {
		return s.Value
	}
	return s.Value * basePx
}

func (s *Size) unmarshal(value any, key string) error {
	switch v := value.(type) {
	case float64:
		if v < 0 {
			return fmt.Errorf("config: bar %s: negative size %v", key, v)
		}
		s.Value, s.Unit = v, SizeMultiplier
		return nil
	case string:
		if !strings.HasSuffix(v, "px") {
			return fmt.Errorf("config: bar %s: invalid size %q (want a number or \"Npx\")", key, v)
		}
		px, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		if err != nil || px < 0 {
			return fmt.Errorf("config: bar %s: invalid size %q (want a number or \"Npx\")", key, v)
		}
		s.Value, s.Unit = px, SizePixels
		return nil
	default:
		return fmt.Errorf("config: bar %s: invalid size %v (want a number or \"Npx\")", key, value)
	}
}
