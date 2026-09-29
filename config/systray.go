package config

import (
	"github.com/BurntSushi/toml"
)

// SystrayConfig is the tray module configuration: per-item layout
// knobs and the icon overrides.
type SystrayConfig struct {
	Click     ClickConfig
	IconSize  int
	IconScale float64
	ItemGap   float64
	Blacklist []string
	// Overrides map an item's Id to a replacement icon name.
	Overrides map[string]string
}

// DefaultsSystray returns the schema defaults.
func DefaultsSystray() SystrayConfig {
	return SystrayConfig{
		IconSize:  16,
		IconScale: 1.0,
		ItemGap:   0.25,
		Blacklist: []string{},
		Overrides: map[string]string{},
	}
}

// applySystray overlays [modules.systray].
func applySystray(md toml.MetaData, prim toml.Primitive) (SystrayConfig, error) {
	cfg := DefaultsSystray()
	var doc struct {
		IconSize  *int               `toml:"icon-size"`
		IconScale *float64           `toml:"icon-scale"`
		ItemGap   *float64           `toml:"item-gap"`
		Blacklist *[]string          `toml:"blacklist"`
		Overrides *map[string]string `toml:"overrides"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.IconSize != nil {
		cfg.IconSize = *doc.IconSize
	}
	if doc.IconScale != nil {
		cfg.IconScale = *doc.IconScale
	}
	if doc.ItemGap != nil {
		cfg.ItemGap = *doc.ItemGap
	}
	if doc.Blacklist != nil {
		cfg.Blacklist = *doc.Blacklist
	}
	if doc.Overrides != nil {
		cfg.Overrides = *doc.Overrides
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
