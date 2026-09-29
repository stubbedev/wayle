package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// WorldClockConfig is the world-clock module configuration: the format
// carries tz('<Zone>', '<strftime>') calls rendered every second.
type WorldClockConfig struct {
	Click     ClickConfig
	Format    string
	LabelShow bool
}

// DefaultsWorldClock returns the schema defaults.
func DefaultsWorldClock() WorldClockConfig {
	return WorldClockConfig{
		Format:    "{{ tz('UTC', '%H:%M %Z') }}",
		LabelShow: true,
	}
}

// applyWorldClock overlays [modules.world-clock].
func applyWorldClock(md toml.MetaData, prim toml.Primitive) (WorldClockConfig, error) {
	cfg := DefaultsWorldClock()
	var doc struct {
		Format    *string `toml:"format"`
		LabelShow *bool   `toml:"label-show"`
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
