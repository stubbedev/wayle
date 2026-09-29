package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// SeparatorConfig is the separator module config.
type SeparatorConfig struct {
	Color  ColorValue
	Length Size
	Size   int
}

// DefaultsSeparator returns the schema defaults.
func DefaultsSeparator() SeparatorConfig {
	return SeparatorConfig{
		Color:  mustColor("fg-subtle"),
		Length: Size{Value: 1.5, Unit: SizeMultiplier},
		Size:   1,
	}
}

// applySeparator overlays [modules.separator].
func applySeparator(md toml.MetaData, prim toml.Primitive) (SeparatorConfig, error) {
	cfg := DefaultsSeparator()
	var doc struct {
		Color  string    `toml:"color"`
		Length tomlValue `toml:"length"`
		Size   *int      `toml:"size"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Color != "" {
		cv, err := ParseColorValue(doc.Color)
		if err != nil {
			return cfg, fmt.Errorf("separator: color: %w", err)
		}
		cfg.Color = cv
	}
	if doc.Length.value != nil {
		if err := cfg.Length.unmarshal(doc.Length.value, "length"); err != nil {
			return cfg, err
		}
	}
	if doc.Size != nil {
		if *doc.Size < 1 || *doc.Size > 100 {
			return cfg, fmt.Errorf("separator: size %d outside 1-100", *doc.Size)
		}
		cfg.Size = *doc.Size
	}
	return cfg, nil
}
