package config

import (
	"fmt"
)

// Execution modes.
const (
	CustomModePoll  = "poll"
	CustomModeWatch = "watch"
)

// CustomModuleConfig is one [[modules.custom]] definition: a shell
// command rendered into the bar (the CustomModuleDefinition schema).
type CustomModuleConfig struct {
	ID             string
	Command        string
	Mode           string
	IntervalMs     int
	Format         string
	LabelShow      bool
	LabelMaxLength int
	HideIfEmpty    bool
	Icon           IconConfig
	Click          ClickConfig
}

// DefaultsCustomModule seeds one definition with the schema defaults.
func DefaultsCustomModule() CustomModuleConfig {
	return CustomModuleConfig{
		Mode:       CustomModePoll,
		IntervalMs: 5000,
		Format:     "{{ output }}",
		LabelShow:  true,
	}
}

type customDoc struct {
	ID             *string     `toml:"id"`
	Command        *string     `toml:"command"`
	Mode           *string     `toml:"mode"`
	IntervalMs     *int        `toml:"interval-ms"`
	Format         *string     `toml:"format"`
	LabelShow      *bool       `toml:"label-show"`
	LabelMaxLength *int        `toml:"label-max-length"`
	HideIfEmpty    *bool       `toml:"hide-if-empty"`
	IconShow       *bool       `toml:"icon-show"`
	IconName       *string     `toml:"icon-name"`
	IconColor      *ColorValue `toml:"icon-color"`
	LeftClick      *string     `toml:"left-click"`
	MiddleClick    *string     `toml:"middle-click"`
	RightClick     *string     `toml:"right-click"`
	ScrollUp       *string     `toml:"scroll-up"`
	ScrollDown     *string     `toml:"scroll-down"`
}

// applyCustomDefinitions decodes the [[modules.custom]] array.
func applyCustomDefinitions(defs []customDoc) ([]CustomModuleConfig, error) {
	out := make([]CustomModuleConfig, 0, len(defs))
	seen := map[string]bool{}
	for i, doc := range defs {
		cfg := DefaultsCustomModule()
		if doc.ID != nil {
			cfg.ID = *doc.ID
		}
		if cfg.ID == "" {
			return nil, fmt.Errorf("custom module %d: id is required", i)
		}
		if seen[cfg.ID] {
			return nil, fmt.Errorf("custom module %q: duplicate id", cfg.ID)
		}
		seen[cfg.ID] = true
		if doc.Command != nil {
			cfg.Command = *doc.Command
		}
		if doc.Mode != nil {
			cfg.Mode = *doc.Mode
		}
		if cfg.Mode != CustomModePoll && cfg.Mode != CustomModeWatch {
			return nil, fmt.Errorf("custom module %q: mode %q is not poll or watch", cfg.ID, cfg.Mode)
		}
		if doc.IntervalMs != nil {
			cfg.IntervalMs = *doc.IntervalMs
		}
		if cfg.IntervalMs < 0 {
			return nil, fmt.Errorf("custom module %q: interval-ms is negative", cfg.ID)
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
			return nil, fmt.Errorf("custom module %q: label-max-length is negative", cfg.ID)
		}
		if doc.HideIfEmpty != nil {
			cfg.HideIfEmpty = *doc.HideIfEmpty
		}
		icon := DefaultsIcon(true, "")
		if doc.IconShow != nil {
			icon.Show = *doc.IconShow
		}
		if doc.IconName != nil {
			icon.Name = *doc.IconName
		}
		if doc.IconColor != nil {
			icon.Color = *doc.IconColor
		}
		cfg.Icon = icon
		binding := DefaultsClick(nil)
		for _, entry := range []struct {
			raw   *string
			name  string
			field *ClickAction
		}{
			{doc.LeftClick, "left-click", &binding.LeftClick},
			{doc.MiddleClick, "middle-click", &binding.MiddleClick},
			{doc.RightClick, "right-click", &binding.RightClick},
			{doc.ScrollUp, "scroll-up", &binding.ScrollUp},
			{doc.ScrollDown, "scroll-down", &binding.ScrollDown},
		} {
			if entry.raw == nil {
				continue
			}
			action, err := ParseClickAction(*entry.raw)
			if err != nil {
				return nil, fmt.Errorf("custom module %q: %s: %w", cfg.ID, entry.name, err)
			}
			*entry.field = action
		}
		cfg.Click = binding
		out = append(out, cfg)
	}
	return out, nil
}

// CustomByID finds one definition by id.
func (c *Config) CustomByID(id string) (CustomModuleConfig, bool) {
	for _, def := range c.Custom {
		if def.ID == id {
			return def, true
		}
	}
	return CustomModuleConfig{}, false
}
