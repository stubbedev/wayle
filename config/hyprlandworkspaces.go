package config

import (
	"fmt"
	"strconv"

	"github.com/BurntSushi/toml"
)

// WorkspacesDisplayMode selects what a workspace button shows.
type WorkspacesDisplayMode string

// Display modes.
const (
	DisplayModeLabel WorkspacesDisplayMode = "label"
	DisplayModeIcon  WorkspacesDisplayMode = "icon"
	DisplayModeNone  WorkspacesDisplayMode = "none"
)

// WorkspacesNumbering selects how workspace numbers render.
type WorkspacesNumbering string

// Numbering modes.
const (
	NumberingAbsolute WorkspacesNumbering = "absolute"
	NumberingRelative WorkspacesNumbering = "relative"
)

// ActiveIndicator selects how the focused workspace stands out.
type ActiveIndicator string

// Active indicators.
const (
	ActiveBackground ActiveIndicator = "background"
	ActiveUnderline  ActiveIndicator = "underline"
)

// HyprlandWorkspacesConfig is the hyprland-workspaces module config.
// The button styling it shares with the sway/niri schemas lives in
// Shared (MonitorSpecific, the colors, sizes, app icons, urgency,
// workspace-ignore, border, and bindings; WorkspaceMap is keyed by
// the decimal workspace id). The rest is hyprland's own.
type HyprlandWorkspacesConfig struct {
	Shared                        CompositorWorkspacesConfig
	MinWorkspace                  int
	ShowSpecial                   bool
	LabelUseName                  bool
	Numbering                     WorkspacesNumbering
	HighlightActiveOnOtherMonitor bool
	ActiveOnOtherMonitorColor     ColorValue
}

// DefaultsHyprlandWorkspaces returns the schema defaults.
func DefaultsHyprlandWorkspaces() HyprlandWorkspacesConfig {
	shared := defaultsCompositorWorkspaces(false)
	return HyprlandWorkspacesConfig{
		Shared:                        shared,
		MinWorkspace:                  0,
		ShowSpecial:                   true,
		LabelUseName:                  false,
		Numbering:                     NumberingAbsolute,
		HighlightActiveOnOtherMonitor: true,
		ActiveOnOtherMonitorColor:     mustColor("accent"),
	}
}

// applyHyprlandWorkspaces overlays [modules.hyprland-workspaces].
func applyHyprlandWorkspaces(md toml.MetaData, prim toml.Primitive) (HyprlandWorkspacesConfig, error) {
	const module = "hyprland-workspaces"
	cfg := DefaultsHyprlandWorkspaces()
	var doc struct {
		workspaceClicksDoc
		MinWorkspace     *int                 `toml:"min-workspace-count"`
		MonitorSpecific  *bool                `toml:"monitor-specific"`
		ShowSpecial      *bool                `toml:"show-special"`
		UrgentShow       *bool                `toml:"urgent-show"`
		UrgentMode       *string              `toml:"urgent-mode"`
		DisplayMode      *string              `toml:"display-mode"`
		LabelUseName     *bool                `toml:"label-use-name"`
		Numbering        *string              `toml:"numbering"`
		Divider          *string              `toml:"divider"`
		AppIconsShow     *bool                `toml:"app-icons-show"`
		AppIconsDedupe   *bool                `toml:"app-icons-dedupe"`
		AppIconsFallback *string              `toml:"app-icons-fallback"`
		AppIconsEmpty    *string              `toml:"app-icons-empty"`
		IconGap          tomlValue            `toml:"icon-gap"`
		WorkspacePad     tomlValue            `toml:"workspace-padding"`
		IconSize         tomlValue            `toml:"icon-size"`
		LabelSize        tomlValue            `toml:"label-size"`
		WorkspaceIgnore  []string             `toml:"workspace-ignore"`
		ActiveIndicator  *string              `toml:"active-indicator"`
		HighlightOther   *bool                `toml:"highlight-active-on-other-monitor"`
		ActiveColor      string               `toml:"active-color"`
		OccupiedColor    string               `toml:"occupied-color"`
		EmptyColor       string               `toml:"empty-color"`
		ContainerBgColor string               `toml:"container-bg-color"`
		BorderShow       *bool                `toml:"border-show"`
		BorderColor      string               `toml:"border-color"`
		ActiveOtherColor string               `toml:"active-on-other-monitor-color"`
		WorkspaceMap     map[string]tomlValue `toml:"workspace-map"`
		AppIconMap       map[string]string    `toml:"app-icon-map"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, fmt.Errorf("%s: %w", module, err)
	}
	s := &cfg.Shared
	for _, b := range []struct {
		raw    *bool
		target *bool
	}{
		{doc.MonitorSpecific, &s.MonitorSpecific},
		{doc.ShowSpecial, &cfg.ShowSpecial},
		{doc.UrgentShow, &s.UrgentShow},
		{doc.LabelUseName, &cfg.LabelUseName},
		{doc.AppIconsShow, &s.AppIconsShow},
		{doc.AppIconsDedupe, &s.AppIconsDedupe},
		{doc.HighlightOther, &cfg.HighlightActiveOnOtherMonitor},
		{doc.BorderShow, &s.BorderShow},
	} {
		if b.raw != nil {
			*b.target = *b.raw
		}
	}
	for _, str := range []struct {
		raw    *string
		target *string
	}{
		{doc.Divider, &s.Divider},
		{doc.AppIconsFallback, &s.AppIconsFallback},
		{doc.AppIconsEmpty, &s.AppIconsEmpty},
	} {
		if str.raw != nil {
			*str.target = *str.raw
		}
	}
	if doc.MinWorkspace != nil {
		if *doc.MinWorkspace < 0 || *doc.MinWorkspace > 255 {
			return cfg, fmt.Errorf("%s: min-workspace-count %d outside 0-255", module, *doc.MinWorkspace)
		}
		cfg.MinWorkspace = *doc.MinWorkspace
	}
	if doc.DisplayMode != nil {
		s.DisplayMode = WorkspacesDisplayMode(*doc.DisplayMode)
	}
	if doc.UrgentMode != nil {
		s.UrgentMode = UrgentMode(*doc.UrgentMode)
	}
	if doc.Numbering != nil {
		cfg.Numbering = WorkspacesNumbering(*doc.Numbering)
	}
	if doc.ActiveIndicator != nil {
		s.ActiveIndicator = ActiveIndicator(*doc.ActiveIndicator)
	}
	for _, sz := range []struct {
		key    string
		raw    tomlValue
		target *Size
	}{
		{"icon-gap", doc.IconGap, &s.IconGap},
		{"workspace-padding", doc.WorkspacePad, &s.WorkspacePad},
		{"icon-size", doc.IconSize, &s.IconSize},
		{"label-size", doc.LabelSize, &s.LabelSize},
	} {
		if err := parseSizeKey(module, sz.key, sz.raw, sz.target); err != nil {
			return cfg, err
		}
	}
	if doc.WorkspaceIgnore != nil {
		s.WorkspaceIgnore = doc.WorkspaceIgnore
	}
	for _, c := range []struct {
		key    string
		raw    string
		target *ColorValue
	}{
		{"active-color", doc.ActiveColor, &s.ActiveColor},
		{"occupied-color", doc.OccupiedColor, &s.OccupiedColor},
		{"empty-color", doc.EmptyColor, &s.EmptyColor},
		{"container-bg-color", doc.ContainerBgColor, &s.ContainerBgColor},
		{"border-color", doc.BorderColor, &s.BorderColor},
		{"active-on-other-monitor-color", doc.ActiveOtherColor, &cfg.ActiveOnOtherMonitorColor},
	} {
		if err := parseColorKey(module, c.key, c.raw, c.target); err != nil {
			return cfg, err
		}
	}
	if doc.WorkspaceMap != nil {
		s.WorkspaceMap = map[string]NamedWorkspaceStyle{}
		for key, raw := range doc.WorkspaceMap {
			// WorkspaceMap(BTreeMap<i32, _>): keys are workspace ids.
			id, err := strconv.ParseInt(key, 10, 32)
			if err != nil {
				return cfg, fmt.Errorf("%s: workspace-map key %q is not a workspace id", module, key)
			}
			style, err := parseNamedWorkspaceStyle(raw.value)
			if err != nil {
				return cfg, fmt.Errorf("%s: workspace-map[%s]: %w", module, key, err)
			}
			s.WorkspaceMap[strconv.FormatInt(id, 10)] = style
		}
	}
	if doc.AppIconMap != nil {
		s.AppIconMap = doc.AppIconMap
	}
	doc.apply(&s.Click)

	switch {
	case s.DisplayMode != DisplayModeLabel && s.DisplayMode != DisplayModeIcon && s.DisplayMode != DisplayModeNone:
		return cfg, fmt.Errorf("%s: invalid display-mode %q (want label|icon|none)", module, s.DisplayMode)
	case s.ActiveIndicator != ActiveBackground && s.ActiveIndicator != ActiveUnderline:
		return cfg, fmt.Errorf("%s: invalid active-indicator %q (want background|underline)", module, s.ActiveIndicator)
	case cfg.Numbering != NumberingAbsolute && cfg.Numbering != NumberingRelative:
		return cfg, fmt.Errorf("%s: invalid numbering %q (want absolute|relative)", module, cfg.Numbering)
	case s.UrgentMode != UrgentWorkspace && s.UrgentMode != UrgentApplication:
		return cfg, fmt.Errorf("%s: invalid urgent-mode %q (want workspace|application)", module, s.UrgentMode)
	}
	return cfg, nil
}
