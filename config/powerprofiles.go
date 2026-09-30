package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// PowerProfilesConfig is the power-profiles module configuration: the
// active profile rendered per the format, with per-profile icons and
// colors.
type PowerProfilesConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow and the Show of every
	// Icons entry mirror its label-show and icon-show.
	Button    ButtonConfig
	LabelShow bool
	Icons     map[string]IconConfig
	Colors    map[string]ColorValue
}

// DefaultsPowerProfiles returns the schema defaults.
func DefaultsPowerProfiles() PowerProfilesConfig {
	return PowerProfilesConfig{
		Format:    "{{ profile }}",
		LabelShow: false,
		Button:    DefaultsButton(buttonColors("auto", "auto", "bg-surface-elevated", "bg-surface-elevated", "blue"), TokenBlue, false, 0),
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
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIconShow(cfg.Icons)
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
