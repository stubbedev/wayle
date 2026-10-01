package config

// MangoWorkspacesConfig is ported from crates/wayle-config/src/schemas/modules/mango_workspaces/mod.rs.
//
// MangoWM tag switcher module configuration.
type MangoWorkspacesConfig struct {
	// Hide tags that hold no clients and are not active.
	HideEmpty bool `cfg:"hide-empty"`
	// Always show tags up to this one-based index, even when empty.
	//
	// `0` shows only occupied or active tags (subject to `hide-empty`). A
	// value above the compositor's tag count just shows every tag.
	MinTagCount uint8 `cfg:"min-tag-count"`
	// What identifies each tag: its label, an icon, or nothing.
	DisplayMode DisplayMode `cfg:"display-mode"`
	// Text shown between the tag label and its application icons.
	Divider string `cfg:"divider"`
	// Show an application icon per client on each tag.
	AppIconsShow bool `cfg:"app-icons-show"`
	// Collapse clients that share an application to a single icon.
	AppIconsDedupe bool `cfg:"app-icons-dedupe"`
	// Icon for clients not matched by `app-icon-map`.
	AppIconsFallback string `cfg:"app-icons-fallback"`
	// Icon shown when a tag has no clients.
	AppIconsEmpty string `cfg:"app-icons-empty"`
	// Highlight tags whose clients requested attention.
	UrgentShow bool `cfg:"urgent-show"`
	// Whether urgency is tracked per tag or per application.
	UrgentMode UrgentMode `cfg:"urgent-mode"`
	// How the active tag is marked.
	ActiveIndicator ActiveIndicator `cfg:"active-indicator"`
	// Padding around each tag button, in rem.
	TagPadding Size `cfg:"tag-padding"`
	// Spacing between application icons. Accepts a scale multiplier or pixels (e.g. `"4px"`).
	IconGap Size `cfg:"icon-gap"`
	// Application icon size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	IconSize Size `cfg:"icon-size"`
	// Tag label text size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	LabelSize Size `cfg:"label-size"`
	// Color of the active tag.
	ActiveColor ColorValue `cfg:"active-color"`
	// Color of tags that hold clients but are not active.
	OccupiedColor ColorValue `cfg:"occupied-color"`
	// Color of empty tags.
	EmptyColor ColorValue `cfg:"empty-color"`
	// Background color of the tag container.
	ContainerBgColor ColorValue `cfg:"container-bg-color"`
	// Draw a border around the tag container.
	BorderShow bool `cfg:"border-show"`
	// Border color when the border is shown.
	BorderColor ColorValue `cfg:"border-color"`
	// Window-to-icon mappings for the application icons.
	//
	// Keys are glob patterns matched against a client's app id, or `title:`
	// patterns matched against its title. Values are symbolic icon names.
	//
	// ## Example
	//
	// ```toml
	// [modules.mango-workspaces.app-icon-map]
	// "*firefox*" = "ld-globe-symbolic"
	// "title:*YouTube*" = "si-youtube-symbolic"
	// ```
	AppIconMap map[string]string `cfg:"app-icon-map"`
	// Per-tag icon and color overrides, keyed by one-based tag index.
	//
	// ## Example
	//
	// ```toml
	// [modules.mango-workspaces.tag-map.1]
	// label = "web"
	// icon = "ld-globe-symbolic"
	// color = "#4a90d9"
	//
	// [modules.mango-workspaces.tag-map.2]
	// label = "term"
	// icon = "ld-terminal-symbolic"
	// ```
	TagMap map[string]WorkspaceStyle `cfg:"tag-map"`
	// Action for a left click on a tag.
	LeftClick WorkspaceClickAction `cfg:"left-click"`
	// Action for a middle click on a tag.
	MiddleClick WorkspaceClickAction `cfg:"middle-click"`
	// Action for a right click on a tag.
	RightClick WorkspaceClickAction `cfg:"right-click"`
	// Action for scrolling up over the tag container.
	ScrollUp WorkspaceClickAction `cfg:"scroll-up"`
	// Action for scrolling down over the tag container.
	ScrollDown WorkspaceClickAction `cfg:"scroll-down"`
}

// DefaultsMangoWorkspaces returns the schema defaults.
func DefaultsMangoWorkspaces() MangoWorkspacesConfig {
	return MangoWorkspacesConfig{
		HideEmpty:        true,
		MinTagCount:      0,
		DisplayMode:      DisplayModeLabel,
		Divider:          " ",
		AppIconsShow:     false,
		AppIconsDedupe:   true,
		AppIconsFallback: "ld-app-window-symbolic",
		AppIconsEmpty:    "tb-minus-symbolic",
		UrgentShow:       true,
		UrgentMode:       UrgentWorkspace,
		ActiveIndicator:  ActiveBackground,
		TagPadding:       Size{Value: 0.5, Unit: SizeMultiplier},
		IconGap:          Size{Value: 0.3, Unit: SizeMultiplier},
		IconSize:         Size{Value: 1, Unit: SizeMultiplier},
		LabelSize:        Size{Value: 1, Unit: SizeMultiplier},
		ActiveColor:      mustColor("accent"),
		OccupiedColor:    mustColor("fg-muted"),
		EmptyColor:       mustColor("fg-subtle"),
		ContainerBgColor: mustColor("bg-surface-elevated"),
		BorderShow:       false,
		BorderColor:      mustColor("border-default"),
		AppIconMap:       map[string]string{},
		TagMap:           map[string]WorkspaceStyle{},
		LeftClick:        ParseWorkspaceClickAction("focus:this"),
		MiddleClick:      ParseWorkspaceClickAction(""),
		RightClick:       ParseWorkspaceClickAction(""),
		ScrollUp:         ParseWorkspaceClickAction("focus:previous"),
		ScrollDown:       ParseWorkspaceClickAction("focus:next"),
	}
}

// Clicks returns the five input bindings.
func (c MangoWorkspacesConfig) Clicks() WorkspaceClicks {
	return WorkspaceClicks{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
