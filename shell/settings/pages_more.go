package settings

import "github.com/stubbedev/wayle/config"

// barGeneralPage is pages/bar/general.
func barGeneralPage(*config.Config) pageSpec {
	return pageSpec{id: "bar-general", navKey: "settings-nav-bar-general", icon: "ld-layout-dashboard-symbolic", header: "settings-page-bar-general", sections: []sectionSpec{
		{title: "settings-section-layout", rows: []rowSpec{
			field("bar.location"),
			field("bar.exclusive"),
			field("bar.layer"),
			field("bar.scale"),
			field("bar.layout", layoutRow),
		}},
		{title: "settings-section-appearance", rows: []rowSpec{
			field("bar.bg"),
			field("bar.background-opacity", percentage),
			field("bar.rounding"),
			field("bar.shadow"),
		}},
		{title: "settings-section-spacing", rows: fields("bar.inset-edge", "bar.inset-ends", "bar.padding", "bar.padding-ends", "bar.module-gap")},
		{title: "settings-section-border", rows: fields("bar.border-location", "bar.border-width", "bar.border-color")},
	}}
}

// barButtonPage is pages/bar/button.
func barButtonPage(*config.Config) pageSpec {
	return pageSpec{id: "bar-button", navKey: "settings-nav-bar-button", icon: "ld-square-symbolic", header: "settings-page-bar-button", sections: []sectionSpec{
		{title: "settings-section-style", rows: []rowSpec{
			field("bar.button-variant"),
			field("bar.button-opacity", percentage),
			field("bar.button-bg-opacity", percentage),
			field("bar.button-rounding"),
			field("bar.button-gap"),
			field("bar.button-icon-position"),
		}},
		{title: "settings-section-icons", rows: []rowSpec{
			field("bar.button-icon-size"),
			field("bar.button-icon-padding"),
		}},
		{title: "settings-section-labels", rows: []rowSpec{
			field("bar.button-label-size"),
			field("bar.button-label-weight"),
			field("bar.button-label-padding"),
		}},
		{title: "settings-section-border", rows: []rowSpec{
			field("bar.button-border-location"),
			field("bar.button-border-width"),
		}},
		{title: "settings-section-group", rows: []rowSpec{
			field("bar.button-group-opacity", percentage),
			field("bar.button-group-rounding"),
			field("bar.button-group-padding"),
			field("bar.button-group-module-gap"),
			field("bar.button-group-background"),
			field("bar.button-group-border-location"),
			field("bar.button-group-border-width"),
			field("bar.button-group-border-color"),
		}},
	}}
}

// notificationsPage is pages/notifications.
func notificationsPage(*config.Config) pageSpec {
	return pageSpec{id: "notifications", navKey: "settings-nav-notifications", icon: "ld-bell-symbolic", header: "settings-page-notifications", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("modules.notifications.enabled"),
		}},
		{title: "settings-section-popup-display", rows: []rowSpec{
			field("modules.notifications.popup-position"),
			field("modules.notifications.popup-max-visible"),
			field("modules.notifications.popup-stacking-order"),
			field("modules.notifications.popup-duration"),
			field("modules.notifications.popup-hover-pause"),
			field("modules.notifications.popup-shadow"),
			field("modules.notifications.popup-close-behavior"),
			field("modules.notifications.popup-urgency-bar"),
		}},
		{title: "settings-section-positioning", rows: []rowSpec{
			field("modules.notifications.popup-monitor"),
			field("modules.notifications.popup-layer"),
			field("modules.notifications.popup-margin-x", sizeBase(config.PopupMarginBaseRem)),
			field("modules.notifications.popup-margin-y", sizeBase(config.PopupMarginBaseRem)),
			field("modules.notifications.popup-gap", sizeBase(config.PopupGapBaseRem)),
		}},
		{title: "settings-section-filtering", rows: []rowSpec{
			field("modules.notifications.icon-source"),
			field("modules.notifications.blocklist", stringList),
		}},
		{title: "settings-section-animation", rows: surfaceAnimationRows("animations.notifications")},
	}}
}

// dropdownsPage is pages/dropdowns: each dropdown's size overrides.
func dropdownsPage(*config.Config) pageSpec {
	var sections []sectionSpec
	for _, d := range []string{
		"audio", "battery", "bluetooth", "brightness", "calendar", "dashboard", "media",
		"network", "notification", "treeman", "weather",
	} {
		path := "dropdowns." + d
		title, ok := config.I18nKey(path)
		if !ok {
			title = "settings-section-sizes"
		}
		sections = append(sections, sectionSpec{title: title, rows: dropdownSizeRows(path)})
	}
	sections = append(sections, sectionSpec{title: "settings-section-animation", rows: surfaceAnimationRows("animations.dropdown")})
	return pageSpec{
		id: "dropdowns", navKey: "settings-nav-dropdowns", icon: "ld-panel-top-open-symbolic",
		header: "settings-page-dropdowns", sections: sections,
	}
}

// osdPage is pages/osd.
func osdPage(*config.Config) pageSpec {
	return pageSpec{id: "osd", navKey: "settings-nav-osd", icon: "ld-monitor-symbolic", header: "settings-page-osd", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("osd.enabled"),
		}},
		{title: "settings-section-display", rows: []rowSpec{
			field("osd.position"),
			field("osd.layer"),
			field("osd.text-align"),
			field("osd.duration"),
			field("osd.monitor"),
			field("osd.margin", sizeBase(config.OsdMarginBaseRem)),
			field("osd.border"),
		}},
		{title: "settings-section-presets", rows: []rowSpec{
			field("osd.presets", toastPresetList),
		}},
		{title: "settings-section-animation", rows: surfaceAnimationRows("animations.osd")},
	}}
}

// launcherPage is pages/launcher entry.
func launcherPage(*config.Config) pageSpec {
	return pageSpec{id: "launcher", navKey: "settings-nav-launcher-page", icon: "ld-rocket-symbolic", header: "settings-page-launcher", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{
			field("launcher.location"),
			field("launcher.width", sizeBase(config.LauncherWidthBaseRem)),
			field("launcher.lines"),
			field("launcher.monitor"),
			field("launcher.modes", stringList),
			field("launcher.cycle"),
			field("launcher.fixed-num-lines"),
			field("launcher.sidebar-mode"),
		}},
		{title: "settings-section-launcher-matching", rows: []rowSpec{
			field("launcher.matching"),
			field("launcher.tokenize"),
			field("launcher.negate-char"),
			field("launcher.normalize-match"),
			field("launcher.sort"),
			field("launcher.sorting-method"),
			field("launcher.case"),
			field("launcher.auto-select"),
			field("launcher.hover-select"),
		}},
		{title: "settings-section-launcher-appearance", rows: []rowSpec{
			field("launcher.show-icons"),
			field("launcher.icon-theme"),
			field("launcher.terminal"),
			field("launcher.display-names", stringMap),
		}},
		{title: "settings-section-launcher-history", rows: []rowSpec{
			field("launcher.history.enable"),
			field("launcher.history.max-size"),
		}},
		{title: "settings-section-launcher-keybindings", rows: []rowSpec{
			field("launcher.keybindings", stringMap),
		}},
		{title: "settings-section-animation", rows: surfaceAnimationRows("animations.launcher")},
	}}
}

// launcherModesPage is pages/launcher modes_entry.
func launcherModesPage(*config.Config) pageSpec {
	return pageSpec{id: "launcher-modes", navKey: "settings-nav-launcher-modes-page", icon: "ld-grid-2x2-symbolic", header: "settings-page-launcher-modes", sections: []sectionSpec{
		{title: "settings-section-launcher-drun", rows: []rowSpec{
			field("launcher.drun.categories", stringList),
			field("launcher.drun.exclude-categories", stringList),
			field("launcher.drun.match-fields", enumList),
			field("launcher.drun.display-format"),
			field("launcher.drun.show-actions"),
			field("launcher.drun.url-launcher"),
		}},
		{title: "settings-section-launcher-run", rows: []rowSpec{
			field("launcher.run.run-command"),
			field("launcher.run.shell-command"),
			field("launcher.run.list-command"),
		}},
		{title: "settings-section-launcher-window", rows: []rowSpec{
			field("launcher.window.format"),
			field("launcher.window.match-fields", enumList),
			field("launcher.window.hide-active"),
			field("launcher.window.close-on-delete"),
		}},
		{title: "settings-section-launcher-ssh", rows: []rowSpec{
			field("launcher.ssh.client"),
			field("launcher.ssh.command"),
			field("launcher.ssh.parse-hosts"),
			field("launcher.ssh.parse-known-hosts"),
		}},
		{title: "settings-section-launcher-filebrowser", rows: []rowSpec{
			field("launcher.filebrowser.directory"),
			field("launcher.filebrowser.sorting-method"),
			field("launcher.filebrowser.directories-first"),
			field("launcher.filebrowser.show-hidden"),
			field("launcher.filebrowser.command"),
		}},
		{title: "settings-section-launcher-combi", rows: []rowSpec{
			field("launcher.combi.modes", stringList),
			field("launcher.combi.display-format"),
		}},
		{title: "settings-section-launcher-scripts", rows: []rowSpec{
			field("launcher.scripts", stringMap),
		}},
	}}
}

// themePage is pages/styling.
func themePage(*config.Config) pageSpec {
	return pageSpec{id: "theme", navKey: "settings-nav-theme", icon: "ld-palette-symbolic", header: "settings-page-theme", sections: []sectionSpec{
		{title: "settings-section-provider", rows: []rowSpec{
			field("styling.appearance"),
			field("styling.theme-provider"),
			field("styling.theming-monitor"),
		}},
		{title: "settings-section-palette", rows: []rowSpec{
			themePreset(),
			field("styling.palette.bg"),
			field("styling.palette.surface"),
			field("styling.palette.elevated"),
			field("styling.palette.fg"),
			field("styling.palette.fg-muted"),
			field("styling.palette.primary"),
			field("styling.palette.red"),
			field("styling.palette.yellow"),
			field("styling.palette.green"),
			field("styling.palette.blue"),
		}},
		{title: "settings-section-matugen", rows: []rowSpec{
			field("styling.matugen-scheme"),
			field("styling.matugen-contrast", signedNormalized),
			field("styling.matugen-source-color"),
			field("styling.matugen-light"),
		}},
		{title: "settings-section-wallust", rows: []rowSpec{
			field("styling.wallust-palette"),
			field("styling.wallust-saturation", percentage),
			field("styling.wallust-check-contrast"),
			field("styling.wallust-backend"),
			field("styling.wallust-colorspace"),
			field("styling.wallust-apply-globally"),
		}},
		{title: "settings-section-pywal", rows: []rowSpec{
			field("styling.pywal-saturation", normalized),
			field("styling.pywal-contrast", spin(1.0, 21.0, 0.5, 1)),
			field("styling.pywal-light"),
			field("styling.pywal-apply-globally"),
		}},
	}}
}

// wallpaperPage is pages/wallpaper: the image and its scaling, the
// cycling directory with its options, the per-monitor overrides and
// the change animation.
func wallpaperPage(*config.Config) pageSpec {
	return pageSpec{id: "wallpaper", navKey: "settings-nav-wallpaper", icon: "ld-image-symbolic", header: "settings-page-wallpaper", sections: []sectionSpec{
		{title: "settings-section-general", rows: []rowSpec{field("wallpaper.wallpaper", filePath), field("wallpaper.fit-mode")}},
		{title: "settings-section-cycling", rows: []rowSpec{
			field("wallpaper.cycling-directory", filePath),
			cyclingReveal("wallpaper.cycling-directory",
				field("wallpaper.cycling-mode"),
				field("wallpaper.cycling-interval-mins", spin(1, 1440, 1, 0)),
				field("wallpaper.cycling-same-image"),
			),
		}},
		{title: "settings-section-display", rows: []rowSpec{field("wallpaper.monitors", monitorWallpaperList)}},
		{title: "settings-section-animation", rows: surfaceAnimationRows("animations.wallpaper")},
	}}
}
