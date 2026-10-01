package config

// Icon views: each module's icon-show/icon-color with the icon a
// module shows by default. Modules that swap icons by state build the
// view for the state's icon with IconWith.

// IconWith builds an icon view.
func IconWith(show bool, name string, color ColorValue) IconConfig {
	return IconConfig{Show: show, Name: name, Color: color}
}

// Icon is the battery's fallback (alert) icon.
func (c BatteryConfig) Icon() IconConfig { return IconWith(c.IconShow, c.AlertIcon, c.IconColor) }

// Icon is the bluetooth disconnected icon.
func (c BluetoothConfig) Icon() IconConfig {
	return IconWith(c.IconShow, c.DisconnectedIcon, c.IconColor)
}

// Icon is the keybind-mode icon.
func (c KeybindModeConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the microphone's active icon.
func (c MicrophoneConfig) Icon() IconConfig {
	return IconWith(c.IconShow, c.IconActive, c.IconColor)
}

// Icon is the network offline icon.
func (c NetworkConfig) Icon() IconConfig {
	return IconWith(c.IconShow, c.WifiOfflineIcon, c.IconColor)
}

// Icon is the notifications bell.
func (c NotificationConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the power icon; the power module always shows it.
func (c PowerConfig) Icon() IconConfig { return IconWith(true, c.IconName, c.IconColor) }

// Icon is the recorder's idle icon.
func (c RecorderConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconIdle, c.IconColor) }

// Icon is the CPU icon.
func (c CpuConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the RAM icon.
func (c RamConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the storage icon.
func (c StorageConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the treeman stable icon.
func (c TreemanConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the weather fallback icon.
func (c WeatherConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the world-clock icon.
func (c WorldClockConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the clock icon.
func (c ClockConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the netstat icon.
func (c NetstatConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the mail icon.
func (c MailConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the media fallback icon.
func (c MediaConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the window-title fallback icon.
func (c WindowTitleConfig) Icon() IconConfig { return IconWith(c.IconShow, c.IconName, c.IconColor) }

// Icon is the keyboard-input icon.
func (c KeyboardInputConfig) Icon() IconConfig {
	return IconWith(c.IconShow, c.IconName, c.IconColor)
}

// Icon is the volume icon at full level; the module swaps the name by
// level and mute state.
func (c VolumeConfig) Icon() IconConfig {
	name := c.IconMuted
	if n := len(c.LevelIcons); n > 0 {
		name = c.LevelIcons[n-1]
	}
	return IconWith(c.IconShow, name, c.IconColor)
}
