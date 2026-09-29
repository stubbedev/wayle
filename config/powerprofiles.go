package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// PowerProfilesConfig is the power-profiles module configuration: the
// active profile rendered per the format, with per-profile icons and
// colors.
type PowerProfilesConfig struct {
	Click     ClickConfig
	Format    string
	LabelShow bool
	Icons     map[string]IconConfig
	Colors    map[string]ColorValue
}

// DefaultsPowerProfiles returns the schema defaults.
func DefaultsPowerProfiles() PowerProfilesConfig {
	return PowerProfilesConfig{
		Format:    "{{ profile }}",
		LabelShow: false,
		Click:     DefaultsClick(map[string]string{"left-click": ":cycle"}),
		Icons: map[string]IconConfig{
			ProfileBalanced:    DefaultsIcon(true, "ld-scale-symbolic"),
			ProfilePerformance: DefaultsIcon(true, "ld-rocket-symbolic"),
			ProfilePowerSaver:  DefaultsIcon(true, "ld-leaf-symbolic"),
		},
		Colors: map[string]ColorValue{},
	}
}

// applyPowerProfiles overlays [modules.power-profiles].
func applyPowerProfiles(md toml.MetaData, prim toml.Primitive) (PowerProfilesConfig, error) {
	cfg := DefaultsPowerProfiles()
	var doc struct {
		Format        *string     `toml:"format"`
		LabelShow     *bool       `toml:"label-show"`
		IconShow      *bool       `toml:"icon-show"`
		IconBalanced  *string     `toml:"icon-balanced"`
		IconPerf      *string     `toml:"icon-performance"`
		IconSaver     *string     `toml:"icon-power-saver"`
		ColorBalanced *ColorValue `toml:"color-balanced"`
		ColorPerf     *ColorValue `toml:"color-performance"`
		ColorSaver    *ColorValue `toml:"color-power-saver"`
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
	if doc.IconShow != nil {
		for name, icon := range cfg.Icons {
			icon.Show = *doc.IconShow
			cfg.Icons[name] = icon
		}
	}
	for name, key := range map[string]*string{
		ProfileBalanced:    doc.IconBalanced,
		ProfilePerformance: doc.IconPerf,
		ProfilePowerSaver:  doc.IconSaver,
	} {
		if key == nil {
			continue
		}
		icon := cfg.Icons[name]
		icon.Name = *key
		cfg.Icons[name] = icon
	}
	for name, color := range map[string]*ColorValue{
		ProfileBalanced:    doc.ColorBalanced,
		ProfilePerformance: doc.ColorPerf,
		ProfilePowerSaver:  doc.ColorSaver,
	} {
		if color == nil {
			continue
		}
		cfg.Colors[name] = *color
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("power-profiles: format is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}

// Profile names as the daemon spells them.
const (
	ProfileBalanced    = "balanced"
	ProfilePerformance = "performance"
	ProfilePowerSaver  = "power-saver"
)
