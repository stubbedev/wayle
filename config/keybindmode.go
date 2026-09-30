package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// KeybindModeConfig is the keybind-mode module configuration: the
// Hyprland submap rendered as the active keybind layer.
type KeybindModeConfig struct {
	Click  ClickConfig
	Format string
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
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
		Button:    DefaultsButton(buttonColors("auto", "blue", "blue", "bg-surface-elevated", "blue"), TokenBlue, true, 0),
	}
}

// applyKeybindMode overlays [modules.keybind-mode].
func applyKeybindMode(md toml.MetaData, prim toml.Primitive) (KeybindModeConfig, error) {
	cfg := DefaultsKeybindMode()
	var doc struct {
		Format   *string `toml:"format"`
		AutoHide *bool   `toml:"auto-hide"`
		IconName *string `toml:"icon-name"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.AutoHide != nil {
		cfg.AutoHide = *doc.AutoHide
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.Format != nil && *doc.Format == "" {
		return cfg, errors.New("keybind-mode: format is empty")
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.Icon)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
