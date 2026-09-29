package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// Bar is the bar chrome: per-monitor layout, spacing, and placement.
// The fields carry the subset of BarConfig the Go shell consumes; the
// styling keys follow with the wayle-styling port.
type Bar struct {
	Location          Location
	Layer             Layer
	Exclusive         bool
	ModuleGap         Size
	Padding           Size
	PaddingEnds       Size
	Rounding          RoundingLevel
	BackgroundOpacity int
	Scale             float64
	Layout            []BarLayout
}

// BarLayout is the bar layout for one monitor. Monitor is a connector
// name ("DP-1") or "*" for every monitor without an exact entry;
// Extends inherits the unset sections of another layout by its monitor
// value.
type BarLayout struct {
	Monitor string
	Extends string
	Show    bool
	Left    []BarItem
	Center  []BarItem
	Right   []BarItem
}

// ClockConfig is the clock module configuration; the format is strftime.
type ClockConfig struct {
	Format string
}

// GeneralConfig is the cross-shell general section.
type GeneralConfig struct {
	FontSans string
	FontMono string
}

// Config is the loaded user configuration.
type Config struct {
	Bar     Bar
	Clock   ClockConfig
	General GeneralConfig
}

// Defaults returns the schema defaults for every modeled section.
func Defaults() *Config {
	return &Config{
		Bar: Bar{
			Location:          LocationTop,
			Layer:             LayerTop,
			Exclusive:         true,
			ModuleGap:         Size{Value: 0.5, Unit: SizeMultiplier},
			Padding:           Size{Value: 0.35, Unit: SizeMultiplier},
			PaddingEnds:       Size{Value: 0.5, Unit: SizeMultiplier},
			Rounding:          RoundingNone,
			BackgroundOpacity: 100,
			Scale:             1.0,
		},
		Clock: ClockConfig{
			Format: "%a %b %d %I:%M %p",
		},
		General: GeneralConfig{
			FontSans: "Inter",
			FontMono: "JetBrains Mono",
		},
	}
}

// fileDoc mirrors the top-level TOML document. Sections the Go shell
// does not model yet (osd, launcher, modules beyond the clock, ...) are
// ignored here exactly as serde ignores unknown fields in Rust.
type fileDoc struct {
	Bar     *toml.Primitive `toml:"bar"`
	Modules *struct {
		Clock *toml.Primitive `toml:"clock"`
	} `toml:"modules"`
	General *toml.Primitive `toml:"general"`
}

// barDoc mirrors the [bar] table; layout is handled by BarItem's
// UnmarshalTOML through the slice element decode.
type barDoc struct {
	Location          string      `toml:"location"`
	Layer             string      `toml:"layer"`
	Exclusive         bool        `toml:"exclusive"`
	ModuleGap         tomlValue   `toml:"module-gap"`
	Padding           tomlValue   `toml:"padding"`
	PaddingEnds       tomlValue   `toml:"padding-ends"`
	Rounding          string      `toml:"rounding"`
	BackgroundOpacity *int        `toml:"background-opacity"`
	Scale             *float64    `toml:"scale"`
	Layout            []BarLayout `toml:"layout"`
}

type generalDoc struct {
	FontSans string `toml:"font-sans"`
	FontMono string `toml:"font-mono"`
}

// tomlValue defers a leaf's decode so Size can accept number|string.
type tomlValue struct {
	value any
}

func (t *tomlValue) UnmarshalTOML(value any) error {
	t.value = value
	return nil
}

// applyTOML overlays a config.toml document onto the defaults. Every
// failure is a load error; the receiver keeps the pre-failure state.
func (c *Config) applyTOML(data []byte) error {
	var doc fileDoc
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return err
	}
	if doc.Bar != nil {
		bar := barDoc{}
		if err := md.PrimitiveDecode(*doc.Bar, &bar); err != nil {
			return err
		}
		applied, err := bar.toBar()
		if err != nil {
			return err
		}
		c.Bar = applied
	}
	if doc.Modules != nil && doc.Modules.Clock != nil {
		clock := struct {
			Format string `toml:"format"`
		}{}
		if err := md.PrimitiveDecode(*doc.Modules.Clock, &clock); err != nil {
			return err
		}
		c.Clock.Format = clock.Format
	}
	if doc.General != nil {
		general := generalDoc{}
		if err := md.PrimitiveDecode(*doc.General, &general); err != nil {
			return err
		}
		c.General = GeneralConfig(general)
	}
	return nil
}

func (b barDoc) toBar() (Bar, error) {
	bar := Defaults().Bar
	if b.Location != "" {
		bar.Location = Location(b.Location)
	}
	if b.Layer != "" {
		bar.Layer = Layer(b.Layer)
	}
	bar.Exclusive = b.Exclusive
	if b.ModuleGap.value != nil {
		if err := bar.ModuleGap.unmarshal(b.ModuleGap.value, "module-gap"); err != nil {
			return Bar{}, err
		}
	}
	if b.Padding.value != nil {
		if err := bar.Padding.unmarshal(b.Padding.value, "padding"); err != nil {
			return Bar{}, err
		}
	}
	if b.PaddingEnds.value != nil {
		if err := bar.PaddingEnds.unmarshal(b.PaddingEnds.value, "padding-ends"); err != nil {
			return Bar{}, err
		}
	}
	if b.Rounding != "" {
		bar.Rounding = RoundingLevel(b.Rounding)
	}
	if b.BackgroundOpacity != nil {
		bar.BackgroundOpacity = *b.BackgroundOpacity
	}
	if b.Scale != nil {
		bar.Scale = *b.Scale
	}
	bar.Layout = b.Layout

	if !validLocations[bar.Location] {
		return Bar{}, fmt.Errorf("bar: invalid location %q (want top|bottom|left|right)", bar.Location)
	}
	if !validLayers[bar.Layer] {
		return Bar{}, fmt.Errorf("bar: invalid layer %q (want background|bottom|top|overlay)", bar.Layer)
	}
	if !validRounding[bar.Rounding] {
		return Bar{}, fmt.Errorf("bar: invalid rounding %q (want none|sm|md|lg|full)", bar.Rounding)
	}
	if bar.BackgroundOpacity < 0 || bar.BackgroundOpacity > 100 {
		return Bar{}, fmt.Errorf("bar: background-opacity %d outside 0-100", bar.BackgroundOpacity)
	}
	if bar.Scale < 0.25 || bar.Scale > 3.0 {
		return Bar{}, fmt.Errorf("bar: scale %v outside 0.25-3.0", bar.Scale)
	}
	for i := range bar.Layout {
		if err := validateLayout(&bar.Layout[i]); err != nil {
			return Bar{}, err
		}
	}
	return bar, nil
}

func validateLayout(l *BarLayout) error {
	if l.Monitor == "" {
		return errors.New("bar: layout entry missing monitor")
	}
	if l.Extends == l.Monitor {
		return fmt.Errorf("bar: layout %q extends itself", l.Monitor)
	}
	return nil
}
