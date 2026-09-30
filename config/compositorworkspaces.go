package config

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// LabelStrategy selects what a sway/niri workspace button's label
// shows (the schema's LabelStrategy).
type LabelStrategy string

// Label strategies.
const (
	LabelIndex        LabelStrategy = "index"
	LabelNameOrIndex  LabelStrategy = "name-or-index"
	LabelNameOnly     LabelStrategy = "name-only"
	LabelIndexAndName LabelStrategy = "index-and-name"
)

// UrgentMode selects where urgency shows: the whole workspace blinks,
// or only the urgent application's icon.
type UrgentMode string

// Urgent modes.
const (
	UrgentWorkspace   UrgentMode = "workspace"
	UrgentApplication UrgentMode = "application"
)

// CSSClass is ActiveIndicator::css_class.
func (a ActiveIndicator) CSSClass() string {
	if a == ActiveUnderline {
		return "indicator-underline"
	}
	return "indicator-background"
}

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
// command, or empty for none (the schema's WorkspaceClickAction).
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
	if name, ok := cutPrefix(s, "dropdown:"); ok {
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

// WorkspaceClicks carries a workspace module's five bindings.
type WorkspaceClicks struct {
	LeftClick   WorkspaceClickAction
	MiddleClick WorkspaceClickAction
	RightClick  WorkspaceClickAction
	ScrollUp    WorkspaceClickAction
	ScrollDown  WorkspaceClickAction
}

// DefaultWorkspaceClicks is the bindings every workspace schema
// defaults to: left focuses the clicked workspace, scroll walks them.
func DefaultWorkspaceClicks() WorkspaceClicks {
	return WorkspaceClicks{
		LeftClick:  WorkspaceClickAction{Kind: WorkspaceClickFocusThis},
		ScrollUp:   WorkspaceClickAction{Kind: WorkspaceClickFocusPrevious},
		ScrollDown: WorkspaceClickAction{Kind: WorkspaceClickFocusNext},
	}
}

// NamedWorkspaceStyle is one workspace-map entry of the name-keyed
// maps (sway, niri): a label, icon, and color override.
type NamedWorkspaceStyle struct {
	Label string
	Icon  string
	Color ColorValue
	// LabelSet/ColorSet distinguish unset from empty/transparent.
	LabelSet bool
	ColorSet bool
}

// CompositorWorkspacesConfig is the sway-workspaces and niri-workspaces
// module config: the two schemas are field-for-field identical and
// differ only in the hide-trailing-empty default.
type CompositorWorkspacesConfig struct {
	MonitorSpecific   bool
	HideTrailingEmpty bool
	DisplayMode       WorkspacesDisplayMode
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
	// WorkspaceMap is keyed by workspace name or stable id.
	WorkspaceMap map[string]NamedWorkspaceStyle
	// AppIconMap maps app-id globs (or title:/app: prefixed globs) to
	// icon names, tried in key order.
	AppIconMap map[string]string
	Click      WorkspaceClicks
}

// DefaultsSwayWorkspaces returns the sway-workspaces schema defaults.
func DefaultsSwayWorkspaces() CompositorWorkspacesConfig {
	return defaultsCompositorWorkspaces(false)
}

// DefaultsNiriWorkspaces returns the niri-workspaces schema defaults:
// niri's dynamic model always carries a trailing empty workspace, so
// hiding it is the default.
func DefaultsNiriWorkspaces() CompositorWorkspacesConfig {
	return defaultsCompositorWorkspaces(true)
}

func defaultsCompositorWorkspaces(hideTrailing bool) CompositorWorkspacesConfig {
	return CompositorWorkspacesConfig{
		MonitorSpecific:   true,
		HideTrailingEmpty: hideTrailing,
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
		WorkspacePad:      Size{Value: 0.5, Unit: SizeMultiplier},
		IconSize:          Size{Value: 1.0, Unit: SizeMultiplier},
		LabelSize:         Size{Value: 1.0, Unit: SizeMultiplier},
		WorkspaceIgnore:   []string{},
		ActiveColor:       mustColor("accent"),
		OccupiedColor:     mustColor("fg-muted"),
		EmptyColor:        mustColor("fg-subtle"),
		ContainerBgColor:  mustColor("bg-surface-elevated"),
		BorderShow:        false,
		BorderColor:       mustColor("border-default"),
		WorkspaceMap:      map[string]NamedWorkspaceStyle{},
		AppIconMap:        map[string]string{},
		Click:             DefaultWorkspaceClicks(),
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

// workspaceClicksDoc is the binding keys every workspace schema shares.
type workspaceClicksDoc struct {
	LeftClick   *string `toml:"left-click"`
	MiddleClick *string `toml:"middle-click"`
	RightClick  *string `toml:"right-click"`
	ScrollUp    *string `toml:"scroll-up"`
	ScrollDown  *string `toml:"scroll-down"`
}

func (d workspaceClicksDoc) apply(clicks *WorkspaceClicks) {
	for _, entry := range []struct {
		raw    *string
		target *WorkspaceClickAction
	}{
		{d.LeftClick, &clicks.LeftClick},
		{d.MiddleClick, &clicks.MiddleClick},
		{d.RightClick, &clicks.RightClick},
		{d.ScrollUp, &clicks.ScrollUp},
		{d.ScrollDown, &clicks.ScrollDown},
	} {
		if entry.raw != nil {
			*entry.target = ParseWorkspaceClickAction(*entry.raw)
		}
	}
}

// parseColorKey parses one optional color key into target.
func parseColorKey(module, key, raw string, target *ColorValue) error {
	if raw == "" {
		return nil
	}
	cv, err := ParseColorValue(raw)
	if err != nil {
		return fmt.Errorf("%s: %s: %w", module, key, err)
	}
	*target = cv
	return nil
}

// parseSizeKey parses one optional size key into target.
func parseSizeKey(module, key string, raw tomlValue, target *Size) error {
	if raw.value == nil {
		return nil
	}
	if err := target.unmarshal(raw.value, key); err != nil {
		return fmt.Errorf("%s: %w", module, err)
	}
	return nil
}

// applyCompositorWorkspaces overlays [modules.sway-workspaces] or
// [modules.niri-workspaces] on the module's defaults.
func applyCompositorWorkspaces(md toml.MetaData, prim toml.Primitive, module string, cfg CompositorWorkspacesConfig) (CompositorWorkspacesConfig, error) {
	var doc struct {
		workspaceClicksDoc
		MonitorSpecific   *bool                `toml:"monitor-specific"`
		HideTrailingEmpty *bool                `toml:"hide-trailing-empty"`
		DisplayMode       *string              `toml:"display-mode"`
		LabelStrategy     *string              `toml:"label-strategy"`
		UrgentShow        *bool                `toml:"urgent-show"`
		UrgentMode        *string              `toml:"urgent-mode"`
		ActiveIndicator   *string              `toml:"active-indicator"`
		Divider           *string              `toml:"divider"`
		AppIconsShow      *bool                `toml:"app-icons-show"`
		AppIconsDedupe    *bool                `toml:"app-icons-dedupe"`
		AppIconsFallback  *string              `toml:"app-icons-fallback"`
		AppIconsEmpty     *string              `toml:"app-icons-empty"`
		IconGap           tomlValue            `toml:"icon-gap"`
		WorkspacePad      tomlValue            `toml:"workspace-padding"`
		IconSize          tomlValue            `toml:"icon-size"`
		LabelSize         tomlValue            `toml:"label-size"`
		WorkspaceIgnore   []string             `toml:"workspace-ignore"`
		ActiveColor       string               `toml:"active-color"`
		OccupiedColor     string               `toml:"occupied-color"`
		EmptyColor        string               `toml:"empty-color"`
		ContainerBgColor  string               `toml:"container-bg-color"`
		BorderShow        *bool                `toml:"border-show"`
		BorderColor       string               `toml:"border-color"`
		WorkspaceMap      map[string]tomlValue `toml:"workspace-map"`
		AppIconMap        map[string]string    `toml:"app-icon-map"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, fmt.Errorf("%s: %w", module, err)
	}
	for _, b := range []struct {
		raw    *bool
		target *bool
	}{
		{doc.MonitorSpecific, &cfg.MonitorSpecific},
		{doc.HideTrailingEmpty, &cfg.HideTrailingEmpty},
		{doc.UrgentShow, &cfg.UrgentShow},
		{doc.AppIconsShow, &cfg.AppIconsShow},
		{doc.AppIconsDedupe, &cfg.AppIconsDedupe},
		{doc.BorderShow, &cfg.BorderShow},
	} {
		if b.raw != nil {
			*b.target = *b.raw
		}
	}
	for _, s := range []struct {
		raw    *string
		target *string
	}{
		{doc.Divider, &cfg.Divider},
		{doc.AppIconsFallback, &cfg.AppIconsFallback},
		{doc.AppIconsEmpty, &cfg.AppIconsEmpty},
	} {
		if s.raw != nil {
			*s.target = *s.raw
		}
	}
	if doc.DisplayMode != nil {
		cfg.DisplayMode = WorkspacesDisplayMode(*doc.DisplayMode)
	}
	if doc.LabelStrategy != nil {
		cfg.LabelStrategy = LabelStrategy(*doc.LabelStrategy)
	}
	if doc.UrgentMode != nil {
		cfg.UrgentMode = UrgentMode(*doc.UrgentMode)
	}
	if doc.ActiveIndicator != nil {
		cfg.ActiveIndicator = ActiveIndicator(*doc.ActiveIndicator)
	}
	for _, s := range []struct {
		key    string
		raw    tomlValue
		target *Size
	}{
		{"icon-gap", doc.IconGap, &cfg.IconGap},
		{"workspace-padding", doc.WorkspacePad, &cfg.WorkspacePad},
		{"icon-size", doc.IconSize, &cfg.IconSize},
		{"label-size", doc.LabelSize, &cfg.LabelSize},
	} {
		if err := parseSizeKey(module, s.key, s.raw, s.target); err != nil {
			return cfg, err
		}
	}
	if doc.WorkspaceIgnore != nil {
		cfg.WorkspaceIgnore = doc.WorkspaceIgnore
	}
	for _, c := range []struct {
		key    string
		raw    string
		target *ColorValue
	}{
		{"active-color", doc.ActiveColor, &cfg.ActiveColor},
		{"occupied-color", doc.OccupiedColor, &cfg.OccupiedColor},
		{"empty-color", doc.EmptyColor, &cfg.EmptyColor},
		{"container-bg-color", doc.ContainerBgColor, &cfg.ContainerBgColor},
		{"border-color", doc.BorderColor, &cfg.BorderColor},
	} {
		if err := parseColorKey(module, c.key, c.raw, c.target); err != nil {
			return cfg, err
		}
	}
	if doc.WorkspaceMap != nil {
		cfg.WorkspaceMap = map[string]NamedWorkspaceStyle{}
		for key, raw := range doc.WorkspaceMap {
			style, err := parseNamedWorkspaceStyle(raw.value)
			if err != nil {
				return cfg, fmt.Errorf("%s: workspace-map[%s]: %w", module, key, err)
			}
			cfg.WorkspaceMap[key] = style
		}
	}
	if doc.AppIconMap != nil {
		cfg.AppIconMap = doc.AppIconMap
	}
	doc.apply(&cfg.Click)

	switch cfg.DisplayMode {
	case DisplayModeLabel, DisplayModeIcon, DisplayModeNone:
	default:
		return cfg, fmt.Errorf("%s: invalid display-mode %q (want label|icon|none)", module, cfg.DisplayMode)
	}
	switch cfg.LabelStrategy {
	case LabelIndex, LabelNameOrIndex, LabelNameOnly, LabelIndexAndName:
	default:
		return cfg, fmt.Errorf("%s: invalid label-strategy %q (want index|name-or-index|name-only|index-and-name)", module, cfg.LabelStrategy)
	}
	if cfg.UrgentMode != UrgentWorkspace && cfg.UrgentMode != UrgentApplication {
		return cfg, fmt.Errorf("%s: invalid urgent-mode %q (want workspace|application)", module, cfg.UrgentMode)
	}
	if cfg.ActiveIndicator != ActiveBackground && cfg.ActiveIndicator != ActiveUnderline {
		return cfg, fmt.Errorf("%s: invalid active-indicator %q (want background|underline)", module, cfg.ActiveIndicator)
	}
	return cfg, nil
}

// parseNamedWorkspaceStyle decodes one { label, icon, color } table.
func parseNamedWorkspaceStyle(value any) (NamedWorkspaceStyle, error) {
	style := NamedWorkspaceStyle{}
	table, ok := value.(map[string]any)
	if !ok {
		return style, fmt.Errorf("want a table, got %T", value)
	}
	for key, v := range table {
		// Unknown keys are ignored, as serde does without
		// deny_unknown_fields.
		if key != "label" && key != "icon" && key != "color" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return style, fmt.Errorf("%s: want a string, got %T", key, v)
		}
		switch key {
		case "label":
			style.Label, style.LabelSet = s, true
		case "icon":
			style.Icon = s
		case "color":
			cv, err := ParseColorValue(s)
			if err != nil {
				return style, fmt.Errorf("color: %w", err)
			}
			style.Color, style.ColorSet = cv, true
		}
	}
	return style, nil
}
