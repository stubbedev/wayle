package config

// BluetoothConfig is ported from crates/wayle-config/src/schemas/modules/bluetooth/mod.rs.
//
// Bluetooth connection status with a dropdown for pairing and managing devices.
type BluetoothConfig struct {
	// Icon when Bluetooth is disabled or unavailable.
	DisabledIcon string `cfg:"disabled-icon"`
	// Icon when Bluetooth is on but no devices connected.
	DisconnectedIcon string `cfg:"disconnected-icon"`
	// Icon when devices are connected.
	ConnectedIcon string `cfg:"connected-icon"`
	// Icon when scanning for devices.
	SearchingIcon string `cfg:"searching-icon"`
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
	// Display connection label (device name or count).
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
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

// DefaultsBluetooth returns the schema defaults.
func DefaultsBluetooth() BluetoothConfig {
	return BluetoothConfig{
		DisabledIcon:     "ld-bluetooth-off-symbolic",
		DisconnectedIcon: "ld-bluetooth-symbolic",
		ConnectedIcon:    "ld-bluetooth-connected-symbolic",
		SearchingIcon:    "ld-bluetooth-searching-symbolic",
		BorderShow:       false,
		BorderColor:      mustColor("blue"),
		IconShow:         true,
		IconColor:        mustColor("auto"),
		IconBgColor:      mustColor("blue"),
		LabelShow:        true,
		LabelColor:       mustColor("blue"),
		LabelMaxLength:   15,
		ButtonBgColor:    mustColor("bg-surface-elevated"),
		LeftClick:        ParseClickAction("dropdown:bluetooth"),
		RightClick:       ClickAction{},
		MiddleClick:      ClickAction{},
		ScrollUp:         ClickAction{},
		ScrollDown:       ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c BluetoothConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
