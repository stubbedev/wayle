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
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
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
		Button:           DefaultsButton(buttonColors("auto", "blue", "blue", "bg-surface-elevated", "blue"), TokenBlue, true, 15),
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
		IconName         *string `toml:"icon-name"`
		ConnectedIcon    *string `toml:"connected-icon"`
		DisabledIcon     *string `toml:"disabled-icon"`
		DisconnectedIcon *string `toml:"disconnected-icon"`
		SearchingIcon    *string `toml:"searching-icon"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
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
	button, err := applyButton(md, prim, cfg.Button, AllButtonKeys)
	if err != nil {
		return cfg, err
	}
	cfg.Button = button
	button.mirrorLabel(&cfg.LabelShow, nil)
	button.mirrorIcon(&cfg.Icon)
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
