package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// NetworkConfig is the network module config.
type NetworkConfig struct {
	Click     ClickConfig
	LabelShow bool
	// WifiFallback is the label when connected but the SSID is hidden.
	WifiFallback string
	Connecting   string
	Disconnected string
	Wired        string
}

// DefaultsNetwork returns the schema defaults (labels from _bar.ftl;
// the icon strings land with icon rendering).
func DefaultsNetwork() NetworkConfig {
	return NetworkConfig{
		Click:        DefaultsClick(map[string]string{"left-click": "dropdown:network"}),
		LabelShow:    true,
		WifiFallback: "WiFi",
		Connecting:   "Connecting...",
		Disconnected: "Disconnected",
		Wired:        "Wired",
	}
}

// applyNetwork overlays [modules.network].
func applyNetwork(md toml.MetaData, prim toml.Primitive) (NetworkConfig, error) {
	cfg := DefaultsNetwork()
	var doc struct {
		LabelShow    *bool   `toml:"label-show"`
		WifiFallback *string `toml:"wifi-fallback-label"`
		Connecting   *string `toml:"connecting-label"`
		Disconnected *string `toml:"disconnected-label"`
		Wired        *string `toml:"wired-label"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	for _, set := range []struct {
		raw   *string
		label *string
		name  string
	}{
		{doc.WifiFallback, &cfg.WifiFallback, "wifi-fallback-label"},
		{doc.Connecting, &cfg.Connecting, "connecting-label"},
		{doc.Disconnected, &cfg.Disconnected, "disconnected-label"},
		{doc.Wired, &cfg.Wired, "wired-label"},
	} {
		if set.raw == nil {
			continue
		}
		if *set.raw == "" {
			return cfg, fmt.Errorf("network: %s is empty", set.name)
		}
		*set.label = *set.raw
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
