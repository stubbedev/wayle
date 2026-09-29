package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// ClickConfig carries a module's five input bindings.
type ClickConfig struct {
	LeftClick   ClickAction
	MiddleClick ClickAction
	RightClick  ClickAction
	ScrollUp    ClickAction
	ScrollDown  ClickAction
}

// applyClicks decodes the binding keys from a module table on top of
// the defaults every module config seeds. It shares the module's
// primitive so each apply function can pull both its own keys and the
// bindings from one [modules.<name>] table.
func applyClicks(md toml.MetaData, prim toml.Primitive, defaults ClickConfig) (ClickConfig, error) {
	var doc struct {
		LeftClick   *string `toml:"left-click"`
		MiddleClick *string `toml:"middle-click"`
		RightClick  *string `toml:"right-click"`
		ScrollUp    *string `toml:"scroll-up"`
		ScrollDown  *string `toml:"scroll-down"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return defaults, err
	}
	for _, entry := range []struct {
		raw    *string
		target *ClickAction
		name   string
	}{
		{doc.LeftClick, &defaults.LeftClick, "left-click"},
		{doc.MiddleClick, &defaults.MiddleClick, "middle-click"},
		{doc.RightClick, &defaults.RightClick, "right-click"},
		{doc.ScrollUp, &defaults.ScrollUp, "scroll-up"},
		{doc.ScrollDown, &defaults.ScrollDown, "scroll-down"},
	} {
		if entry.raw == nil {
			continue
		}
		action, err := ParseClickAction(*entry.raw)
		if err != nil {
			return defaults, fmt.Errorf("%s: %w", entry.name, err)
		}
		*entry.target = action
	}
	return defaults, nil
}

// DefaultsClick builds the schema's binding defaults for one module
// from name -> raw action pairs; unparsed keys panic, since the pairs
// are compile-time data.
func DefaultsClick(defs map[string]string) ClickConfig {
	cfg := ClickConfig{}
	for key, raw := range defs {
		action := MustClickAction(raw)
		switch key {
		case "left-click":
			cfg.LeftClick = action
		case "middle-click":
			cfg.MiddleClick = action
		case "right-click":
			cfg.RightClick = action
		case "scroll-up":
			cfg.ScrollUp = action
		case "scroll-down":
			cfg.ScrollDown = action
		}
	}
	return cfg
}
