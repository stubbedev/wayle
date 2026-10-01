package config

// DisplayMode is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// What identifies a workspace in the UI.
type DisplayMode string

// DisplayMode values.
const (
	// Show workspace number or name.
	DisplayModeLabel DisplayMode = "label"
	// Show icon from `workspace-map` (falls back to label if unmapped).
	DisplayModeIcon DisplayMode = "icon"
	// Show nothing - only app icons visible.
	DisplayModeNone DisplayMode = "none"
)

var _ = registerEnum(DisplayModeLabel, DisplayModeIcon, DisplayModeNone)

// Numbering is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// How workspace numbers are displayed.
type Numbering string

// Numbering values.
const (
	// Show actual Hyprland workspace IDs (1, 2, 3, 4, 5, 6...).
	NumberingAbsolute Numbering = "absolute"
	// Show numbers relative to monitor's starting workspace.
	//
	// If monitor has workspaces 4, 5, 6 assigned, they display as 1, 2, 3.
	// Useful when keybinds use per-monitor numbering (Shift+1 for ws 4, etc.).
	NumberingRelative Numbering = "relative"
)

var _ = registerEnum(NumberingAbsolute, NumberingRelative)

// UrgentMode is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// Where the urgent pulse animation is applied.
type UrgentMode string

// UrgentMode values.
const (
	// Pulse the entire workspace.
	UrgentWorkspace UrgentMode = "workspace"
	// Pulse only the app icon(s) belonging to the urgent window.
	//
	// Falls back to `workspace` when app icons are disabled.
	UrgentApplication UrgentMode = "application"
)

var _ = registerEnum(UrgentWorkspace, UrgentApplication)

// ActiveIndicator is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// Visual indicator style for the active workspace.
type ActiveIndicator string

// ActiveIndicator values.
const (
	// Entire button gets a colored background.
	ActiveBackground ActiveIndicator = "background"
	// Small colored bar under the workspace button.
	ActiveUnderline ActiveIndicator = "underline"
)

var _ = registerEnum(ActiveBackground, ActiveUnderline)

// WorkspaceStyle is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// Per-workspace styling override.
type WorkspaceStyle struct {
	// Custom icon for this workspace. When set, the icon is shown regardless
	// of the module's `display-mode`, so a row can mix labelled workspaces
	// with icon-only ones (e.g. `[1][2][icon]`).
	Icon *string `cfg:"icon"`
	// Custom background color for this workspace when active.
	Color *ColorValue `cfg:"color"`
	// Text shown instead of the workspace's name or index.
	Label *string `cfg:"label"`
}

// HyprlandWorkspacesConfig is ported from crates/wayle-config/src/schemas/modules/hyprland_workspaces/mod.rs.
//
// Hyprland workspace indicators with click-to-switch.
type HyprlandWorkspacesConfig struct {
	// Minimum number of workspace buttons to display.
	//
	// When set to 0 (default), only active and occupied workspaces are shown.
	// When set to N, at least N buttons are always visible, with empty ones
	// using `empty-color` styling.
	MinWorkspaceCount uint8 `cfg:"min-workspace-count"`
	// Show only workspaces belonging to the bar's monitor.
	//
	// When true, each bar shows only its monitor's workspaces.
	// When false, all workspaces from all monitors are shown.
	MonitorSpecific bool `cfg:"monitor-specific"`
	// Include special workspaces (scratchpads) in the display.
	//
	// Special workspaces have negative IDs in Hyprland.
	ShowSpecial bool `cfg:"show-special"`
	// Pulse animation on workspaces with urgent windows.
	//
	// When a window requests attention (e.g., terminal bell), the workspace
	// button pulses until you switch to it.
	UrgentShow bool `cfg:"urgent-show"`
	// Where the urgent pulse is applied.
	//
	// - `workspace`: Entire workspace pulses (default)
	// - `application`: Only the app icon(s) belonging to the urgent window
	//   pulse, falling back to `workspace` when app icons are disabled
	UrgentMode UrgentMode `cfg:"urgent-mode"`
	// What identifies each workspace button.
	//
	// - `label`: Shows workspace number (or name if `label-use-name` is true)
	// - `icon`: Shows icon from `workspace-map` (falls back to label if unmapped)
	// - `none`: Shows nothing - only app icons visible
	DisplayMode DisplayMode `cfg:"display-mode"`
	// Use workspace name instead of number when displaying labels.
	//
	// Only applies when `display-mode = "label"` or as fallback for unmapped
	// workspaces in `display-mode = "icon"`.
	LabelUseName bool `cfg:"label-use-name"`
	// How workspace numbers are displayed.
	//
	// - `absolute`: Show actual Hyprland workspace IDs (1, 2, 3, 4, 5, 6...)
	// - `relative`: Show numbers relative to monitor's starting workspace.
	//   If a monitor has workspaces 4, 5, 6 assigned, they display as 1, 2, 3.
	//   Useful when keybinds use per-monitor numbering.
	Numbering Numbering `cfg:"numbering"`
	// Text separator between workspace identity and app icons.
	//
	// Only shown when both `display-mode` is not `none` and `app-icons-show`
	// is enabled. Common values: `"|"`, `"·"`, `"-"`.
	Divider string `cfg:"divider"`
	// Show application icons for windows in each workspace.
	//
	// When enabled, displays icons for running applications.
	// Icons are resolved via `app-icon-map` configuration.
	AppIconsShow bool `cfg:"app-icons-show"`
	// Deduplicate application icons within a workspace.
	//
	// When true, shows only one icon per unique window class.
	// When false, shows an icon for every window.
	AppIconsDedupe bool `cfg:"app-icons-dedupe"`
	// Fallback icon for applications not matched by `app-icon-map`.
	AppIconsFallback string `cfg:"app-icons-fallback"`
	// Icon shown for empty workspaces when `app-icons-show` is enabled.
	//
	// When a workspace has no windows but is displayed (via `min-workspace-count`),
	// this icon appears as a placeholder.
	AppIconsEmpty string `cfg:"app-icons-empty"`
	// Gap between app icons within a workspace button.
	//
	// Only applies to spacing between app icons.
	IconGap Size `cfg:"icon-gap"`
	// Padding for workspace content along the bar direction. Accepts a scale multiplier or pixels (e.g. `"8px"`).
	//
	// For horizontal bars, controls horizontal (left/right) padding.
	// For vertical bars, controls vertical (top/bottom) padding.
	WorkspacePadding Size `cfg:"workspace-padding"`
	// Workspace icon size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	//
	// Applies to workspace identity icons and custom icons from `workspace-map`.
	IconSize Size `cfg:"icon-size"`
	// Workspace label and divider size. Accepts a scale multiplier or pixels (e.g. `"16px"`).
	//
	// Applies to workspace number/name labels and the divider text.
	LabelSize Size `cfg:"label-size"`
	// Workspaces to hide from the display.
	//
	// Glob patterns matching workspace IDs. Examples:
	// - `"10"` - hide workspace 10
	// - `"1?"` - hide workspaces 10-19
	WorkspaceIgnore []string `cfg:"workspace-ignore"`
	// Visual indicator for the active workspace.
	ActiveIndicator ActiveIndicator `cfg:"active-indicator"`
	// Highlight workspaces active on other monitors with a different color.
	//
	// When true, workspaces active on a different monior are highlicted differently.
	// When false, workspaces active on a another monitor are not specially highlighted.
	//
	// This setting only makes sense when `monitor-specific` is false.
	HighlightActiveOnOtherMonitor bool `cfg:"highlight-active-on-other-monitor"`
	// Color for the active (focused) workspace.
	//
	// Applied to icons and labels. In `background` indicator mode,
	// also used as the button background.
	ActiveColor ColorValue `cfg:"active-color"`
	// Color for occupied workspaces (has windows but not focused).
	//
	// Applied to icons and labels.
	OccupiedColor ColorValue `cfg:"occupied-color"`
	// Color for empty workspaces.
	//
	// Applied to the empty placeholder icon and labels.
	EmptyColor ColorValue `cfg:"empty-color"`
	// Background color for the workspaces container.
	ContainerBgColor ColorValue `cfg:"container-bg-color"`
	// Display border around the workspaces container.
	BorderShow bool `cfg:"border-show"`
	// Border color for the workspaces container.
	BorderColor ColorValue `cfg:"border-color"`
	// Active on other minitor indicator color.
	//
	// Only applies when `highlight-active-on-other-monitor` is `true`.
	ActiveOnOtherMonitorColor ColorValue `cfg:"active-on-other-monitor-color"`
	// Per-workspace icon and color overrides.
	//
	// Keys are workspace IDs (use negative for special workspaces).
	//
	// ## Example
	//
	// ```toml
	// [modules.hyprland-workspaces.workspace-map]
	// 1 = { icon = "ld-globe-symbolic", color = "#4a90d9" }
	// 2 = { icon = "ld-terminal-symbolic" }
	// ```
	WorkspaceMap WorkspaceMap `cfg:"workspace-map"`
	// Application icon mapping with glob pattern support.
	//
	// Maps window class or title to symbolic icon names. Supports:
	// - No prefix: Matches window class (e.g., `"*firefox*"`)
	// - `class:` prefix: Explicit class match (e.g., `"class:org.mozilla.*"`)
	// - `title:` prefix: Matches window title (e.g., `"title:*YouTube*"`)
	//
	// User mappings are merged with built-in defaults for common applications.
	//
	// ## Example
	//
	// ```toml
	// [modules.hyprland-workspaces.app-icon-map]
	// "*firefox*" = "ld-globe-symbolic"
	// "title:*YouTube*" = "ld-youtube-symbolic"
	// ```
	AppIconMap map[string]string `cfg:"app-icon-map"`
	// Action on left-clicking a workspace. Default focuses it.
	LeftClick WorkspaceClickAction `cfg:"left-click"`
	// Action on middle-clicking a workspace.
	MiddleClick WorkspaceClickAction `cfg:"middle-click"`
	// Action on right-clicking a workspace.
	RightClick WorkspaceClickAction `cfg:"right-click"`
	// Action on scrolling up over the module. Default focuses the previous workspace.
	ScrollUp WorkspaceClickAction `cfg:"scroll-up"`
	// Action on scrolling down over the module. Default focuses the next workspace.
	ScrollDown WorkspaceClickAction `cfg:"scroll-down"`
}

// DefaultsHyprlandWorkspaces returns the schema defaults.
func DefaultsHyprlandWorkspaces() HyprlandWorkspacesConfig {
	return HyprlandWorkspacesConfig{
		MinWorkspaceCount:             0,
		MonitorSpecific:               true,
		ShowSpecial:                   true,
		UrgentShow:                    true,
		UrgentMode:                    UrgentWorkspace,
		DisplayMode:                   DisplayModeLabel,
		LabelUseName:                  false,
		Numbering:                     NumberingAbsolute,
		Divider:                       " ",
		AppIconsShow:                  false,
		AppIconsDedupe:                true,
		AppIconsFallback:              "ld-app-window-symbolic",
		AppIconsEmpty:                 "tb-minus-symbolic",
		IconGap:                       Size{Value: 0.3, Unit: SizeMultiplier},
		WorkspacePadding:              Size{Value: 0.5, Unit: SizeMultiplier},
		IconSize:                      Size{Value: 1, Unit: SizeMultiplier},
		LabelSize:                     Size{Value: 1, Unit: SizeMultiplier},
		WorkspaceIgnore:               []string{},
		ActiveIndicator:               ActiveBackground,
		HighlightActiveOnOtherMonitor: true,
		ActiveColor:                   mustColor("accent"),
		OccupiedColor:                 mustColor("fg-muted"),
		EmptyColor:                    mustColor("fg-subtle"),
		ContainerBgColor:              mustColor("bg-surface-elevated"),
		BorderShow:                    false,
		BorderColor:                   mustColor("border-default"),
		ActiveOnOtherMonitorColor:     mustColor("accent"),
		WorkspaceMap:                  WorkspaceMap{},
		AppIconMap:                    map[string]string{},
		LeftClick:                     ParseWorkspaceClickAction("focus:this"),
		MiddleClick:                   ParseWorkspaceClickAction(""),
		RightClick:                    ParseWorkspaceClickAction(""),
		ScrollUp:                      ParseWorkspaceClickAction("focus:previous"),
		ScrollDown:                    ParseWorkspaceClickAction("focus:next"),
	}
}

// Clicks returns the five input bindings.
func (c HyprlandWorkspacesConfig) Clicks() WorkspaceClicks {
	return WorkspaceClicks{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}
