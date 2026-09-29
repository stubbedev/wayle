package config

import (
	"github.com/BurntSushi/toml"
)

// IconConfig is the shared icon-* trio of the bar modules.
type IconConfig struct {
	Show  bool
	Name  string
	Color ColorValue
}

// applyIcon decodes the icon keys of a module table on top of the
// module's defaults.
func applyIcon(md toml.MetaData, prim toml.Primitive, defaults IconConfig) (IconConfig, error) {
	var doc struct {
		Show  *bool       `toml:"icon-show"`
		Name  *string     `toml:"icon-name"`
		Color *ColorValue `toml:"icon-color"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return defaults, err
	}
	if doc.Show != nil {
		defaults.Show = *doc.Show
	}
	if doc.Name != nil {
		defaults.Name = *doc.Name
	}
	if doc.Color != nil {
		defaults.Color = *doc.Color
	}
	return defaults, nil
}

// DefaultsIcon seeds one module's icon defaults; the token resolves to
// the module's own accent or the bar fg at paint time.
func DefaultsIcon(show bool, name string) IconConfig {
	return IconConfig{
		Show:  show,
		Name:  name,
		Color: mustColor("auto"),
	}
}
