package config

import (
	"github.com/BurntSushi/toml"
)

// WindowTitleConfig is the window-title module config.
type WindowTitleConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and LabelMaxLength
	// mirror its label-show and label-max-length.
	Button         ButtonConfig
	Format         string
	LabelShow      bool
	LabelMaxLength int
}

// DefaultsWindowTitle returns the schema defaults.
func DefaultsWindowTitle() WindowTitleConfig {
	return WindowTitleConfig{
		Format:         "{{ title }}",
		LabelShow:      true,
		LabelMaxLength: 50,
		Button:         DefaultsButton(buttonColors("auto", "blue", "blue", "bg-surface-elevated", "blue"), TokenBlue, true, 50),
	}
}

// applyWindowTitle overlays [modules.window-title].
func applyWindowTitle(md toml.MetaData, prim toml.Primitive) (WindowTitleConfig, error) {
	cfg := DefaultsWindowTitle()
	var doc struct {
		Format *string `toml:"format"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, &cfg.LabelMaxLength)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
