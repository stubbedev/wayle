package config

import (
	"github.com/BurntSushi/toml"
)

// Schema icon defaults (BluetoothConfig's state icon keys).
const (
	defaultBtConnectedIcon    = "ld-bluetooth-connected-symbolic"
	defaultBtDisabledIcon     = "ld-bluetooth-off-symbolic"
	defaultBtDisconnectedIcon = "ld-bluetooth-symbolic"
	defaultBtSearchingIcon    = "ld-bluetooth-searching-symbolic"
)

// BluetoothConfig is the bluetooth module config.
type BluetoothConfig struct {
	Click     ClickConfig
	LabelShow bool
	Icon      IconConfig
	// State icons: absent or powered-off, searching, and the plain
	// paired states.
	ConnectedIcon    string
	DisabledIcon     string
	DisconnectedIcon string
	SearchingIcon    string
}

// DefaultsBluetooth returns the schema defaults.
func DefaultsBluetooth() BluetoothConfig {
	return BluetoothConfig{
		LabelShow:        true,
		Icon:             DefaultsIcon(true, defaultBtDisconnectedIcon),
		ConnectedIcon:    defaultBtConnectedIcon,
		DisabledIcon:     defaultBtDisabledIcon,
		DisconnectedIcon: defaultBtDisconnectedIcon,
		SearchingIcon:    defaultBtSearchingIcon,
	}
}

// applyBluetooth overlays [modules.bluetooth].
func applyBluetooth(md toml.MetaData, prim toml.Primitive) (BluetoothConfig, error) {
	cfg := DefaultsBluetooth()
	var doc struct {
		LabelShow        *bool       `toml:"label-show"`
		IconShow         *bool       `toml:"icon-show"`
		IconName         *string     `toml:"icon-name"`
		IconColor        *ColorValue `toml:"icon-color"`
		ConnectedIcon    *string     `toml:"connected-icon"`
		DisabledIcon     *string     `toml:"disabled-icon"`
		DisconnectedIcon *string     `toml:"disconnected-icon"`
		SearchingIcon    *string     `toml:"searching-icon"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.IconShow != nil {
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	for _, set := range []struct {
		raw  *string
		icon *string
	}{
		{doc.ConnectedIcon, &cfg.ConnectedIcon},
		{doc.DisabledIcon, &cfg.DisabledIcon},
		{doc.DisconnectedIcon, &cfg.DisconnectedIcon},
		{doc.SearchingIcon, &cfg.SearchingIcon},
	} {
		if set.raw != nil {
			*set.icon = *set.raw
		}
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
