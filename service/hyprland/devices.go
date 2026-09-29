package hyprland

// KeyboardDevice is one entry of j/devices' keyboards array.
type KeyboardDevice struct {
	Address      string `json:"address"`
	Name         string `json:"name"`
	Rules        string `json:"rules"`
	Model        string `json:"model"`
	Layout       string `json:"layout"`
	Variant      string `json:"variant"`
	ActiveKeymap string `json:"active_keymap"`
	Main         bool   `json:"main"`
}

// Devices is j/devices: the compositor's input devices.
type Devices struct {
	Keyboards []KeyboardDevice `json:"keyboards"`
}

// Devices queries the compositor's input devices (j/devices).
func (c *Connection) Devices() (Devices, error) {
	reply, err := c.Command("j/devices")
	if err != nil {
		return Devices{}, err
	}
	return decodeJSON[Devices](reply)
}
