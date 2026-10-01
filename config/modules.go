package config

import "reflect"

// ModulesConfig is the [modules] table, one section per bar module
// (crates/wayle-config/src/schemas/modules/mod.rs). Config embeds it,
// so cfg.Battery reads cfg.ModulesConfig.Battery.
//
// Configuration for all available Wayle modules.
type ModulesConfig struct {
	// Battery status module.
	Battery BatteryConfig `cfg:"battery"`
	// Bluetooth connection module.
	Bluetooth BluetoothConfig `cfg:"bluetooth"`
	// Backlight brightness module.
	Brightness BrightnessConfig `cfg:"brightness"`
	// Cava audio visualizer module.
	Cava CavaConfig `cfg:"cava"`
	// Clock display module.
	Clock ClockConfig `cfg:"clock"`
	// CPU usage module.
	CPU CpuConfig `cfg:"cpu"`
	// Dashboard module.
	Dashboard DashboardConfig `cfg:"dashboard"`
	// Hyprland workspace switcher module.
	HyprlandWorkspaces HyprlandWorkspacesConfig `cfg:"hyprland-workspaces"`
	// Hyprsunset (blue light filter) module.
	Hyprsunset HyprsunsetConfig `cfg:"hyprsunset"`
	// Idle inhibitor module.
	IdleInhibit IdleInhibitConfig `cfg:"idle-inhibit"`
	// Keybind mode indicator module.
	KeybindMode KeybindModeConfig `cfg:"keybind-mode"`
	// Keyboard input module.
	KeyboardInput KeyboardInputConfig `cfg:"keyboard-input"`
	// Unread mail count module.
	Mail MailConfig `cfg:"mail"`
	// MangoWM tag switcher module.
	MangoWorkspaces MangoWorkspacesConfig `cfg:"mango-workspaces"`
	// Media player module.
	Media MediaConfig `cfg:"media"`
	// Microphone input module.
	Microphone MicrophoneConfig `cfg:"microphone"`
	// Network connection module.
	Network NetworkConfig `cfg:"network"`
	// Network traffic statistics module.
	Netstat NetstatConfig `cfg:"netstat"`
	// Niri workspace switcher module.
	NiriWorkspaces NiriWorkspacesConfig `cfg:"niri-workspaces"`
	// Notification center module.
	Notification NotificationConfig `cfg:"notifications,deprecated=notification"`
	// Power menu module.
	Power PowerConfig `cfg:"power"`
	// Power profile indicator/switcher module.
	PowerProfiles PowerProfilesConfig `cfg:"power-profiles"`
	// RAM usage module.
	RAM RamConfig `cfg:"ram"`
	// Screen recorder module.
	Recorder RecorderConfig `cfg:"recorder"`
	// Screenshot capture module.
	Screenshot ScreenshotConfig `cfg:"screenshot"`
	// Storage usage module.
	Storage StorageConfig `cfg:"storage"`
	// Separator module.
	Separator SeparatorConfig `cfg:"separator"`
	// sway workspace switcher module.
	SwayWorkspaces SwayWorkspacesConfig `cfg:"sway-workspaces"`
	// System tray module.
	Systray SystrayConfig `cfg:"systray"`
	// treeman worktree health module.
	Treeman TreemanConfig `cfg:"treeman"`
	// Volume control module.
	Volume VolumeConfig `cfg:"volume"`
	// Weather display module.
	Weather WeatherConfig `cfg:"weather"`
	// Window title module.
	WindowTitle WindowTitleConfig `cfg:"window-title"`
	// World clock module.
	WorldClock WorldClockConfig `cfg:"world-clock"`
	// Custom user-defined modules, each backed by a shell command. See
	// [`CustomModuleDefinition`] for all fields (id, command, interval, click
	// actions, icons, etc.). Reference them in a layout with `custom-<id>`.
	//
	// ## Example
	//
	// ```toml
	// [[modules.custom]]
	// id = "gpu-temp"
	// command = "nvidia-smi --query-gpu=temperature.gpu --format=csv,noheader"
	// interval-ms = 5000
	// icon-name = "ld-thermometer-symbolic"
	//
	// [[modules.custom]]
	// id = "weather"
	// command = "curl -s wttr.in/?format=%t"
	// interval-ms = 600000
	// ```
	Custom []CustomModuleDefinition `cfg:"custom"`
}

// clicker is a module section with the five bar_button bindings.
type clicker interface {
	Clicks() ClickConfig
}

// ModuleClicks returns the bindings of the [modules.<name>] section
// named by its schema key; ok is false for a module without bindings
// (or without a section).
func (m *ModulesConfig) ModuleClicks(name string) (ClickConfig, bool) {
	v := valueOf(m)
	for _, f := range fieldsOf(v.Type()) {
		if f.key != name {
			continue
		}
		c, ok := reflect.TypeAssert[clicker](v.FieldByIndex(f.index))
		if !ok {
			return ClickConfig{}, false
		}
		return c.Clicks(), true
	}
	return ClickConfig{}, false
}

// DefaultsModules returns every module's schema defaults.
func DefaultsModules() ModulesConfig {
	return ModulesConfig{
		Battery:            DefaultsBattery(),
		Bluetooth:          DefaultsBluetooth(),
		Brightness:         DefaultsBrightness(),
		Cava:               DefaultsCava(),
		Clock:              DefaultsClock(),
		CPU:                DefaultsCpu(),
		Dashboard:          DefaultsDashboard(),
		HyprlandWorkspaces: DefaultsHyprlandWorkspaces(),
		Hyprsunset:         DefaultsHyprsunset(),
		IdleInhibit:        DefaultsIdleInhibit(),
		KeybindMode:        DefaultsKeybindMode(),
		KeyboardInput:      DefaultsKeyboardInput(),
		Mail:               DefaultsMail(),
		MangoWorkspaces:    DefaultsMangoWorkspaces(),
		Media:              DefaultsMedia(),
		Microphone:         DefaultsMicrophone(),
		Network:            DefaultsNetwork(),
		Netstat:            DefaultsNetstat(),
		NiriWorkspaces:     DefaultsNiriWorkspaces(),
		Notification:       DefaultsNotification(),
		Power:              DefaultsPower(),
		PowerProfiles:      DefaultsPowerProfiles(),
		RAM:                DefaultsRam(),
		Recorder:           DefaultsRecorder(),
		Screenshot:         DefaultsScreenshot(),
		Storage:            DefaultsStorage(),
		Separator:          DefaultsSeparator(),
		SwayWorkspaces:     DefaultsSwayWorkspaces(),
		Systray:            DefaultsSystray(),
		Treeman:            DefaultsTreeman(),
		Volume:             DefaultsVolume(),
		Weather:            DefaultsWeather(),
		WindowTitle:        DefaultsWindowTitle(),
		WorldClock:         DefaultsWorldClock(),
		Custom:             []CustomModuleDefinition{},
	}
}
