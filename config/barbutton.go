package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// ButtonColors are a bar button's five color slots
// (crates/wayle-widgets/src/components/bar_buttons/types.rs
// BarButtonColors).
type ButtonColors struct {
	Icon     ColorValue // icon-color
	Label    ColorValue // label-color
	IconBg   ColorValue // icon-bg-color
	ButtonBg ColorValue // button-bg-color
	Border   ColorValue // border-color
}

// ButtonConfig is the bar-button key set a bar_button module carries
// (wayle-derive BAR_BUTTON_REQUIRED minus the click keys, which live in
// ClickConfig): border-show, border-color, icon-show, icon-color,
// icon-bg-color, label-show, label-color, label-max-length, and
// button-bg-color.
type ButtonConfig struct {
	BorderShow bool
	IconShow   bool
	LabelShow  bool
	// LabelMaxLength truncates the label; 0 disables.
	LabelMaxLength int
	Colors         ButtonColors
	// Defaults are the module's schema colors: the fallback resolve_color
	// uses for a custom hex under a non-wayle theme provider (the Rust
	// ConfigProperty::default).
	Defaults ButtonColors
	// AutoIconColor is the token an auto icon color resolves to under
	// the basic variant — the module's accent, fixed per module rather
	// than a config key.
	AutoIconColor CssToken
}

// DefaultsButton builds a module's button defaults: its schema colors,
// its auto icon token, and the label keys that vary per module. Every
// module defaults border-show off and icon-show on.
func DefaultsButton(colors ButtonColors, autoIcon CssToken, labelShow bool, labelMaxLength int) ButtonConfig {
	return ButtonConfig{
		IconShow:       true,
		LabelShow:      labelShow,
		LabelMaxLength: labelMaxLength,
		Colors:         colors,
		Defaults:       colors,
		AutoIconColor:  autoIcon,
	}
}

// buttonColors spells a module's schema color defaults as config
// strings, in the ButtonColors field order.
func buttonColors(icon, label, iconBg, buttonBg, border string) ButtonColors {
	return ButtonColors{
		Icon: mustColor(icon), Label: mustColor(label), IconBg: mustColor(iconBg),
		ButtonBg: mustColor(buttonBg), Border: mustColor(border),
	}
}

// ButtonKeys selects which bar-button keys a module's table declares;
// a key outside the set is ignored, as serde ignores unknown fields.
type ButtonKeys uint16

// The bar-button keys.
const (
	KeyBorderShow ButtonKeys = 1 << iota
	KeyBorderColor
	KeyIconShow
	KeyIconColor
	KeyIconBgColor
	KeyLabelShow
	KeyLabelColor
	KeyLabelMaxLength
	KeyButtonBgColor

	// AllButtonKeys is the full bar_button set.
	AllButtonKeys = KeyBorderShow | KeyBorderColor | KeyIconShow | KeyIconColor | KeyIconBgColor |
		KeyLabelShow | KeyLabelColor | KeyLabelMaxLength | KeyButtonBgColor
)

// buttonDoc mirrors the bar-button keys of a module table.
type buttonDoc struct {
	BorderShow     *bool       `toml:"border-show"`
	BorderColor    *ColorValue `toml:"border-color"`
	IconShow       *bool       `toml:"icon-show"`
	IconColor      *ColorValue `toml:"icon-color"`
	IconBgColor    *ColorValue `toml:"icon-bg-color"`
	LabelShow      *bool       `toml:"label-show"`
	LabelColor     *ColorValue `toml:"label-color"`
	LabelMaxLength *int64      `toml:"label-max-length"`
	ButtonBgColor  *ColorValue `toml:"button-bg-color"`
}

// overlay applies the declared keys onto b. label-max-length is a u32
// in the schema; a negative or oversized value is a load error.
func (d buttonDoc) overlay(b ButtonConfig, keys ButtonKeys) (ButtonConfig, error) {
	for _, f := range []struct {
		key ButtonKeys
		raw *bool
		dst *bool
	}{
		{KeyBorderShow, d.BorderShow, &b.BorderShow},
		{KeyIconShow, d.IconShow, &b.IconShow},
		{KeyLabelShow, d.LabelShow, &b.LabelShow},
	} {
		if keys&f.key != 0 && f.raw != nil {
			*f.dst = *f.raw
		}
	}
	for _, f := range []struct {
		key ButtonKeys
		raw *ColorValue
		dst *ColorValue
	}{
		{KeyBorderColor, d.BorderColor, &b.Colors.Border},
		{KeyIconColor, d.IconColor, &b.Colors.Icon},
		{KeyIconBgColor, d.IconBgColor, &b.Colors.IconBg},
		{KeyLabelColor, d.LabelColor, &b.Colors.Label},
		{KeyButtonBgColor, d.ButtonBgColor, &b.Colors.ButtonBg},
	} {
		if keys&f.key != 0 && f.raw != nil {
			*f.dst = *f.raw
		}
	}
	if keys&KeyLabelMaxLength != 0 && d.LabelMaxLength != nil {
		if n := *d.LabelMaxLength; n < 0 || n > 1<<32-1 {
			return b, fmt.Errorf("config: label-max-length %d outside 0-%d", n, uint32(1<<32-1))
		}
		b.LabelMaxLength = int(*d.LabelMaxLength)
	}
	return b, nil
}

// applyButton decodes the declared bar-button keys of a module table on
// top of the module's defaults.
func applyButton(md toml.MetaData, prim toml.Primitive, defaults ButtonConfig, keys ButtonKeys) (ButtonConfig, error) {
	var doc buttonDoc
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return defaults, err
	}
	return doc.overlay(defaults, keys)
}

// mirrorLabel copies the label keys onto a module's own label fields,
// which predate ButtonConfig; maxLength may be nil.
func (b ButtonConfig) mirrorLabel(show *bool, maxLength *int) {
	*show = b.LabelShow
	if maxLength != nil {
		*maxLength = b.LabelMaxLength
	}
}

// mirrorIcon copies icon-show and icon-color onto a module's IconConfig.
func (b ButtonConfig) mirrorIcon(icon *IconConfig) {
	icon.Show = b.IconShow
	icon.Color = b.Colors.Icon
}

// mirrorIconShow copies icon-show onto every per-state icon.
func (b ButtonConfig) mirrorIconShow(icons map[string]IconConfig) {
	for name, icon := range icons {
		icon.Show = b.IconShow
		icons[name] = icon
	}
}

// ContainerConfig is the key set of a bar_container module (cava,
// systray): border-show, border-color, and button-bg-color, the
// container background (wayle-derive BAR_CONTAINER_REQUIRED).
type ContainerConfig struct {
	BorderShow  bool
	Background  ColorValue
	BorderColor ColorValue
	// DefaultBackground and DefaultBorderColor are the schema colors,
	// resolve_color's fallback under a non-wayle provider.
	DefaultBackground  ColorValue
	DefaultBorderColor ColorValue
}

// DefaultsContainer builds a container module's defaults from its
// schema colors; border-show defaults off.
func DefaultsContainer(background, border string) ContainerConfig {
	bg, bc := mustColor(background), mustColor(border)
	return ContainerConfig{
		Background: bg, BorderColor: bc,
		DefaultBackground: bg, DefaultBorderColor: bc,
	}
}

// applyContainer decodes a container module's keys onto its defaults.
func applyContainer(md toml.MetaData, prim toml.Primitive, defaults ContainerConfig) (ContainerConfig, error) {
	var doc struct {
		BorderShow    *bool       `toml:"border-show"`
		BorderColor   *ColorValue `toml:"border-color"`
		ButtonBgColor *ColorValue `toml:"button-bg-color"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return defaults, err
	}
	setIf(doc.BorderShow, &defaults.BorderShow)
	setIf(doc.BorderColor, &defaults.BorderColor)
	setIf(doc.ButtonBgColor, &defaults.Background)
	return defaults, nil
}
