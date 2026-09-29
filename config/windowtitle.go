package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// WindowTitleConfig is the window-title module config.
type WindowTitleConfig struct {
	Click          ClickConfig
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
	}
}

// applyWindowTitle overlays [modules.window-title].
func applyWindowTitle(md toml.MetaData, prim toml.Primitive) (WindowTitleConfig, error) {
	cfg := DefaultsWindowTitle()
	var doc struct {
		Format         *string `toml:"format"`
		LabelShow      *bool   `toml:"label-show"`
		LabelMaxLength *int    `toml:"label-max-length"`
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
	if doc.LabelMaxLength != nil {
		cfg.LabelMaxLength = *doc.LabelMaxLength
	}
	if cfg.LabelMaxLength < 0 {
		return cfg, errors.New("window-title: label-max-length is negative")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
