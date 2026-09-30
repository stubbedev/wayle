package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// SystrayIconBaseRem is the tray icon's base size in rem
// (schemas/modules/systray ICON_BASE_REM); icon-scale multiplies it.
const SystrayIconBaseRem = 1.25

// TrayItemOverride replaces one item's icon and/or its color; Name is
// a glob matched against the item's Id and Title.
type TrayItemOverride struct {
	Name     string
	Icon     string
	Color    ColorValue
	HasColor bool
}

// SystrayConfig is [modules.systray] (schemas/modules/systray): the
// tray has no bindings of its own - each item takes the clicks.
type SystrayConfig struct {
	IconScale       Size
	ItemGap         Size
	InternalPadding Size
	// Blacklist globs hide items whose Id or Title matches.
	Blacklist   []string
	Overrides   []TrayItemOverride
	BorderShow  bool
	BorderColor ColorValue
	ButtonBG    ColorValue
}

// DefaultsSystray returns the schema defaults.
func DefaultsSystray() SystrayConfig {
	return SystrayConfig{
		IconScale:       Size{Value: 1.0, Unit: SizeMultiplier},
		ItemGap:         Size{Value: 0.25, Unit: SizeMultiplier},
		InternalPadding: Size{Value: 0.5, Unit: SizeMultiplier},
		Blacklist:       []string{},
		BorderColor:     mustColor("border-accent"),
		ButtonBG:        mustColor("bg-surface-elevated"),
	}
}

// applySystray overlays [modules.systray].
func applySystray(md toml.MetaData, prim toml.Primitive) (SystrayConfig, error) {
	cfg := DefaultsSystray()
	var doc struct {
		IconScale       tomlValue   `toml:"icon-scale"`
		ItemGap         tomlValue   `toml:"item-gap"`
		InternalPadding tomlValue   `toml:"internal-padding"`
		Blacklist       *[]string   `toml:"blacklist"`
		BorderShow      *bool       `toml:"border-show"`
		BorderColor     *ColorValue `toml:"border-color"`
		ButtonBG        *ColorValue `toml:"button-bg-color"`
		Overrides       []struct {
			Name  *string `toml:"name"`
			Icon  *string `toml:"icon"`
			Color *string `toml:"color"`
		} `toml:"overrides"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	for _, s := range []struct {
		raw  tomlValue
		dest *Size
		key  string
	}{
		{doc.IconScale, &cfg.IconScale, "icon-scale"},
		{doc.ItemGap, &cfg.ItemGap, "item-gap"},
		{doc.InternalPadding, &cfg.InternalPadding, "internal-padding"},
	} {
		if s.raw.value == nil {
			continue
		}
		if err := s.dest.unmarshal(s.raw.value, s.key); err != nil {
			return cfg, fmt.Errorf("systray: %w", err)
		}
	}
	if doc.Blacklist != nil {
		cfg.Blacklist = *doc.Blacklist
	}
	if doc.BorderShow != nil {
		cfg.BorderShow = *doc.BorderShow
	}
	if doc.BorderColor != nil {
		cfg.BorderColor = *doc.BorderColor
	}
	if doc.ButtonBG != nil {
		cfg.ButtonBG = *doc.ButtonBG
	}
	for _, o := range doc.Overrides {
		if o.Name == nil {
			return cfg, errors.New("systray: an override needs a name")
		}
		entry := TrayItemOverride{Name: *o.Name}
		if o.Icon != nil {
			entry.Icon = *o.Icon
		}
		if o.Color != nil {
			cv, err := ParseColorValue(*o.Color)
			if err != nil {
				return cfg, fmt.Errorf("systray: override %q color: %w", *o.Name, err)
			}
			entry.Color, entry.HasColor = cv, true
		}
		cfg.Overrides = append(cfg.Overrides, entry)
	}
	return cfg, nil
}
