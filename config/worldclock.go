package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// WorldClockConfig is the world-clock module configuration: the format
// carries tz('<Zone>', '<strftime>') calls rendered every second.
type WorldClockConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
	LabelShow bool
	Icon      IconConfig
}

// DefaultsWorldClock returns the schema defaults.
func DefaultsWorldClock() WorldClockConfig {
	return WorldClockConfig{
		Format:    "{{ tz('UTC', '%H:%M %Z') }}",
		LabelShow: true,
		Icon:      DefaultsIcon(true, "ld-globe-symbolic"),
		Button:    DefaultsButton(buttonColors("auto", "yellow", "yellow", "bg-surface-elevated", "yellow"), TokenYellow, true, 0),
	}
}

// applyWorldClock overlays [modules.world-clock].
func applyWorldClock(md toml.MetaData, prim toml.Primitive) (WorldClockConfig, error) {
	cfg := DefaultsWorldClock()
	var doc struct {
		Format *string `toml:"format"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
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
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("world-clock: format is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
