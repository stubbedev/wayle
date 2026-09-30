package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Schema icon defaults (NetworkConfig's wifi/wired/vpn icon keys).
const (
	defaultWifiAcquiringIcon     = "cm-wireless-acquiring-symbolic"
	defaultWifiConnectedIcon     = "cm-wireless-connected-symbolic"
	defaultWifiDisabledIcon      = "cm-wireless-disabled-symbolic"
	defaultWifiOfflineIcon       = "cm-wireless-offline-symbolic"
	defaultWiredAcquiringIcon    = "cm-wired-acquiring-symbolic"
	defaultWiredConnectedIcon    = "cm-wired-connected-symbolic"
	defaultWiredDisconnectedIcon = "cm-wired-disconnected-symbolic"
	defaultVpnConnectedIcon      = "ld-lock-symbolic"
	defaultVpnConnectingIcon     = "ld-refresh-cw-symbolic"
	defaultVpnDisconnectedIcon   = "ld-unplug-symbolic"
)

// DefaultWifiSignalIcons is the schema's wifi-signal-icons list, weak
// to excellent.
func DefaultWifiSignalIcons() []string {
	return []string{
		"cm-wireless-signal-weak-symbolic",
		"cm-wireless-signal-ok-symbolic",
		"cm-wireless-signal-good-symbolic",
		"cm-wireless-signal-excellent-symbolic",
	}
}

// NetworkConfig is the network module config.
type NetworkConfig struct {
	Click ClickConfig
	// Button is the bar-button key set; LabelShow and Icon.Show/Color
	// mirror its label-show, icon-show, and icon-color.
	Button    ButtonConfig
	LabelShow bool
	Icon      IconConfig
	// WifiFallback is the label when connected but the SSID is hidden.
	WifiFallback string
	Connecting   string
	Disconnected string
	Wired        string
	// State icons.
	WifiSignalIcons       []string
	WifiAcquiringIcon     string
	WifiConnectedIcon     string
	WifiDisabledIcon      string
	WifiOfflineIcon       string
	WiredAcquiringIcon    string
	WiredConnectedIcon    string
	WiredDisconnectedIcon string
	VpnConnectedIcon      string
	VpnConnectingIcon     string
	VpnDisconnectedIcon   string
}

// DefaultsNetwork returns the schema defaults (labels from _bar.ftl).
func DefaultsNetwork() NetworkConfig {
	return NetworkConfig{
		Click:                 DefaultsClick(map[string]string{"left-click": "dropdown:network"}),
		LabelShow:             true,
		Icon:                  DefaultsIcon(true, defaultWifiOfflineIcon),
		Button:                DefaultsButton(buttonColors("auto", "accent", "accent", "bg-surface-elevated", "accent"), TokenAccent, true, 15),
		WifiFallback:          "WiFi",
		Connecting:            "Connecting...",
		Disconnected:          "Disconnected",
		Wired:                 "Wired",
		WifiSignalIcons:       DefaultWifiSignalIcons(),
		WifiAcquiringIcon:     defaultWifiAcquiringIcon,
		WifiConnectedIcon:     defaultWifiConnectedIcon,
		WifiDisabledIcon:      defaultWifiDisabledIcon,
		WifiOfflineIcon:       defaultWifiOfflineIcon,
		WiredAcquiringIcon:    defaultWiredAcquiringIcon,
		WiredConnectedIcon:    defaultWiredConnectedIcon,
		WiredDisconnectedIcon: defaultWiredDisconnectedIcon,
		VpnConnectedIcon:      defaultVpnConnectedIcon,
		VpnConnectingIcon:     defaultVpnConnectingIcon,
		VpnDisconnectedIcon:   defaultVpnDisconnectedIcon,
	}
}

// applyNetwork overlays [modules.network].
func applyNetwork(md toml.MetaData, prim toml.Primitive) (NetworkConfig, error) {
	cfg := DefaultsNetwork()
	var doc struct {
		IconName              *string   `toml:"icon-name"`
		WifiFallback          *string   `toml:"wifi-fallback-label"`
		Connecting            *string   `toml:"connecting-label"`
		Disconnected          *string   `toml:"disconnected-label"`
		Wired                 *string   `toml:"wired-label"`
		WifiSignalIcons       *[]string `toml:"wifi-signal-icons"`
		WifiAcquiringIcon     *string   `toml:"wifi-acquiring-icon"`
		WifiConnectedIcon     *string   `toml:"wifi-connected-icon"`
		WifiDisabledIcon      *string   `toml:"wifi-disabled-icon"`
		WifiOfflineIcon       *string   `toml:"wifi-offline-icon"`
		WiredAcquiringIcon    *string   `toml:"wired-acquiring-icon"`
		WiredConnectedIcon    *string   `toml:"wired-connected-icon"`
		WiredDisconnectedIcon *string   `toml:"wired-disconnected-icon"`
		VpnConnectedIcon      *string   `toml:"vpn-connected-icon"`
		VpnConnectingIcon     *string   `toml:"vpn-connecting-icon"`
		VpnDisconnectedIcon   *string   `toml:"vpn-disconnected-icon"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
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
	for _, set := range []struct {
		raw  *string
		icon *string
	}{
		{doc.WifiAcquiringIcon, &cfg.WifiAcquiringIcon},
		{doc.WifiConnectedIcon, &cfg.WifiConnectedIcon},
		{doc.WifiDisabledIcon, &cfg.WifiDisabledIcon},
		{doc.WifiOfflineIcon, &cfg.WifiOfflineIcon},
		{doc.WiredAcquiringIcon, &cfg.WiredAcquiringIcon},
		{doc.WiredConnectedIcon, &cfg.WiredConnectedIcon},
		{doc.WiredDisconnectedIcon, &cfg.WiredDisconnectedIcon},
		{doc.VpnConnectedIcon, &cfg.VpnConnectedIcon},
		{doc.VpnConnectingIcon, &cfg.VpnConnectingIcon},
		{doc.VpnDisconnectedIcon, &cfg.VpnDisconnectedIcon},
	} {
		if set.raw != nil {
			*set.icon = *set.raw
		}
	}
	if doc.WifiSignalIcons != nil {
		cfg.WifiSignalIcons = *doc.WifiSignalIcons
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
