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
	ID         string
	Command    string
	Mode       string
	IntervalMs int
	Format     string
	// Button is the bar-button key set; LabelShow, LabelMaxLength, and
	// Icon.Show/Color mirror it.
	Button         ButtonConfig
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
		Button:     DefaultsButton(buttonColors("auto", "auto", "auto", "bg-surface-elevated", "auto"), TokenAccent, true, 0),
	}
}

type customDoc struct {
	buttonDoc
	ID          *string `toml:"id"`
	Command     *string `toml:"command"`
	Mode        *string `toml:"mode"`
	IntervalMs  *int    `toml:"interval-ms"`
	Format      *string `toml:"format"`
	HideIfEmpty *bool   `toml:"hide-if-empty"`
	IconName    *string `toml:"icon-name"`
	LeftClick   *string `toml:"left-click"`
	MiddleClick *string `toml:"middle-click"`
	RightClick  *string `toml:"right-click"`
	ScrollUp    *string `toml:"scroll-up"`
	ScrollDown  *string `toml:"scroll-down"`
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
		if doc.HideIfEmpty != nil {
			cfg.HideIfEmpty = *doc.HideIfEmpty
		}
		icon := DefaultsIcon(true, "")
		if doc.IconName != nil {
			icon.Name = *doc.IconName
		}
		button, err := doc.overlay(cfg.Button, AllButtonKeys)
		if err != nil {
			return nil, fmt.Errorf("custom module %q: %w", cfg.ID, err)
		}
		// The Rust module wraps each value in ConfigProperty::new, whose
		// default is the value itself: resolve_color's fallback is the
		// configured color, not a schema default.
		button.Defaults = button.Colors
		cfg.Button = button
		button.mirrorLabel(&cfg.LabelShow, &cfg.LabelMaxLength)
		button.mirrorIcon(&icon)
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
