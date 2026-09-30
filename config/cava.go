package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// CavaStyle selects the visualizer rendering.
type CavaStyle string

// Visualizer styles. Wave is not ported to the Go shell yet.
const (
	CavaStyleBars  CavaStyle = "bars"
	CavaStyleWave  CavaStyle = "wave"
	CavaStylePeaks CavaStyle = "peaks"
)

var validCavaStyles = map[CavaStyle]bool{
	CavaStyleBars: true, CavaStyleWave: true, CavaStylePeaks: true,
}

// CavaDirection selects bar growth.
type CavaDirection string

// Growth directions.
const (
	CavaNormal  CavaDirection = "normal"
	CavaReverse CavaDirection = "reverse"
	CavaMirror  CavaDirection = "mirror"
)

var validCavaDirections = map[CavaDirection]bool{
	CavaNormal: true, CavaReverse: true, CavaMirror: true,
}

// CavaConfig is the cava module configuration.
type CavaConfig struct {
	Click ClickConfig
	// Container is the bar_container key set: border-show,
	// border-color, and button-bg-color.
	Container      ContainerConfig
	Bars           int
	BarWidth       int
	BarGap         int
	Color          ColorValue
	Direction      CavaDirection
	Style          CavaStyle
	Framerate      int
	LowCutoff      int
	HighCutoff     int
	NoiseReduction float64
	Monstercat     float64
	InternalPad    Size
	Source         string
}

// DefaultsCava returns the schema defaults for the cava module.
func DefaultsCava() CavaConfig {
	return CavaConfig{
		Click:          DefaultsClick(nil),
		Container:      DefaultsContainer("bg-surface-elevated", "border-accent"),
		Bars:           20,
		BarWidth:       6,
		BarGap:         1,
		Color:          mustColor("accent"),
		Direction:      CavaNormal,
		Style:          CavaStyleBars,
		Framerate:      60,
		LowCutoff:      50,
		HighCutoff:     17000,
		NoiseReduction: 0.65,
		Monstercat:     0.0,
		InternalPad:    Size{Value: 0.5, Unit: SizeMultiplier},
		Source:         "auto",
	}
}

// applyCava overlays the [modules.cava] table onto the defaults.
func applyCava(md toml.MetaData, prim toml.Primitive) (CavaConfig, error) {
	cfg := DefaultsCava()
	var doc struct {
		Bars           *int      `toml:"bars"`
		BarWidth       *int      `toml:"bar-width"`
		BarGap         *int      `toml:"bar-gap"`
		Color          string    `toml:"color"`
		Direction      string    `toml:"direction"`
		Style          string    `toml:"style"`
		Framerate      *int      `toml:"framerate"`
		LowCutoff      *int      `toml:"low-cutoff"`
		HighCutoff     *int      `toml:"high-cutoff"`
		NoiseReduction *float64  `toml:"noise-reduction"`
		Monstercat     *float64  `toml:"monstercat"`
		InternalPad    tomlValue `toml:"internal-padding"`
		Source         *string   `toml:"source"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Bars != nil {
		cfg.Bars = *doc.Bars
	}
	if doc.BarWidth != nil {
		cfg.BarWidth = *doc.BarWidth
	}
	if doc.BarGap != nil {
		cfg.BarGap = *doc.BarGap
	}
	if doc.Color != "" {
		cv, err := ParseColorValue(doc.Color)
		if err != nil {
			return cfg, fmt.Errorf("cava: color: %w", err)
		}
		cfg.Color = cv
	}
	if doc.Direction != "" {
		cfg.Direction = CavaDirection(doc.Direction)
	}
	if doc.Style != "" {
		cfg.Style = CavaStyle(doc.Style)
	}
	if doc.Framerate != nil {
		cfg.Framerate = *doc.Framerate
	}
	if doc.LowCutoff != nil {
		cfg.LowCutoff = *doc.LowCutoff
	}
	if doc.HighCutoff != nil {
		cfg.HighCutoff = *doc.HighCutoff
	}
	if doc.NoiseReduction != nil {
		cfg.NoiseReduction = *doc.NoiseReduction
	}
	if doc.Monstercat != nil {
		cfg.Monstercat = *doc.Monstercat
	}
	if doc.InternalPad.value != nil {
		if err := cfg.InternalPad.unmarshal(doc.InternalPad.value, "internal-padding"); err != nil {
			return cfg, err
		}
	}
	if doc.Source != nil {
		cfg.Source = *doc.Source
	}

	switch {
	case cfg.Bars < 1 || cfg.Bars > 256:
		return cfg, fmt.Errorf("cava: bars %d outside 1-256", cfg.Bars)
	case cfg.BarWidth < 1 || cfg.BarGap < 0:
		return cfg, fmt.Errorf("cava: bar-width %d / bar-gap %d must be positive / non-negative", cfg.BarWidth, cfg.BarGap)
	case cfg.Framerate < 1 || cfg.Framerate > 360:
		return cfg, fmt.Errorf("cava: framerate %d outside 1-360", cfg.Framerate)
	case cfg.NoiseReduction < 0 || cfg.NoiseReduction > 1:
		return cfg, fmt.Errorf("cava: noise-reduction %v outside 0-1", cfg.NoiseReduction)
	case cfg.Monstercat < 0:
		return cfg, fmt.Errorf("cava: monstercat %v must not be negative", cfg.Monstercat)
	case !validCavaStyles[cfg.Style]:
		return cfg, fmt.Errorf("cava: invalid style %q (want bars|wave|peaks)", cfg.Style)
	case !validCavaDirections[cfg.Direction]:
		return cfg, fmt.Errorf("cava: invalid direction %q (want normal|reverse|mirror)", cfg.Direction)
	}
	container, err := applyContainer(md, prim, cfg.Container)
	if err != nil {
		return cfg, err
	}
	cfg.Container = container
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
