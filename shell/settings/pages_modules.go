package settings

import "github.com/stubbedev/wayle/config"

// The module pages (pages/modules), in modules::factories order.

func modulePages(cfg *config.Config) []pageSpec {
	return []pageSpec{
		batteryPage(cfg),
		bluetoothPage(cfg),
		brightnessPage(cfg),
		cavaPage(cfg),
		clockPage(cfg),
		cpuPage(cfg),
		dashboardPage(cfg),
		hyprlandWorkspacesPage(cfg),
		hyprsunsetPage(cfg),
		idleInhibitPage(cfg),
		keybindModePage(cfg),
		keyboardInputPage(cfg),
		mailPage(cfg),
		mangoWorkspacesPage(cfg),
		mediaPage(cfg),
		microphonePage(cfg),
		netstatPage(cfg),
		networkPage(cfg),
		compositorWorkspacesPage("niri"),
		notificationPage(cfg),
		powerPage(cfg),
		powerProfilesPage(cfg),
		ramPage(cfg),
		recorderPage(cfg),
		screenshotPage(cfg),
		separatorPage(cfg),
		storagePage(cfg),
		compositorWorkspacesPage("sway"),
		systrayPage(cfg),
		treemanPage(cfg),
		volumePage(cfg),
		weatherPage(cfg),
		windowTitlePage(cfg),
		worldClockPage(cfg),
	}
}

// batteryPage is pages/modules/battery.
func batteryPage(*config.Config) pageSpec {
	return pageSpec{id: "battery", navKey: "settings-nav-battery", icon: "ld-battery-full-symbolic", header: "settings-page-battery", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.battery.charging-icon", iconRow),
			field("modules.battery.alert-icon", iconRow),
			field("modules.battery.format"),
			field("modules.battery.level-icons", iconList),
			field("modules.battery.thresholds", thresholdList),
		}},
		barDisplaySection("modules.battery"),
		colorsSection("modules.battery"),
		actionsSection("modules.battery", actionChoices("battery")),
	}}
}

// bluetoothPage is pages/modules/bluetooth.
func bluetoothPage(*config.Config) pageSpec {
	return pageSpec{id: "bluetooth", navKey: "settings-nav-bluetooth", icon: "ld-bluetooth-symbolic", header: "settings-page-bluetooth", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.bluetooth.disabled-icon", iconRow),
			field("modules.bluetooth.disconnected-icon", iconRow),
			field("modules.bluetooth.connected-icon", iconRow),
			field("modules.bluetooth.searching-icon", iconRow),
		}},
		barDisplaySection("modules.bluetooth"),
		colorsSection("modules.bluetooth"),
		actionsSection("modules.bluetooth", actionChoices("bluetooth")),
	}}
}

// brightnessPage is pages/modules/brightness.
func brightnessPage(*config.Config) pageSpec {
	return pageSpec{id: "brightness", navKey: "settings-nav-brightness", icon: "ld-sun-symbolic", header: "settings-page-brightness", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.brightness.format"),
			field("modules.brightness.level-icons", iconList),
			field("modules.brightness.min-brightness", spin(0, 100, 1, 0)),
			field("modules.brightness.enable-external"),
			field("modules.brightness.thresholds", thresholdList),
		}},
		barDisplaySection("modules.brightness"),
		colorsSection("modules.brightness"),
		actionsSection("modules.brightness", actionChoices("brightness")),
	}}
}

// cavaPage is pages/modules/cava.
func cavaPage(*config.Config) pageSpec {
	return pageSpec{id: "cava", navKey: "settings-nav-cava", icon: "ld-audio-lines-symbolic", header: "settings-page-cava", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.cava.bars", spin(1.0, 256.0, 1.0, 0)),
			field("modules.cava.framerate", spin(1.0, 360.0, 1.0, 0)),
			field("modules.cava.stereo"),
			field("modules.cava.noise-reduction", normalized),
			field("modules.cava.monstercat", spin(0.0, 10.0, 0.1, 1)),
			field("modules.cava.waves"),
			field("modules.cava.low-cutoff", spin(1.0, 50000.0, 1.0, 0)),
			field("modules.cava.high-cutoff", spin(1.0, 50000.0, 1.0, 0)),
			field("modules.cava.input"),
			field("modules.cava.source"),
			field("modules.cava.style"),
			field("modules.cava.direction"),
			field("modules.cava.bar-width"),
			field("modules.cava.bar-gap"),
			field("modules.cava.internal-padding"),
		}},
		{title: "settings-section-bar-display", rows: []rowSpec{
			field("modules.cava.border-show"),
		}},
		{title: "settings-section-colors", rows: []rowSpec{
			field("modules.cava.color"),
			field("modules.cava.button-bg-color"),
			field("modules.cava.border-color"),
		}},
		{title: "settings-section-actions", rows: []rowSpec{
			field("modules.cava.left-click", actions(actionChoices("cava"))),
			field("modules.cava.right-click", actions(actionChoices("cava"))),
			field("modules.cava.middle-click", actions(actionChoices("cava"))),
			field("modules.cava.scroll-up", actions(actionChoices("cava"))),
			field("modules.cava.scroll-down", actions(actionChoices("cava"))),
		}},
	}}
}

// clockPage is pages/modules/clock.
func clockPage(*config.Config) pageSpec {
	return pageSpec{id: "clock", navKey: "settings-nav-clock", icon: "ld-clock-symbolic", header: "settings-page-clock", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.clock.format"),
			field("modules.clock.icon-name", iconRow),
		}},
		{title: "settings-section-dropdown", rows: []rowSpec{
			field("modules.clock.dropdown-show-seconds"),
			field("modules.clock.calendar-weekday-start"),
		}},
		barDisplaySection("modules.clock"),
		colorsSection("modules.clock"),
		actionsSection("modules.clock", actionChoices("clock")),
	}}
}

// cpuPage is pages/modules/cpu.
func cpuPage(*config.Config) pageSpec {
	return pageSpec{id: "cpu", navKey: "settings-nav-cpu", icon: "ld-cpu-symbolic", header: "settings-page-cpu", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.cpu.poll-interval-ms"),
			field("modules.cpu.temp-sensor"),
			field("modules.cpu.format"),
			field("modules.cpu.icon-name", iconRow),
			field("modules.cpu.thresholds", thresholdList),
		}},
		barDisplaySection("modules.cpu"),
		colorsSection("modules.cpu"),
		actionsSection("modules.cpu", actionChoices("cpu")),
	}}
}

// dashboardPage is pages/modules/dashboard.
func dashboardPage(*config.Config) pageSpec {
	return pageSpec{id: "dashboard", navKey: "settings-nav-dashboard", icon: "ld-layout-dashboard-symbolic", header: "settings-page-dashboard", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.dashboard.icon-override", iconRow),
		}},
		{title: "settings-section-commands", rows: []rowSpec{
			field("modules.dashboard.user-session.actions", enumList),
			field("modules.dashboard.dropdown-lock-command"),
			field("modules.dashboard.dropdown-logout-command"),
			field("modules.dashboard.dropdown-reboot-command"),
			field("modules.dashboard.dropdown-poweroff-command"),
		}},
		{title: "settings-section-thresholds", rows: []rowSpec{
			field("modules.dashboard.usage-warning", spin(0, 100, 1, 0)),
			field("modules.dashboard.usage-error", spin(0, 100, 1, 0)),
			field("modules.dashboard.temp-warning", spin(0, 150, 1, 0)),
			field("modules.dashboard.temp-error", spin(0, 150, 1, 0)),
			field("modules.dashboard.battery-warning", spin(0, 100, 1, 0)),
			field("modules.dashboard.battery-critical", spin(0, 100, 1, 0)),
		}},
		{title: "settings-section-bar-display", rows: []rowSpec{
			field("modules.dashboard.border-show"),
		}},
		{title: "settings-section-colors", rows: []rowSpec{
			field("modules.dashboard.icon-color"),
			field("modules.dashboard.icon-bg-color"),
			field("modules.dashboard.border-color"),
		}},
		{title: "settings-section-actions", rows: []rowSpec{
			field("modules.dashboard.left-click", actions(actionChoices("dashboard"))),
			field("modules.dashboard.right-click", actions(actionChoices("dashboard"))),
			field("modules.dashboard.middle-click", actions(actionChoices("dashboard"))),
			field("modules.dashboard.scroll-up", actions(actionChoices("dashboard"))),
			field("modules.dashboard.scroll-down", actions(actionChoices("dashboard"))),
		}},
	}}
}

// hyprsunsetPage is pages/modules/hyprsunset.
func hyprsunsetPage(*config.Config) pageSpec {
	return pageSpec{id: "hyprsunset", navKey: "settings-nav-hyprsunset", icon: "ld-sun-symbolic", header: "settings-page-hyprsunset", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.hyprsunset.format"),
			field("modules.hyprsunset.temperature"),
			field("modules.hyprsunset.gamma"),
			field("modules.hyprsunset.icon-off", iconRow),
			field("modules.hyprsunset.icon-on", iconRow),
		}},
		{title: "settings-section-hyprsunset-schedule", rows: []rowSpec{
			field("modules.hyprsunset.auto-schedule"),
			field("modules.hyprsunset.latitude", spin(-90.0, 90.0, 0.1, 4)),
			field("modules.hyprsunset.longitude", spin(-180.0, 180.0, 0.1, 4)),
		}},
		barDisplaySection("modules.hyprsunset"),
		colorsSection("modules.hyprsunset"),
		actionsSection("modules.hyprsunset", actionChoices("hyprsunset")),
	}}
}

// idleInhibitPage is pages/modules/idle_inhibit.
func idleInhibitPage(*config.Config) pageSpec {
	return pageSpec{id: "idle-inhibit", navKey: "settings-nav-idle-inhibit", icon: "ld-coffee-symbolic", header: "settings-page-idle-inhibit", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.idle-inhibit.startup-duration"),
			field("modules.idle-inhibit.icon-inactive", iconRow),
			field("modules.idle-inhibit.icon-active", iconRow),
			field("modules.idle-inhibit.format"),
		}},
		barDisplaySection("modules.idle-inhibit"),
		colorsSection("modules.idle-inhibit"),
		actionsSection("modules.idle-inhibit", actionChoices("idle-inhibit")),
	}}
}

// keybindModePage is pages/modules/keybind_mode.
func keybindModePage(*config.Config) pageSpec {
	return pageSpec{id: "keybind-mode", navKey: "settings-nav-keybind-mode", icon: "ld-layers-symbolic", header: "settings-page-keybind-mode", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.keybind-mode.format"),
			field("modules.keybind-mode.icon-name", iconRow),
			field("modules.keybind-mode.auto-hide"),
		}},
		barDisplaySection("modules.keybind-mode"),
		colorsSection("modules.keybind-mode"),
		actionsSection("modules.keybind-mode", actionChoices("keybind-mode")),
	}}
}

// keyboardInputPage is pages/modules/keyboard_input.
func keyboardInputPage(*config.Config) pageSpec {
	return pageSpec{id: "keyboard-input", navKey: "settings-nav-keyboard-input", icon: "ld-keyboard-symbolic", header: "settings-page-keyboard-input", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.keyboard-input.format"),
			field("modules.keyboard-input.icon-name", iconRow),
			field("modules.keyboard-input.layout-alias-map", stringMap),
		}},
		barDisplaySection("modules.keyboard-input"),
		colorsSection("modules.keyboard-input"),
		actionsSection("modules.keyboard-input", actionChoices("keyboard-input")),
	}}
}

// mediaPage is pages/modules/media.
func mediaPage(*config.Config) pageSpec {
	return pageSpec{id: "media", navKey: "settings-nav-media", icon: "ld-music-symbolic", header: "settings-page-media", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.media.icon-type"),
			field("modules.media.format"),
			field("modules.media.icon-name", iconRow),
			field("modules.media.spinning-disc-icon", iconRow),
			field("modules.media.player-icons", stringMap),
			field("modules.media.players-ignored", stringList),
			field("modules.media.player-priority", stringList),
		}},
		barDisplaySection("modules.media"),
		colorsSection("modules.media"),
		actionsSection("modules.media", actionChoices("media")),
	}}
}

// microphonePage is pages/modules/microphone.
func microphonePage(*config.Config) pageSpec {
	return pageSpec{id: "microphone", navKey: "settings-nav-microphone", icon: "ld-mic-symbolic", header: "settings-page-microphone", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.microphone.icon-active", iconRow),
			field("modules.microphone.icon-muted", iconRow),
			field("modules.microphone.thresholds", thresholdList),
		}},
		barDisplaySection("modules.microphone"),
		colorsSection("modules.microphone"),
		actionsSection("modules.microphone", actionChoices("microphone")),
	}}
}

// netstatPage is pages/modules/netstat.
func netstatPage(*config.Config) pageSpec {
	return pageSpec{id: "netstat", navKey: "settings-nav-netstat", icon: "ld-activity-symbolic", header: "settings-page-netstat", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.netstat.poll-interval-ms"),
			field("modules.netstat.interface"),
			field("modules.netstat.format"),
			field("modules.netstat.icon-name", iconRow),
		}},
		barDisplaySection("modules.netstat"),
		colorsSection("modules.netstat"),
		actionsSection("modules.netstat", actionChoices("netstat")),
	}}
}

// networkPage is pages/modules/network.
func networkPage(*config.Config) pageSpec {
	return pageSpec{id: "network", navKey: "settings-nav-network", icon: "ld-wifi-symbolic", header: "settings-page-network", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.network.wifi-disabled-icon", iconRow),
			field("modules.network.wifi-acquiring-icon", iconRow),
			field("modules.network.wifi-offline-icon", iconRow),
			field("modules.network.wifi-connected-icon", iconRow),
			field("modules.network.wired-connected-icon", iconRow),
			field("modules.network.wired-acquiring-icon", iconRow),
			field("modules.network.wired-disconnected-icon", iconRow),
			field("modules.network.wifi-signal-icons", iconList),
		}},
		barDisplaySection("modules.network"),
		colorsSection("modules.network"),
		actionsSection("modules.network", actionChoices("network")),
	}}
}

// notificationPage is pages/modules/notification_module.
func notificationPage(*config.Config) pageSpec {
	return pageSpec{id: "notification", navKey: "settings-nav-notification", icon: "ld-bell-symbolic", header: "settings-page-notification", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.notifications.icon-name", iconRow),
			field("modules.notifications.icon-unread", iconRow),
			field("modules.notifications.icon-dnd", iconRow),
			field("modules.notifications.thresholds", thresholdList),
		}},
		barDisplaySection("modules.notifications"),
		colorsSection("modules.notifications"),
		actionsSection("modules.notifications", actionChoices("notification")),
	}}
}

// powerProfilesPage is pages/modules/power_profiles.
func powerProfilesPage(*config.Config) pageSpec {
	return pageSpec{id: "power-profiles", navKey: "settings-nav-power-profiles", icon: "ld-scale-symbolic", header: "settings-page-power-profiles", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.power-profiles.format"),
		}},
		{title: "settings-section-power-profiles-profiles", rows: []rowSpec{
			field("modules.power-profiles.icon-power-saver", iconRow),
			field("modules.power-profiles.color-power-saver"),
			field("modules.power-profiles.icon-balanced", iconRow),
			field("modules.power-profiles.color-balanced"),
			field("modules.power-profiles.icon-performance", iconRow),
			field("modules.power-profiles.color-performance"),
		}},
		barDisplaySection("modules.power-profiles"),
		colorsSection("modules.power-profiles"),
		actionsSection("modules.power-profiles", actionChoices("power-profiles")),
	}}
}

// powerPage is pages/modules/power.
func powerPage(*config.Config) pageSpec {
	return pageSpec{id: "power", navKey: "settings-nav-power", icon: "ld-power-symbolic", header: "settings-page-power", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.power.icon-name", iconRow),
		}},
		{title: "settings-section-bar-display", rows: []rowSpec{
			field("modules.power.border-show"),
		}},
		{title: "settings-section-colors", rows: []rowSpec{
			field("modules.power.icon-color"),
			field("modules.power.icon-bg-color"),
			field("modules.power.border-color"),
		}},
		{title: "settings-section-power-menu", rows: []rowSpec{
			field("modules.power.show-lock"),
			field("modules.power.lock-command"),
			field("modules.power.show-logout"),
			field("modules.power.logout-command"),
			field("modules.power.show-suspend"),
			field("modules.power.suspend-command"),
			field("modules.power.show-reboot"),
			field("modules.power.reboot-command"),
			field("modules.power.show-shutdown"),
			field("modules.power.shutdown-command"),
		}},
		{title: "settings-section-actions", rows: []rowSpec{
			field("modules.power.left-click", actions(actionChoices("power"))),
			field("modules.power.right-click", actions(actionChoices("power"))),
			field("modules.power.middle-click", actions(actionChoices("power"))),
			field("modules.power.scroll-up", actions(actionChoices("power"))),
			field("modules.power.scroll-down", actions(actionChoices("power"))),
		}},
	}}
}

// ramPage is pages/modules/ram.
func ramPage(*config.Config) pageSpec {
	return pageSpec{id: "ram", navKey: "settings-nav-ram", icon: "ld-memory-stick-symbolic", header: "settings-page-ram", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.ram.poll-interval-ms"),
			field("modules.ram.format"),
			field("modules.ram.icon-name", iconRow),
			field("modules.ram.thresholds", thresholdList),
		}},
		barDisplaySection("modules.ram"),
		colorsSection("modules.ram"),
		actionsSection("modules.ram", actionChoices("ram")),
	}}
}

// screenshotPage is pages/modules/screenshot.
func screenshotPage(*config.Config) pageSpec {
	return pageSpec{id: "screenshot", navKey: "settings-nav-screenshot", icon: "ld-camera-symbolic", header: "settings-page-screenshot", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.screenshot.output-directory"),
			field("modules.screenshot.filename-format"),
			field("modules.screenshot.label"),
			field("modules.screenshot.copy-to-clipboard"),
			field("modules.screenshot.notify"),
		}},
		{title: "settings-section-icons", rows: []rowSpec{
			field("modules.screenshot.icon", iconRow),
		}},
		barDisplaySection("modules.screenshot"),
		colorsSection("modules.screenshot"),
		actionsSection("modules.screenshot", actionChoices("screenshot")),
	}}
}

// separatorPage is pages/modules/separator.
func separatorPage(*config.Config) pageSpec {
	return pageSpec{id: "separator", navKey: "settings-nav-separator", icon: "ld-minus-symbolic", header: "settings-page-separator", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.separator.size"),
			field("modules.separator.length"),
		}},
		{title: "settings-section-colors", rows: []rowSpec{
			field("modules.separator.color"),
		}},
	}}
}

// storagePage is pages/modules/storage.
func storagePage(*config.Config) pageSpec {
	return pageSpec{id: "storage", navKey: "settings-nav-storage", icon: "ld-hard-drive-symbolic", header: "settings-page-storage", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.storage.poll-interval-ms"),
			field("modules.storage.mount-point", mountPoints),
			field("modules.storage.format"),
			field("modules.storage.icon-name", iconRow),
			field("modules.storage.thresholds", thresholdList),
		}},
		barDisplaySection("modules.storage"),
		colorsSection("modules.storage"),
		actionsSection("modules.storage", actionChoices("storage")),
	}}
}

// treemanPage is pages/modules/treeman.
func treemanPage(*config.Config) pageSpec {
	return pageSpec{id: "treeman", navKey: "settings-nav-treeman", icon: "ld-layers-symbolic", header: "settings-page-treeman", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.treeman.format"),
			field("modules.treeman.icon-name", iconRow),
			field("modules.treeman.icon-preparing", iconRow),
			field("modules.treeman.icon-tearing-down", iconRow),
			field("modules.treeman.icon-failed", iconRow),
			field("modules.treeman.hide-if-empty"),
		}},
		barDisplaySection("modules.treeman"),
		colorsSection("modules.treeman"),
		actionsSection("modules.treeman", actionChoices("treeman")),
	}}
}

// volumePage is pages/modules/volume.
func volumePage(*config.Config) pageSpec {
	return pageSpec{id: "volume", navKey: "settings-nav-volume", icon: "ld-volume-2-symbolic", header: "settings-page-volume", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.volume.icon-muted", iconRow),
			field("modules.volume.format"),
			field("modules.volume.level-icons", iconList),
			field("modules.volume.thresholds", thresholdList),
		}},
		{title: "settings-section-dropdown", rows: []rowSpec{
			field("modules.volume.dropdown-app-icons"),
		}},
		barDisplaySection("modules.volume"),
		colorsSection("modules.volume"),
		actionsSection("modules.volume", actionChoices("volume")),
	}}
}

// weatherPage is pages/modules/weather.
func weatherPage(*config.Config) pageSpec {
	return pageSpec{id: "weather", navKey: "settings-nav-weather", icon: "ld-cloud-sun-symbolic", header: "settings-page-weather", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.weather.provider"),
			field("modules.weather.location"),
			field("modules.weather.units"),
			field("modules.weather.format"),
			field("modules.weather.time-format"),
			field("modules.weather.refresh-interval-seconds"),
			field("modules.weather.icon-name", iconRow),
		}},
		{title: "settings-section-api-keys", rows: []rowSpec{
			field("modules.weather.visual-crossing-key"),
			field("modules.weather.weatherapi-key"),
		}},
		barDisplaySection("modules.weather"),
		colorsSection("modules.weather"),
		actionsSection("modules.weather", actionChoices("weather")),
	}}
}

// windowTitlePage is pages/modules/window_title.
func windowTitlePage(*config.Config) pageSpec {
	return pageSpec{id: "window-title", navKey: "settings-nav-window-title", icon: "ld-app-window-symbolic", header: "settings-page-window-title", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.window-title.format"),
			field("modules.window-title.icon-name", iconRow),
			field("modules.window-title.icon-mappings", stringMap),
		}},
		barDisplaySection("modules.window-title"),
		colorsSection("modules.window-title"),
		actionsSection("modules.window-title", actionChoices("window-title")),
	}}
}

// worldClockPage is pages/modules/world_clock.
func worldClockPage(*config.Config) pageSpec {
	return pageSpec{id: "world-clock", navKey: "settings-nav-world-clock", icon: "ld-globe-symbolic", header: "settings-page-world-clock", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.world-clock.format"),
			field("modules.world-clock.icon-name", iconRow),
		}},
		barDisplaySection("modules.world-clock"),
		colorsSection("modules.world-clock"),
		actionsSection("modules.world-clock", actionChoices("world-clock")),
	}}
}

// recorderPage is pages/modules/recorder.
func recorderPage(*config.Config) pageSpec {
	const m = "modules.recorder"
	return pageSpec{id: "recorder", navKey: "settings-nav-recorder", icon: "ld-video-symbolic", header: "settings-page-recorder", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field(m + ".format"),
			field(m + ".output-format"),
			field(m + ".output-directory"),
			field(m + ".show-cursor"),
			field(m+".start-delay-ms", milliseconds(0, 5000)),
		}},
		// Quality and the encoder are chosen automatically, so the
		// framerate is the only video knob.
		{title: "settings-section-video", rows: []rowSpec{field(m+".framerate", spin(1, 240, 1, 0))}},
		{title: "settings-section-audio", rows: []rowSpec{
			field(m + ".system-audio"),
			field(m + ".microphone"),
			field(m+".microphone-device", microphoneDevice),
		}},
		{title: "settings-section-webcam", rows: []rowSpec{
			field(m + ".webcam-enabled"),
			field(m+".webcam-device", webcamDevice),
			field(m+".webcam-x", percentage),
			field(m+".webcam-y", percentage),
			field(m+".webcam-size", percentage),
		}},
		{title: "settings-section-icons", rows: []rowSpec{
			field(m+".icon-idle", iconRow),
			field(m+".icon-recording", iconRow),
			field(m+".icon-paused", iconRow),
		}},
		barDisplaySection(m),
		colorsSection(m),
		actionsSection(m, actionChoices("recorder")),
	}}
}

// mailPage is pages/modules/mail.
func mailPage(*config.Config) pageSpec {
	const m = "modules.mail"
	return pageSpec{id: "mail", navKey: "settings-nav-mail", icon: "ld-mail-symbolic", header: "settings-page-mail", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field(m + ".format"),
			field(m + ".query"),
			field(m+".icon-name", iconRow),
			field(m + ".hide-when-zero"),
			field(m + ".notify"),
			field(m + ".notify-summary"),
			field(m + ".notify-body"),
		}},
		{title: "settings-section-mail-accounts", rows: []rowSpec{field(m+".accounts", mailAccountList)}},
		barDisplaySection(m),
		colorsSection(m),
		actionsSection(m, actionChoices("mail")),
	}}
}

// systrayPage is pages/modules/systray.
func systrayPage(*config.Config) pageSpec {
	const m = "modules.systray"
	return pageSpec{id: "systray", navKey: "settings-nav-systray", icon: "ld-panel-top-symbolic", header: "settings-page-systray", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field(m+".icon-scale", sizeBase(config.SystrayIconBaseRem)),
			field(m + ".item-gap"),
			field(m + ".internal-padding"),
			field(m+".blacklist", stringList),
			field(m+".overrides", trayOverrideList),
		}},
		{title: "settings-section-bar-display", rows: fields(m + ".border-show")},
		{title: "settings-section-colors", rows: fields(m+".border-color", m+".button-bg-color")},
	}}
}
