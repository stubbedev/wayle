package config

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Types the four workspace modules share
// (crates/wayle-config/src/schemas/modules/{hyprland,niri,sway,mango}_workspaces).

// CSSClass is ActiveIndicator::css_class.
func (a ActiveIndicator) CSSClass() string {
	if a == ActiveUnderline {
		return "indicator-underline"
	}
	return "indicator-background"
}

func (WorkspaceStyle) noStructDefault() {}

// WorkspaceClickKind discriminates a WorkspaceClickAction.
type WorkspaceClickKind int

// Workspace click kinds.
const (
	WorkspaceClickNone WorkspaceClickKind = iota
	WorkspaceClickFocusThis
	WorkspaceClickFocusNext
	WorkspaceClickFocusPrevious
	WorkspaceClickFocusLast
	WorkspaceClickDropdown
	WorkspaceClickShell
)

// WorkspaceClickAction is one workspace-module binding: focus:this,
// focus:next, focus:previous, focus:last, dropdown:NAME, a shell
// command, or empty for none (niri_workspaces WorkspaceClickAction).
type WorkspaceClickAction struct {
	Kind WorkspaceClickKind
	// Arg is the dropdown name or the shell command.
	Arg string
}

// ParseWorkspaceClickAction parses the schema string; like the Rust
// from_str it never fails, anything unrecognized is a shell command.
func ParseWorkspaceClickAction(s string) WorkspaceClickAction {
	switch s {
	case "":
		return WorkspaceClickAction{}
	case "focus:this":
		return WorkspaceClickAction{Kind: WorkspaceClickFocusThis}
	case "focus:next":
		return WorkspaceClickAction{Kind: WorkspaceClickFocusNext}
	case "focus:previous":
		return WorkspaceClickAction{Kind: WorkspaceClickFocusPrevious}
	case "focus:last":
		return WorkspaceClickAction{Kind: WorkspaceClickFocusLast}
	}
	if name, ok := strings.CutPrefix(s, "dropdown:"); ok {
		return WorkspaceClickAction{Kind: WorkspaceClickDropdown, Arg: name}
	}
	return WorkspaceClickAction{Kind: WorkspaceClickShell, Arg: s}
}

// String serializes back to the schema form.
func (a WorkspaceClickAction) String() string {
	switch a.Kind {
	case WorkspaceClickFocusThis:
		return "focus:this"
	case WorkspaceClickFocusNext:
		return "focus:next"
	case WorkspaceClickFocusPrevious:
		return "focus:previous"
	case WorkspaceClickFocusLast:
		return "focus:last"
	case WorkspaceClickDropdown:
		return "dropdown:" + a.Arg
	case WorkspaceClickShell:
		return a.Arg
	}
	return ""
}

// UnmarshalConfig implements Unmarshaler.
func (a *WorkspaceClickAction) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	*a = ParseWorkspaceClickAction(s)
	return nil
}

// MarshalConfig implements Marshaler.
func (a WorkspaceClickAction) MarshalConfig() any { return a.String() }

func (WorkspaceClickAction) configSchema(*schemaGen) Schema {
	return Schema{
		"type":        "string",
		"description": "Click/scroll action: focus:this | focus:next | focus:previous | focus:last | dropdown:NAME | shell command | empty for none",
	}
}

// WorkspaceClicks carries a workspace module's five bindings.
type WorkspaceClicks struct {
	LeftClick   WorkspaceClickAction
	RightClick  WorkspaceClickAction
	MiddleClick WorkspaceClickAction
	ScrollUp    WorkspaceClickAction
	ScrollDown  WorkspaceClickAction
}

// WorkspaceMap is hyprland's workspace-map: styles keyed by numeric
// workspace id, the TOML string keys parsed as i32.
//
// Per-workspace icon and color overrides, keyed by workspace ID.
//
// TOML table keys are always strings, so `"1"` parses into the workspace
// with ID `1`. Negative IDs refer to Hyprland's special workspaces. Keys
// that don't appear in the map fall back to the default behaviour set by
// [`HyprlandWorkspacesConfig::display_mode`].
//
// ## Examples
//
// ```toml
// [modules.hyprland-workspaces.workspace-map]
// # Whole entry on one line with an inline table
// 1 = { label = "Web", icon = "ld-globe-symbolic", color = "#4a90d9" }
// 2 = { icon = "ld-terminal-symbolic" }
// 3 = { icon = "ld-code-symbolic", color = "accent" }
//
// # Or spread the entry across its own subtable
// [modules.hyprland-workspaces.workspace-map.4]
// label = "Chat"
// icon = "ld-message-square-symbolic"
// color = "status-success"
//
// # Negative IDs target Hyprland special workspaces
// [modules.hyprland-workspaces.workspace-map.-99]
// icon = "ld-scratch-symbolic"
// ```
type WorkspaceMap map[int32]WorkspaceStyle

// UnmarshalConfig implements Unmarshaler: a table of styles whose keys
// must parse as i32.
func (m *WorkspaceMap) UnmarshalConfig(v any) error {
	var byName map[string]WorkspaceStyle
	if err := decodeInto(valueOf(&byName), v); err != nil {
		return err
	}
	out := make(WorkspaceMap, len(byName))
	for key, style := range byName {
		id, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			return parseIntError(key, err)
		}
		out[int32(id)] = style
	}
	*m = out
	return nil
}

// parseIntError renders strconv's failure as Rust's ParseIntError.
func parseIntError(key string, err error) error {
	switch {
	case key == "":
		return errors.New("cannot parse integer from empty string")
	case strings.Contains(err.Error(), "out of range"):
		if strings.HasPrefix(key, "-") {
			return errors.New("number too small to fit in target type")
		}
		return errors.New("number too large to fit in target type")
	}
	return errors.New("invalid digit found in string")
}

// MarshalConfig implements Marshaler: keys in numeric order, as the
// Rust BTreeMap<i32, _> iterates them.
func (m WorkspaceMap) MarshalConfig() any {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	out := newTable()
	for _, id := range ids {
		out.set(strconv.Itoa(id), encodeValue(valueOf(new(m[int32(id)]))))
	}
	return out
}

func (WorkspaceMap) configSchema(g *schemaGen) Schema {
	return Schema{
		"type":                 "object",
		"additionalProperties": false,
		"patternProperties":    Schema{`^-?\d+$`: g.subschema(typeOf[WorkspaceStyle]())},
		"description":          typeDoc(typeOf[WorkspaceMap]()),
	}
}

// Lookup returns the style of one workspace id.
func (m WorkspaceMap) Lookup(id int32) (WorkspaceStyle, bool) {
	s, ok := m[id]
	return s, ok
}

// NamedWorkspaceMap is the niri/sway workspace-map: styles keyed by
// workspace name or stable id. Its schema shares the WorkspaceMap
// definition, as the two Rust types share one name.
type NamedWorkspaceMap map[string]WorkspaceStyle

func (NamedWorkspaceMap) configSchemaName() string { return "WorkspaceMap" }

func (NamedWorkspaceMap) configSchema(g *schemaGen) Schema {
	return Schema{"type": "object", "additionalProperties": g.subschema(typeOf[WorkspaceStyle]())}
}

// CompositorWorkspacesConfig is the view the sway and niri workspace
// modules share: their two schemas are field-for-field identical.
type CompositorWorkspacesConfig struct {
	MonitorSpecific   bool
	HideTrailingEmpty bool
	DisplayMode       DisplayMode
	LabelStrategy     LabelStrategy
	UrgentShow        bool
	UrgentMode        UrgentMode
	ActiveIndicator   ActiveIndicator
	Divider           string
	AppIconsShow      bool
	AppIconsDedupe    bool
	AppIconsFallback  string
	AppIconsEmpty     string
	IconGap           Size
	WorkspacePad      Size
	IconSize          Size
	LabelSize         Size
	WorkspaceIgnore   []string
	ActiveColor       ColorValue
	OccupiedColor     ColorValue
	EmptyColor        ColorValue
	ContainerBgColor  ColorValue
	BorderShow        bool
	BorderColor       ColorValue
	WorkspaceMap      NamedWorkspaceMap
	AppIconMap        map[string]string
	Click             WorkspaceClicks
}

// View returns the shared sway/niri view.
func (c SwayWorkspacesConfig) View() CompositorWorkspacesConfig {
	return CompositorWorkspacesConfig{
		c.MonitorSpecific, c.HideTrailingEmpty, c.DisplayMode, c.LabelStrategy,
		c.UrgentShow, c.UrgentMode, c.ActiveIndicator, c.Divider,
		c.AppIconsShow, c.AppIconsDedupe, c.AppIconsFallback, c.AppIconsEmpty,
		c.IconGap, c.WorkspacePadding, c.IconSize, c.LabelSize, c.WorkspaceIgnore,
		c.ActiveColor, c.OccupiedColor, c.EmptyColor, c.ContainerBgColor,
		c.BorderShow, c.BorderColor, c.WorkspaceMap, c.AppIconMap, c.Clicks(),
	}
}

// View returns the shared sway/niri view.
func (c NiriWorkspacesConfig) View() CompositorWorkspacesConfig {
	return CompositorWorkspacesConfig{
		c.MonitorSpecific, c.HideTrailingEmpty, c.DisplayMode, c.LabelStrategy,
		c.UrgentShow, c.UrgentMode, c.ActiveIndicator, c.Divider,
		c.AppIconsShow, c.AppIconsDedupe, c.AppIconsFallback, c.AppIconsEmpty,
		c.IconGap, c.WorkspacePadding, c.IconSize, c.LabelSize, c.WorkspaceIgnore,
		c.ActiveColor, c.OccupiedColor, c.EmptyColor, c.ContainerBgColor,
		c.BorderShow, c.BorderColor, c.WorkspaceMap, c.AppIconMap, c.Clicks(),
	}
}

// View returns the hyprland module's fields in the shared shape; the
// numeric workspace map carries over with decimal keys.
func (c HyprlandWorkspacesConfig) View() CompositorWorkspacesConfig {
	named := make(NamedWorkspaceMap, len(c.WorkspaceMap))
	for id, style := range c.WorkspaceMap {
		named[strconv.Itoa(int(id))] = style
	}
	return CompositorWorkspacesConfig{
		MonitorSpecific: c.MonitorSpecific, DisplayMode: c.DisplayMode,
		UrgentShow: c.UrgentShow, UrgentMode: c.UrgentMode, ActiveIndicator: c.ActiveIndicator,
		Divider: c.Divider, AppIconsShow: c.AppIconsShow, AppIconsDedupe: c.AppIconsDedupe,
		AppIconsFallback: c.AppIconsFallback, AppIconsEmpty: c.AppIconsEmpty,
		IconGap: c.IconGap, WorkspacePad: c.WorkspacePadding, IconSize: c.IconSize,
		LabelSize: c.LabelSize, WorkspaceIgnore: c.WorkspaceIgnore,
		ActiveColor: c.ActiveColor, OccupiedColor: c.OccupiedColor, EmptyColor: c.EmptyColor,
		ContainerBgColor: c.ContainerBgColor, BorderShow: c.BorderShow, BorderColor: c.BorderColor,
		WorkspaceMap: named, AppIconMap: c.AppIconMap, Click: c.Clicks(),
	}
}

// AppIconMapKeys returns the app-icon-map patterns in the order the
// Rust BTreeMap iterates them: sorted.
func (c CompositorWorkspacesConfig) AppIconMapKeys() []string {
	keys := make([]string, 0, len(c.AppIconMap))
	for key := range c.AppIconMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// View returns the mango module in the shared workspace shape; tags
// are its workspaces.
func (c MangoWorkspacesConfig) View() CompositorWorkspacesConfig {
	return CompositorWorkspacesConfig{
		DisplayMode: c.DisplayMode, UrgentShow: c.UrgentShow, UrgentMode: c.UrgentMode,
		ActiveIndicator: c.ActiveIndicator, Divider: c.Divider, AppIconsShow: c.AppIconsShow,
		AppIconsDedupe: c.AppIconsDedupe, AppIconsFallback: c.AppIconsFallback,
		AppIconsEmpty: c.AppIconsEmpty, IconGap: c.IconGap, WorkspacePad: c.TagPadding,
		IconSize: c.IconSize, LabelSize: c.LabelSize, ActiveColor: c.ActiveColor,
		OccupiedColor: c.OccupiedColor, EmptyColor: c.EmptyColor,
		ContainerBgColor: c.ContainerBgColor, BorderShow: c.BorderShow,
		BorderColor: c.BorderColor, WorkspaceMap: NamedWorkspaceMap(c.TagMap),
		AppIconMap: c.AppIconMap, Click: c.Clicks(),
	}
}
