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
	Click    ClickConfig
	Format   string
	Location string
	Units    string
	RefreshS int
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
	LabelShow bool
	Icon      IconConfig
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
		Icon:      DefaultsIcon(true, "ld-sun-symbolic"),
		Button:    DefaultsButton(buttonColors("auto", "accent", "accent", "bg-surface-elevated", "border-accent"), TokenAccent, true, 0),
	}
}

// applyWeather overlays [modules.weather].
func applyWeather(md toml.MetaData, prim toml.Primitive) (WeatherConfig, error) {
	cfg := DefaultsWeather()
	var doc struct {
		Format   *string `toml:"format"`
		Location *string `toml:"location"`
		Units    *string `toml:"units"`
		RefreshS *int    `toml:"refresh-interval-seconds"`
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
	icon, err := applyIcon(md, prim, cfg.Icon)
	if err != nil {
		return cfg, err
	}
	cfg.Icon = icon
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.Icon)
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
