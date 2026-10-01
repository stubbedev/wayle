package config

// LabelStrategy is ported from crates/wayle-config/src/schemas/modules/niri_workspaces/mod.rs.
//
// What identifies each workspace's label.
type LabelStrategy string

// LabelStrategy values.
const (
	// Always show the index (e.g. `1`, `2`, `3`).
	LabelIndex LabelStrategy = "index"
	// Show the name when set, fall back to the index.
	LabelNameOrIndex LabelStrategy = "name-or-index"
	// Show only the name; unnamed workspaces show nothing.
	LabelNameOnly LabelStrategy = "name-only"
	// Show both, joined as `"1: web"`. Unnamed workspaces show the index alone.
	LabelIndexAndName LabelStrategy = "index-and-name"
)

var _ = registerEnum(LabelIndex, LabelNameOrIndex, LabelNameOnly, LabelIndexAndName)

// NiriWorkspacesConfig is ported from crates/wayle-config/src/schemas/modules/niri_workspaces/mod.rs.
//
// Niri workspace indicators with click-to-switch.
type NiriWorkspacesConfig struct {
	// Show only workspaces on this bar's monitor.
	//
	// When `true` (default), each bar shows only its own output's
	// workspaces. When `false`, all workspaces from every output are shown.
	MonitorSpecific bool `cfg:"monitor-specific"`
	// Hide niri's auto-allocated trailing empty workspace.
	//
	// Niri keeps one empty workspace at the tail of every output for
	// dynamic allocation.
	HideTrailingEmpty bool `cfg:"hide-trailing-empty"`
	// What identifies each workspace button.
	//
	// - `label` (default): show the workspace label per `label-strategy`
	// - `icon`: show an icon from `workspace-map` (falls back to label if unmapped)
	// - `none`: show nothing — only app icons visible (if enabled)
	DisplayMode DisplayMode `cfg:"display-mode"`
	// How to compose the workspace label when `display-mode = "label"`.
	//
	// - `index`: index only (`"1"`, `"2"`)
	// - `name-or-index` (default): name when set, index otherwise
	// - `name-only`: name only; unnamed workspaces show nothing
	// - `index-and-name`: `"1: web"` form; unnamed workspaces show the index alone
	LabelStrategy LabelStrategy `cfg:"label-strategy"`
	// Pulse animation on workspaces with urgent windows.
	UrgentShow bool `cfg:"urgent-show"`
	// Where the urgent pulse is applied.
	//
	// - `workspace` (default): whole button pulses
	// - `application`: only the urgent app icon pulses, falling back to
	//   `workspace` when app icons are disabled
	UrgentMode UrgentMode `cfg:"urgent-mode"`
	// Visual indicator for the active workspace.
	ActiveIndicator ActiveIndicator `cfg:"active-indicator"`
	// Text separator between workspace identity and app icons.
	Divider string `cfg:"divider"`
	// Show application icons for windows on each workspace.
	AppIconsShow bool `cfg:"app-icons-show"`
	// Deduplicate application icons within a workspace.
	//
	// When `true`, one icon per unique `app_id`. When `false`, one icon
	// per window.
	AppIconsDedupe bool `cfg:"app-icons-dedupe"`
	// Fallback icon for applications not matched by `app-icon-map`.
	AppIconsFallback string `cfg:"app-icons-fallback"`
	// Icon shown when a workspace has no application windows.
	AppIconsEmpty string `cfg:"app-icons-empty"`
	// Gap between app icons within a workspace button. Accepts a scale multiplier or pixels (e.g. `"4px"`).
	IconGap Size `cfg:"icon-gap"`
	// Padding for workspace content along the bar direction. Accepts a scale multiplier or pixels (e.g. `"8px"`).
	WorkspacePadding Size `cfg:"workspace-padding"`
	// Workspace icon size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	//
	// Applies to identity icons and custom icons from `workspace-map`.
	IconSize Size `cfg:"icon-size"`
	// Workspace label and divider size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	LabelSize Size `cfg:"label-size"`
	// Workspaces to hide from the display.
	//
	// Glob patterns matched against the workspace's name (if set), then
	// its index, then its stable id. Examples:
	// - `"scratch"` — hide the workspace named `scratch`
	// - `"1?"` — hide indices 10-19
	WorkspaceIgnore []string `cfg:"workspace-ignore"`
	// Color for the active (visible on its output) workspace.
	//
	// In `background` indicator mode, also used as the button background.
	ActiveColor ColorValue `cfg:"active-color"`
	// Color for occupied workspaces (have windows but not active).
	OccupiedColor ColorValue `cfg:"occupied-color"`
	// Color for empty workspaces and placeholder slots.
	EmptyColor ColorValue `cfg:"empty-color"`
	// Background color for the workspaces container.
	ContainerBgColor ColorValue `cfg:"container-bg-color"`
	// Display border around the workspaces container.
	BorderShow bool `cfg:"border-show"`
	// Border color for the workspaces container.
	BorderColor ColorValue `cfg:"border-color"`
	// Per-workspace icon and color overrides, keyed by name or id-as-string.
	//
	// ## Example
	//
	// ```toml
	// [modules.niri-workspaces.workspace-map]
	// web = { icon = "ld-globe-symbolic", color = "#4a90d9" }
	// terminal = { icon = "ld-terminal-symbolic" }
	// ```
	WorkspaceMap NamedWorkspaceMap `cfg:"workspace-map"`
	// Application icon mapping with glob pattern support.
	//
	// Maps window `app_id` or title to symbolic icon names. Supports:
	// - No prefix: matches `app_id` (e.g. `"*firefox*"`)
	// - `app:` prefix: explicit `app_id` match (e.g. `"app:org.mozilla.*"`)
	// - `title:` prefix: matches window title (e.g. `"title:*YouTube*"`)
	//
	// ## Example
	//
	// ```toml
	// [modules.niri-workspaces.app-icon-map]
	// "*firefox*" = "ld-globe-symbolic"
	// "title:*YouTube*" = "ld-youtube-symbolic"
	// ```
	AppIconMap map[string]string `cfg:"app-icon-map"`
	// Action on left click.
	LeftClick WorkspaceClickAction `cfg:"left-click"`
	// Action on middle click.
	MiddleClick WorkspaceClickAction `cfg:"middle-click"`
	// Action on right click.
	RightClick WorkspaceClickAction `cfg:"right-click"`
	// Action on scroll up.
	ScrollUp WorkspaceClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown WorkspaceClickAction `cfg:"scroll-down"`
}

// DefaultsNiriWorkspaces returns the schema defaults.
func DefaultsNiriWorkspaces() NiriWorkspacesConfig {
	return NiriWorkspacesConfig{
		MonitorSpecific:   true,
		HideTrailingEmpty: true,
		DisplayMode:       DisplayModeLabel,
		LabelStrategy:     LabelNameOrIndex,
		UrgentShow:        true,
		UrgentMode:        UrgentWorkspace,
		ActiveIndicator:   ActiveBackground,
		Divider:           " ",
		AppIconsShow:      false,
		AppIconsDedupe:    true,
		AppIconsFallback:  "ld-app-window-symbolic",
		AppIconsEmpty:     "tb-minus-symbolic",
		IconGap:           Size{Value: 0.3, Unit: SizeMultiplier},
		WorkspacePadding:  Size{Value: 0.5, Unit: SizeMultiplier},
		IconSize:          Size{Value: 1, Unit: SizeMultiplier},
		LabelSize:         Size{Value: 1, Unit: SizeMultiplier},
		WorkspaceIgnore:   []string{},
		ActiveColor:       mustColor("accent"),
		OccupiedColor:     mustColor("fg-muted"),
		EmptyColor:        mustColor("fg-subtle"),
		ContainerBgColor:  mustColor("bg-surface-elevated"),
		BorderShow:        false,
		BorderColor:       mustColor("border-default"),
		WorkspaceMap:      NamedWorkspaceMap{},
		AppIconMap:        map[string]string{},
		LeftClick:         ParseWorkspaceClickAction("focus:this"),
		MiddleClick:       ParseWorkspaceClickAction(""),
		RightClick:        ParseWorkspaceClickAction(""),
		ScrollUp:          ParseWorkspaceClickAction("focus:previous"),
		ScrollDown:        ParseWorkspaceClickAction("focus:next"),
	}
}

// Clicks returns the five input bindings.
func (c NiriWorkspacesConfig) Clicks() WorkspaceClicks {
	return WorkspaceClicks{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
