package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// KeyboardInputConfig is the keyboard-layout module config.
type KeyboardInputConfig struct {
	Format         string
	LabelShow      bool
	LayoutAliasMap map[string]string
}

// DefaultsKeyboardInput returns the schema defaults.
func DefaultsKeyboardInput() KeyboardInputConfig {
	return KeyboardInputConfig{
		Format:         "{{ alias }}",
		LabelShow:      true,
		LayoutAliasMap: map[string]string{},
	}
}

// applyKeyboardInput overlays [modules.keyboard-layout].
func applyKeyboardInput(md toml.MetaData, prim toml.Primitive) (KeyboardInputConfig, error) {
	cfg := DefaultsKeyboardInput()
	var doc struct {
		Format    *string        `toml:"format"`
		LabelShow *bool          `toml:"label-show"`
		AliasMap  map[string]any `toml:"layout-alias-map"`
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
	if doc.AliasMap != nil {
		cfg.LayoutAliasMap = map[string]string{}
		keys := make([]string, 0, len(doc.AliasMap))
		for key := range doc.AliasMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			alias, ok := doc.AliasMap[key].(string)
			if !ok {
				return cfg, fmt.Errorf("keyboard-layout: layout-alias-map[%q] is not a string", key)
			}
			cfg.LayoutAliasMap[key] = alias
		}
	}
	if strings.TrimSpace(cfg.Format) == "" {
		return cfg, errors.New("keyboard-layout: format is empty")
	}
	return cfg, nil
}
