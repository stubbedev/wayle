package config

// SeparatorConfig is ported from crates/wayle-config/src/schemas/modules/separator/mod.rs.
//
// A vertical rule between bar modules.
type SeparatorConfig struct {
	// Thickness of the separator line in pixels.
	Size uint32 `cfg:"size"`
	// Length of the separator line. Accepts a scale multiplier or pixels (e.g. `"24px"`).
	Length Size `cfg:"length"`
	// Color of the separator line.
	Color ColorValue `cfg:"color"`
}

// DefaultsSeparator returns the schema defaults.
func DefaultsSeparator() SeparatorConfig {
	return SeparatorConfig{
		Size:   1,
		Length: Size{Value: 1.5, Unit: SizeMultiplier},
		Color:  mustColor("fg-subtle"),
	}
}
