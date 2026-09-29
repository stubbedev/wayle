package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// HyprsunsetConfig is the hyprsunset module configuration.
type HyprsunsetConfig struct {
	Click        ClickConfig
	Format       string
	LabelShow    bool
	Temperature  int
	Gamma        int
	IconOn       IconConfig
	IconOff      IconConfig
	AutoSchedule bool
	Latitude     float64
	Longitude    float64
}

// DefaultsHyprsunset returns the schema defaults.
func DefaultsHyprsunset() HyprsunsetConfig {
	return HyprsunsetConfig{
		Format:       "{{ status }}",
		LabelShow:    true,
		Temperature:  5000,
		Gamma:        100,
		AutoSchedule: false,
		Click:        DefaultsClick(map[string]string{"left-click": ":toggle"}),
		IconOn:       DefaultsIcon(true, "ld-sunset-symbolic"),
		IconOff:      DefaultsIcon(true, "ld-sun-symbolic"),
	}
}

// applyHyprsunset overlays [modules.hyprsunset].
func applyHyprsunset(md toml.MetaData, prim toml.Primitive) (HyprsunsetConfig, error) {
	cfg := DefaultsHyprsunset()
	var doc struct {
		Format       *string     `toml:"format"`
		LabelShow    *bool       `toml:"label-show"`
		Temperature  *int        `toml:"temperature"`
		Gamma        *int        `toml:"gamma"`
		AutoSchedule *bool       `toml:"auto-schedule"`
		Latitude     *float64    `toml:"latitude"`
		Longitude    *float64    `toml:"longitude"`
		IconShow     *bool       `toml:"icon-show"`
		IconOnName   *string     `toml:"icon-on"`
		IconOffName  *string     `toml:"icon-off"`
		IconColor    *ColorValue `toml:"icon-color"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.Temperature != nil {
		cfg.Temperature = *doc.Temperature
	}
	if doc.Gamma != nil {
		cfg.Gamma = *doc.Gamma
	}
	if doc.AutoSchedule != nil {
		cfg.AutoSchedule = *doc.AutoSchedule
	}
	if doc.Latitude != nil {
		cfg.Latitude = *doc.Latitude
	}
	if doc.Longitude != nil {
		cfg.Longitude = *doc.Longitude
	}
	if doc.IconOnName != nil {
		cfg.IconOn.Name = *doc.IconOnName
	}
	if doc.IconOffName != nil {
		cfg.IconOff.Name = *doc.IconOffName
	}
	if doc.IconShow != nil {
		cfg.IconOn.Show = *doc.IconShow
		cfg.IconOff.Show = *doc.IconShow
	}
	if doc.IconColor != nil {
		cfg.IconOn.Color = *doc.IconColor
		cfg.IconOff.Color = *doc.IconColor
	}
	if cfg.Temperature < 1000 || cfg.Temperature > 10000 {
		return cfg, errors.New("hyprsunset: temperature must be 1000..10000")
	}
	if cfg.Gamma < 0 || cfg.Gamma > 200 {
		return cfg, errors.New("hyprsunset: gamma must be 0..200")
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("hyprsunset: format is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
