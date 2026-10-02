package config

import (
	"reflect"
	"strings"
)

// i18nPrefixes are the settings label prefixes, by Rust schema name:
// each #[wayle_config(i18n_prefix = "...")] struct. A field's label key
// is its struct's prefix, a dash and its canonical key (wayle-derive
// process_fields), the key the settings domain's FTL carries.
var i18nPrefixes = map[string]string{
	"AnimationsConfig":          "settings-animations",
	"BarConfig":                 "settings-bar",
	"BatteryConfig":             "settings-modules-battery",
	"BluetoothConfig":           "settings-modules-bluetooth",
	"BrightnessConfig":          "settings-modules-brightness",
	"CavaConfig":                "settings-modules-cava",
	"ClockConfig":               "settings-modules-clock",
	"CpuConfig":                 "settings-modules-cpu",
	"DashboardConfig":           "settings-modules-dashboard",
	"DropdownsConfig":           "settings-dropdowns",
	"GeneralConfig":             "settings-general",
	"GreeterConfig":             "settings-greeter",
	"HyprlandWorkspacesConfig":  "settings-modules-hyprland-workspaces",
	"HyprsunsetConfig":          "settings-modules-hyprsunset",
	"IdleInhibitConfig":         "settings-modules-idle-inhibit",
	"KeybindModeConfig":         "settings-modules-keybind-mode",
	"KeyboardInputConfig":       "settings-modules-keyboard-input",
	"LauncherCombiConfig":       "settings-launcher-combi",
	"LauncherConfig":            "settings-launcher",
	"LauncherDrunConfig":        "settings-launcher-drun",
	"LauncherFilebrowserConfig": "settings-launcher-filebrowser",
	"LauncherHistoryConfig":     "settings-launcher-history",
	"LauncherRunConfig":         "settings-launcher-run",
	"LauncherSshConfig":         "settings-launcher-ssh",
	"LauncherWindowConfig":      "settings-launcher-window",
	"LockConfig":                "settings-lock",
	"MailConfig":                "settings-modules-mail",
	"MangoWorkspacesConfig":     "settings-modules-mango-workspaces",
	"MediaConfig":               "settings-modules-media",
	"MicrophoneConfig":          "settings-modules-microphone",
	"NetstatConfig":             "settings-modules-netstat",
	"NetworkConfig":             "settings-modules-network",
	"NiriWorkspacesConfig":      "settings-modules-niri-workspaces",
	"NotificationConfig":        "settings-modules-notifications",
	"OsdConfig":                 "settings-osd",
	"PaletteConfig":             "settings-palette",
	"PowerConfig":               "settings-modules-power",
	"PowerProfilesConfig":       "settings-modules-power-profiles",
	"RamConfig":                 "settings-modules-ram",
	"RecorderConfig":            "settings-modules-recorder",
	"ScreenshotConfig":          "settings-modules-screenshot",
	"SeparatorConfig":           "settings-modules-separator",
	"SharePickerConfig":         "settings-share-picker",
	"StorageConfig":             "settings-modules-storage",
	"StylingConfig":             "settings-styling",
	"SwayWorkspacesConfig":      "settings-modules-sway-workspaces",
	"SystrayConfig":             "settings-modules-systray",
	"TreemanConfig":             "settings-modules-treeman",
	"UserSessionConfig":         "settings-modules-dashboard-user-session",
	"VolumeConfig":              "settings-modules-volume",
	"WallpaperConfig":           "settings-wallpaper",
	"WeatherConfig":             "settings-modules-weather",
	"WindowTitleConfig":         "settings-modules-window-title",
	"WorldClockConfig":          "settings-modules-world-clock",
}

// I18nKey is the settings label key of the field at a dot path, false
// for a path that names no field, a container, or a field of a struct
// without a prefix.
func I18nKey(path string) (string, bool) {
	t, f, ok := fieldAtPath(typeOf[Config](), path)
	if !ok || isContainer(f.typ) || f.noI18n {
		return "", false
	}
	prefix, ok := i18nPrefixes[schemaName(t)]
	if !ok {
		return "", false
	}
	return prefix + "-" + f.key, true
}

// fieldAtPath finds the field a dot path names (canonical keys or
// aliases) and the struct type holding it.
func fieldAtPath(t reflect.Type, path string) (reflect.Type, fieldInfo, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fieldInfo{}, false
	}
	segment, rest, more := strings.Cut(path, ".")
	for _, f := range fieldsOf(t) {
		for _, key := range f.lookupKeys() {
			if key != segment {
				continue
			}
			if !more {
				return t, f, true
			}
			if !isContainer(f.typ) {
				return nil, fieldInfo{}, false
			}
			return fieldAtPath(f.typ, rest)
		}
	}
	return nil, fieldInfo{}, false
}
