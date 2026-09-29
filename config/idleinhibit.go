package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// IdleInhibitConfig is the idle-inhibit module configuration.
type IdleInhibitConfig struct {
	Click           ClickConfig
	Format          string
	LabelShow       bool
	StartupDuration uint32
	Icons           map[string]IconConfig
	Colors          map[string]ColorValue
}

// DefaultsIdleInhibit returns the schema defaults.
func DefaultsIdleInhibit() IdleInhibitConfig {
	return IdleInhibitConfig{
		Format:          "{{ state }}",
		LabelShow:       true,
		StartupDuration: 60,
		Icons: map[string]IconConfig{
			IdleInhibitActive:   DefaultsIcon(true, "tb-coffee-symbolic"),
			IdleInhibitInactive: DefaultsIcon(true, "tb-coffee-off-symbolic"),
		},
		Colors: map[string]ColorValue{},
	}
}

// The icon keys.
const (
	IdleInhibitActive   = "active"
	IdleInhibitInactive = "inactive"
)

// applyIdleInhibit overlays [modules.idle-inhibit].
func applyIdleInhibit(md toml.MetaData, prim toml.Primitive) (IdleInhibitConfig, error) {
	cfg := DefaultsIdleInhibit()
	var doc struct {
		Format          *string     `toml:"format"`
		LabelShow       *bool       `toml:"label-show"`
		StartupDuration *uint32     `toml:"startup-duration"`
		IconShow        *bool       `toml:"icon-show"`
		IconActive      *string     `toml:"icon-active"`
		IconInactive    *string     `toml:"icon-inactive"`
		ColorActive     *ColorValue `toml:"color-active"`
		ColorInactive   *ColorValue `toml:"color-inactive"`
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
	if doc.StartupDuration != nil {
		cfg.StartupDuration = *doc.StartupDuration
	}
	for name, key := range map[string]*string{
		IdleInhibitActive:   doc.IconActive,
		IdleInhibitInactive: doc.IconInactive,
	} {
		if key == nil {
			continue
		}
		icon := cfg.Icons[name]
		icon.Name = *key
		cfg.Icons[name] = icon
	}
	if doc.IconShow != nil {
		for name, icon := range cfg.Icons {
			icon.Show = *doc.IconShow
			cfg.Icons[name] = icon
		}
	}
	for name, color := range map[string]*ColorValue{
		IdleInhibitActive:   doc.ColorActive,
		IdleInhibitInactive: doc.ColorInactive,
	} {
		if color == nil {
			continue
		}
		cfg.Colors[name] = *color
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("idle-inhibit: format is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
