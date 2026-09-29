package config

import (
	"github.com/BurntSushi/toml"
)

// BluetoothConfig is the bluetooth module config (label subset; the
// icon strings land with icon rendering).
type BluetoothConfig struct {
	Click     ClickConfig
	LabelShow bool
}

// DefaultsBluetooth returns the schema defaults.
func DefaultsBluetooth() BluetoothConfig {
	return BluetoothConfig{LabelShow: true}
}

// applyBluetooth overlays [modules.bluetooth].
func applyBluetooth(md toml.MetaData, prim toml.Primitive) (BluetoothConfig, error) {
	cfg := DefaultsBluetooth()
	var doc struct {
		LabelShow *bool `toml:"label-show"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
