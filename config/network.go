package config

// NetworkConfig is ported from crates/wayle-config/src/schemas/modules/network/mod.rs.
//
// Network connection status with a dropdown for switching connections.
type NetworkConfig struct {
	// WiFi icon when disabled.
	WifiDisabledIcon string `cfg:"wifi-disabled-icon"`
	// WiFi icon when connecting.
	WifiAcquiringIcon string `cfg:"wifi-acquiring-icon"`
	// WiFi icon when disconnected.
	WifiOfflineIcon string `cfg:"wifi-offline-icon"`
	// WiFi icon when connected but signal strength unavailable.
	WifiConnectedIcon string `cfg:"wifi-connected-icon"`
	// WiFi signal strength icons from weak to excellent.
	//
	// The signal percentage maps to icons: 0-25% uses icons\[0\], 26-50% uses
	// icons\[1\], etc.
	WifiSignalIcons []string `cfg:"wifi-signal-icons"`
	// Wired icon when connected.
	WiredConnectedIcon string `cfg:"wired-connected-icon"`
	// Wired icon when connecting.
	WiredAcquiringIcon string `cfg:"wired-acquiring-icon"`
	// Wired icon when disconnected.
	WiredDisconnectedIcon string `cfg:"wired-disconnected-icon"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display connection label (SSID for WiFi, "Wired" for ethernet).
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Icon when a VPN is connected.
	VpnConnectedIcon string `cfg:"vpn-connected-icon"`
	// Icon while a VPN connection is in flight.
	VpnConnectingIcon string `cfg:"vpn-connecting-icon"`
	// Icon when a VPN is configured but disconnected.
	VpnDisconnectedIcon string `cfg:"vpn-disconnected-icon"`
	// When the VPN state replaces the wifi/wired icon.
	//
	// `auto` shows it only once NetworkManager holds a VPN profile, so adding
	// the key changes nothing on a machine with no VPN.
	VpnShow VpnShow `cfg:"vpn-show"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsNetwork returns the schema defaults.
func DefaultsNetwork() NetworkConfig {
	return NetworkConfig{
		WifiDisabledIcon:  "cm-wireless-disabled-symbolic",
		WifiAcquiringIcon: "cm-wireless-acquiring-symbolic",
		WifiOfflineIcon:   "cm-wireless-offline-symbolic",
		WifiConnectedIcon: "cm-wireless-connected-symbolic",
		WifiSignalIcons: []string{
			"cm-wireless-signal-weak-symbolic",
			"cm-wireless-signal-ok-symbolic",
			"cm-wireless-signal-good-symbolic",
			"cm-wireless-signal-excellent-symbolic",
		},
		WiredConnectedIcon:    "cm-wired-symbolic",
		WiredAcquiringIcon:    "cm-wired-acquiring-symbolic",
		WiredDisconnectedIcon: "cm-wired-disconnected-symbolic",
		BorderShow:            false,
		BorderColor:           mustColor("accent"),
		IconShow:              true,
		IconColor:             mustColor("auto"),
		IconBgColor:           mustColor("accent"),
		LabelShow:             true,
		LabelColor:            mustColor("accent"),
		LabelMaxLength:        15,
		ButtonBgColor:         mustColor("bg-surface-elevated"),
		VpnConnectedIcon:      "ld-lock-symbolic",
		VpnConnectingIcon:     "ld-refresh-cw-symbolic",
		VpnDisconnectedIcon:   "ld-unplug-symbolic",
		VpnShow:               VpnShowAuto,
		LeftClick:             ParseClickAction("dropdown:network"),
		RightClick:            ClickAction{},
		MiddleClick:           ClickAction{},
		ScrollUp:              ClickAction{},
		ScrollDown:            ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c NetworkConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// VpnShow is ported from crates/wayle-config/src/schemas/modules/network/vpn.rs.
//
// Whether the VPN indicator is part of the network module.
type VpnShow string

// VpnShow values.
const (
	// Show the VPN state only once NetworkManager holds a VPN profile.
	VpnShowAuto VpnShow = "auto"
	// Always overlay the VPN state on the network icon.
	VpnShowAlways VpnShow = "always"
	// Never show it; the dropdown still lists VPNs.
	VpnShowNever VpnShow = "never"
)

var _ = registerEnum(VpnShowAuto, VpnShowAlways, VpnShowNever)
