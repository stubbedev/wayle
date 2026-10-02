package settings

import "github.com/stubbedev/wayle/config"

// The workspace module pages (pages/modules/*_workspaces).

// workspaceSizing is the sizing section every workspace page shares;
// padding is the module's own padding field.
func workspaceSizing(module, padding string) sectionSpec {
	return sectionSpec{title: "settings-section-sizing", rows: []rowSpec{
		field(module + ".icon-gap"),
		field(module + "." + padding),
		field(module+".icon-size", sizeBase(config.WorkspaceIconBaseRem)),
		field(module+".label-size", sizeBase(config.WorkspaceLabelBaseRem)),
	}}
}

// workspaceSections are the sections from app icons to the actions,
// in the pages' order: the sizing, urgent, mappings, bar display,
// colors and actions, with the mappings and colors the module's own.
func workspaceSections(module string, sizing sectionSpec, mappings []rowSpec, colors []string) []sectionSpec {
	actionRows := make([]rowSpec, 0, 5)
	for _, key := range []string{"left-click", "middle-click", "right-click", "scroll-up", "scroll-down"} {
		actionRows = append(actionRows, field(module+"."+key, actions(workspaceChoices())))
	}
	colorRows := []rowSpec{field(module + ".active-indicator")}
	for _, c := range colors {
		colorRows = append(colorRows, field(module+"."+c))
	}
	return []sectionSpec{
		{title: "settings-section-app-icons", rows: []rowSpec{
			field(module + ".app-icons-show"),
			field(module + ".app-icons-dedupe"),
			field(module+".app-icons-fallback", iconRow),
			field(module+".app-icons-empty", iconRow),
		}},
		sizing,
		{title: "settings-section-urgent", rows: fields(module+".urgent-show", module+".urgent-mode")},
		{title: "settings-section-mappings", rows: mappings},
		{title: "settings-section-bar-display", rows: fields(module + ".border-show")},
		{title: "settings-section-colors", rows: colorRows},
		{title: "settings-section-actions", rows: actionRows},
	}
}

// hyprlandWorkspacesPage is pages/modules/hyprland_workspaces.
func hyprlandWorkspacesPage(*config.Config) pageSpec {
	const m = "modules.hyprland-workspaces"
	return pageSpec{
		id: "hyprland-workspaces", navKey: "settings-nav-hyprland-workspaces", icon: "ld-grid-2x2-symbolic",
		header: "settings-page-hyprland-workspaces",
		sections: append([]sectionSpec{
			{title: "settings-section-general", rows: fields(m+".min-workspace-count", m+".monitor-specific", m+".show-special", m+".highlight-active-on-other-monitor")},
			{title: "settings-section-display", rows: fields(m+".display-mode", m+".label-use-name", m+".numbering", m+".divider")},
		}, workspaceSections(m, workspaceSizing(m, "workspace-padding"), []rowSpec{
			field(m+".workspace-map", workspaceStyleMap(true)),
			field(m+".app-icon-map", stringMap),
			field(m+".workspace-ignore", stringList),
		}, []string{"active-color", "active-on-other-monitor-color", "occupied-color", "empty-color", "container-bg-color", "border-color"})...),
	}
}

// compositorWorkspacesPage is pages/modules/{niri,sway}_workspaces:
// field for field the same page.
func compositorWorkspacesPage(name string) pageSpec {
	m := "modules." + name + "-workspaces"
	return pageSpec{
		id: name + "-workspaces", navKey: "settings-nav-" + name + "-workspaces", icon: "ld-grid-2x2-symbolic",
		header: "settings-page-" + name + "-workspaces",
		sections: append([]sectionSpec{
			{title: "settings-section-general", rows: fields(m+".monitor-specific", m+".hide-trailing-empty")},
			{title: "settings-section-display", rows: fields(m+".display-mode", m+".label-strategy", m+".divider")},
		}, workspaceSections(m, workspaceSizing(m, "workspace-padding"), []rowSpec{
			field(m+".workspace-map", workspaceStyleMap(false)),
			field(m+".app-icon-map", stringMap),
			field(m+".workspace-ignore", stringList),
		}, []string{"active-color", "occupied-color", "empty-color", "container-bg-color", "border-color"})...),
	}
}

// mangoWorkspacesPage is pages/modules/mango_workspaces.
func mangoWorkspacesPage(*config.Config) pageSpec {
	const m = "modules.mango-workspaces"
	return pageSpec{
		id: "mango-workspaces", navKey: "settings-nav-mango-workspaces", icon: "ld-grid-2x2-symbolic",
		header: "settings-page-mango-workspaces",
		sections: append([]sectionSpec{
			{title: "settings-section-general", rows: fields(m+".hide-empty", m+".min-tag-count")},
			{title: "settings-section-display", rows: fields(m+".display-mode", m+".divider")},
		}, workspaceSections(m, sectionSpec{title: "settings-section-sizing", rows: []rowSpec{
			field(m + ".tag-padding"),
			field(m + ".icon-gap"),
			field(m+".icon-size", sizeBase(config.WorkspaceIconBaseRem)),
			field(m+".label-size", sizeBase(config.WorkspaceLabelBaseRem)),
		}}, []rowSpec{
			field(m+".tag-map", workspaceStyleMap(false)),
			field(m+".app-icon-map", stringMap),
		}, []string{"active-color", "occupied-color", "empty-color", "container-bg-color", "border-color"})...),
	}
}
