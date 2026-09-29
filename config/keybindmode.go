package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// KeybindModeConfig is the keybind-mode module configuration: the
// Hyprland submap rendered as the active keybind layer.
type KeybindModeConfig struct {
	Click     ClickConfig
	Format    string
	LabelShow bool
	AutoHide  bool
	Icon      IconConfig
}

// DefaultsKeybindMode returns the schema defaults.
func DefaultsKeybindMode() KeybindModeConfig {
	return KeybindModeConfig{
		Format:    "{{ mode }}",
		LabelShow: true,
		AutoHide:  false,
		Icon:      DefaultsIcon(true, "ld-layers-symbolic"),
	}
}

// applyKeybindMode overlays [modules.keybind-mode].
func applyKeybindMode(md toml.MetaData, prim toml.Primitive) (KeybindModeConfig, error) {
	cfg := DefaultsKeybindMode()
	var doc struct {
		Format    *string     `toml:"format"`
		LabelShow *bool       `toml:"label-show"`
		AutoHide  *bool       `toml:"auto-hide"`
		IconShow  *bool       `toml:"icon-show"`
		IconName  *string     `toml:"icon-name"`
		IconColor *ColorValue `toml:"icon-color"`
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
	if doc.AutoHide != nil {
		cfg.AutoHide = *doc.AutoHide
	}
	if doc.IconShow != nil {
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("keybind-mode: format is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
