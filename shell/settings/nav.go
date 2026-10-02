package settings

import "github.com/stubbedev/wayle/config"

// initialPage is the page the window opens on (INITIAL_PAGE_ID), or
// the first page while that one is not ported.
const initialPage = "bar-general"

// navSection is NavSectionLayout: a titled sidebar group of pages.
type navSection struct {
	key   string
	pages []pageSpec
}

// layout is pages/nav.rs layout(): the sidebar's sections in order,
// each page built from the config (a page may list what the config
// holds).
func layout(cfg *config.Config) []navSection {
	sections := []navSection{
		{key: "settings-nav-bar-section", pages: []pageSpec{barButtonPage(cfg), barDropdownPage(cfg)}},
		{key: "settings-nav-appearance", pages: []pageSpec{themePage(cfg), animationsPage(cfg)}},
		{key: "settings-nav-system", pages: []pageSpec{generalPage(cfg)}},
		{key: "settings-nav-overlays", pages: []pageSpec{notificationsPage(cfg), osdPage(cfg), sharePickerPage(cfg), dropdownsPage(cfg)}},
		{key: "settings-nav-launcher", pages: []pageSpec{launcherPage(cfg), launcherModesPage(cfg)}},
		{key: "settings-nav-lock", pages: []pageSpec{lockPage(cfg)}},
		{key: "settings-nav-greeter", pages: []pageSpec{greeterPage(cfg)}},
		{key: "settings-nav-modules", pages: modulePages(cfg)},
	}
	out := sections[:0]
	for _, s := range sections {
		if len(s.pages) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// pageByID finds a page in the layout.
func pageByID(sections []navSection, id string) (pageSpec, bool) {
	for _, s := range sections {
		for _, p := range s.pages {
			if p.id == id {
				return p, true
			}
		}
	}
	return pageSpec{}, false
}

// firstPage is the page to open: initialPage, else the first listed.
func firstPage(sections []navSection) string {
	if _, ok := pageByID(sections, initialPage); ok {
		return initialPage
	}
	if len(sections) > 0 && len(sections[0].pages) > 0 {
		return sections[0].pages[0].id
	}
	return ""
}

// barDropdownPage is pages/bar/dropdown.rs.
func barDropdownPage(*config.Config) pageSpec {
	return pageSpec{
		id: "bar-dropdown", navKey: "settings-nav-bar-dropdown", icon: "ld-panel-bottom-symbolic",
		header: "settings-page-bar-dropdown",
		sections: []sectionSpec{
			{title: "settings-section-behavior", rows: fields("bar.dropdown-shadow", "bar.dropdown-autohide", "bar.dropdown-freeze-label")},
			{title: "settings-section-appearance", rows: []rowSpec{field("bar.dropdown-opacity", percentage)}},
		},
	}
}

// animationsPage is pages/animations.
func animationsPage(*config.Config) pageSpec {
	duration := optionalSpin(0, maxDurationMS, durationStepMS, durationFallbackMS)
	return pageSpec{
		id: "animations", navKey: "settings-nav-animations", icon: "ld-zap-symbolic",
		header: "settings-page-animations",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("animations.enabled", "animations.transition", "animations.duration")},
			{title: "settings-section-direction", rows: []rowSpec{
				field("animations.enter"), field("animations.exit"),
				field("animations.enter-duration", duration), field("animations.exit-duration", duration),
			}},
			{title: "settings-section-speed", rows: fields("animations.ui-duration", "animations.interaction-duration", "animations.indicators")},
		},
	}
}

// sharePickerPage is pages/share_picker.
func sharePickerPage(*config.Config) pageSpec {
	return pageSpec{
		id: "share-picker", navKey: "settings-nav-share-picker", icon: "ld-app-window-symbolic",
		header: "settings-page-share-picker",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("share-picker.default-page", "share-picker.hide-token-restore")},
			{title: "settings-section-display", rows: []rowSpec{
				field("share-picker.width", sizeBase(config.SharePickerWidthBaseRem)),
				field("share-picker.height", sizeBase(config.SharePickerHeightBaseRem)),
				field("share-picker.resize-size"),
				field("share-picker.widget-size", sizeBase(config.SharePickerWidgetBaseRem)),
			}},
			{title: "settings-section-share-picker-windows", rows: []rowSpec{
				field("share-picker.windows-spacing", sizeBase(config.SharePickerWindowsSpacingBaseRem)),
				field("share-picker.windows-min-per-row"), field("share-picker.windows-max-per-row"),
			}},
			{title: "settings-section-share-picker-outputs", rows: []rowSpec{
				field("share-picker.outputs-spacing", sizeBase(config.SharePickerOutputsSpacingBaseRem)),
				field("share-picker.outputs-show-label"), field("share-picker.outputs-respect-scaling"),
			}},
			{title: "settings-section-animation", rows: surfaceAnimationRows("animations.share-picker")},
		},
	}
}
