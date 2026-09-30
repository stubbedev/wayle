package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// HyprsunsetConfig is the hyprsunset module configuration.
type HyprsunsetConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow and IconOn/IconOff
	// Show/Color mirror its label-show, icon-show, and icon-color.
	Button       ButtonConfig
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
		Button:       DefaultsButton(buttonColors("auto", "yellow", "yellow", "bg-surface-elevated", "yellow"), TokenYellow, true, 0),
		Temperature:  5000,
		Gamma:        100,
		AutoSchedule: false,
		Click:        DefaultsClick(map[string]string{"left-click": ":toggle"}),
		IconOn:       DefaultsIcon(true, "ld-moon-symbolic"),
		IconOff:      DefaultsIcon(true, "ld-sun-symbolic"),
	}
}

// applyHyprsunset overlays [modules.hyprsunset].
func applyHyprsunset(md toml.MetaData, prim toml.Primitive) (HyprsunsetConfig, error) {
	cfg := DefaultsHyprsunset()
	var doc struct {
		Format       *string  `toml:"format"`
		Temperature  *int     `toml:"temperature"`
		Gamma        *int     `toml:"gamma"`
		AutoSchedule *bool    `toml:"auto-schedule"`
		Latitude     *float64 `toml:"latitude"`
		Longitude    *float64 `toml:"longitude"`
		IconOnName   *string  `toml:"icon-on"`
		IconOffName  *string  `toml:"icon-off"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
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
	// The schema documents these ranges.
	if cfg.Temperature < 1000 || cfg.Temperature > 20000 {
		return cfg, errors.New("hyprsunset: temperature must be 1000..20000")
	}
	if cfg.Latitude < -90 || cfg.Latitude > 90 {
		return cfg, errors.New("hyprsunset: latitude must be -90..90")
	}
	if cfg.Longitude < -180 || cfg.Longitude > 180 {
		return cfg, errors.New("hyprsunset: longitude must be -180..180")
	}
	if cfg.Gamma < 0 || cfg.Gamma > 200 {
		return cfg, errors.New("hyprsunset: gamma must be 0..200")
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("hyprsunset: format is empty")
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.IconOn)
	button.mirrorIcon(&cfg.IconOff)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
