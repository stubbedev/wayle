package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// Weather units.
const (
	WeatherMetric   = "metric"
	WeatherImperial = "imperial"
)

// WeatherConfig is the weather module configuration.
type WeatherConfig struct {
	Click     ClickConfig
	Format    string
	Location  string
	Units     string
	RefreshS  int
	LabelShow bool
}

// DefaultsWeather returns the schema defaults.
func DefaultsWeather() WeatherConfig {
	return WeatherConfig{
		Format:    "{{ temp }}{{ temp_unit }}",
		Click:     DefaultsClick(map[string]string{"left-click": "dropdown:weather"}),
		Location:  "San Francisco",
		Units:     WeatherMetric,
		RefreshS:  1800,
		LabelShow: true,
	}
}

// applyWeather overlays [modules.weather].
func applyWeather(md toml.MetaData, prim toml.Primitive) (WeatherConfig, error) {
	cfg := DefaultsWeather()
	var doc struct {
		Format    *string `toml:"format"`
		Location  *string `toml:"location"`
		Units     *string `toml:"units"`
		RefreshS  *int    `toml:"refresh-interval-seconds"`
		LabelShow *bool   `toml:"label-show"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.Location != nil {
		cfg.Location = *doc.Location
	}
	if doc.Units != nil {
		cfg.Units = *doc.Units
	}
	if doc.RefreshS != nil {
		cfg.RefreshS = *doc.RefreshS
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if cfg.Units != WeatherMetric && cfg.Units != WeatherImperial {
		return cfg, errors.New("weather: units must be metric or imperial")
	}
	if cfg.RefreshS < 0 {
		return cfg, errors.New("weather: refresh-interval-seconds is negative")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
