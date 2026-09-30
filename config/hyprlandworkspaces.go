package config

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/BurntSushi/toml"
)

// WorkspacesDisplayMode selects what a workspace button shows.
type WorkspacesDisplayMode string

// Display modes. Icons are not ported to the Go shell yet.
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

// WorkspaceStyle is one workspace-map entry: per-workspace overrides.
type WorkspaceStyle struct {
	Icon  string
	Color ColorValue
	// ColorSet distinguishes an unset color from transparent.
	ColorSet bool
}

// HyprlandWorkspacesConfig is the hyprland-workspaces module config.
type HyprlandWorkspacesConfig struct {
	DisplayMode     WorkspacesDisplayMode
	ActiveColor     ColorValue
	OccupiedColor   ColorValue
	EmptyColor      ColorValue
	ActiveIndicator ActiveIndicator
	Numbering       WorkspacesNumbering
	LabelSize       Size
	WorkspacePad    Size
	MinWorkspace    int
	LabelUseName    bool
	ShowSpecial     bool
	// MonitorSpecific keeps a bar's row to its own output's
	// workspaces (the schema's monitor-specific).
	MonitorSpecific bool
	Divider         string
	WorkspaceMap    map[int]WorkspaceStyle
}

// DefaultsHyprlandWorkspaces returns the schema defaults.
func DefaultsHyprlandWorkspaces() HyprlandWorkspacesConfig {
	return HyprlandWorkspacesConfig{
		DisplayMode:     DisplayModeLabel,
		ActiveColor:     mustColor("accent"),
		OccupiedColor:   mustColor("fg-muted"),
		EmptyColor:      mustColor("fg-subtle"),
		ActiveIndicator: ActiveBackground,
		Numbering:       NumberingAbsolute,
		LabelSize:       Size{Value: 1.0, Unit: SizeMultiplier},
		WorkspacePad:    Size{Value: 0.5, Unit: SizeMultiplier},
		MinWorkspace:    0,
		LabelUseName:    false,
		ShowSpecial:     true,
		MonitorSpecific: true,
		Divider:         " ",
		WorkspaceMap:    map[int]WorkspaceStyle{},
	}
}

// applyHyprlandWorkspaces overlays [modules.hyprland-workspaces].
func applyHyprlandWorkspaces(md toml.MetaData, prim toml.Primitive) (HyprlandWorkspacesConfig, error) {
	cfg := DefaultsHyprlandWorkspaces()
	var doc struct {
		DisplayMode     string               `toml:"display-mode"`
		ActiveColor     string               `toml:"active-color"`
		OccupiedColor   string               `toml:"occupied-color"`
		EmptyColor      string               `toml:"empty-color"`
		ActiveIndicator string               `toml:"active-indicator"`
		Numbering       string               `toml:"numbering"`
		LabelSize       tomlValue            `toml:"label-size"`
		WorkspacePad    tomlValue            `toml:"workspace-padding"`
		MinWorkspace    *int                 `toml:"min-workspace-count"`
		LabelUseName    *bool                `toml:"label-use-name"`
		ShowSpecial     *bool                `toml:"show-special"`
		MonitorSpecific *bool                `toml:"monitor-specific"`
		Divider         *string              `toml:"divider"`
		WorkspaceMap    map[string]tomlValue `toml:"workspace-map"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.DisplayMode != "" {
		cfg.DisplayMode = WorkspacesDisplayMode(doc.DisplayMode)
	}
	if doc.ActiveColor != "" {
		cv, err := ParseColorValue(doc.ActiveColor)
		if err != nil {
			return cfg, fmt.Errorf("hyprland-workspaces: active-color: %w", err)
		}
		cfg.ActiveColor = cv
	}
	if doc.OccupiedColor != "" {
		cv, err := ParseColorValue(doc.OccupiedColor)
		if err != nil {
			return cfg, fmt.Errorf("hyprland-workspaces: occupied-color: %w", err)
		}
		cfg.OccupiedColor = cv
	}
	if doc.EmptyColor != "" {
		cv, err := ParseColorValue(doc.EmptyColor)
		if err != nil {
			return cfg, fmt.Errorf("hyprland-workspaces: empty-color: %w", err)
		}
		cfg.EmptyColor = cv
	}
	if doc.ActiveIndicator != "" {
		cfg.ActiveIndicator = ActiveIndicator(doc.ActiveIndicator)
	}
	if doc.Numbering != "" {
		cfg.Numbering = WorkspacesNumbering(doc.Numbering)
	}
	if doc.LabelSize.value != nil {
		if err := cfg.LabelSize.unmarshal(doc.LabelSize.value, "label-size"); err != nil {
			return cfg, err
		}
	}
	if doc.WorkspacePad.value != nil {
		if err := cfg.WorkspacePad.unmarshal(doc.WorkspacePad.value, "workspace-padding"); err != nil {
			return cfg, err
		}
	}
	if doc.MinWorkspace != nil {
		cfg.MinWorkspace = *doc.MinWorkspace
	}
	if doc.LabelUseName != nil {
		cfg.LabelUseName = *doc.LabelUseName
	}
	if doc.ShowSpecial != nil {
		cfg.ShowSpecial = *doc.ShowSpecial
	}
	if doc.MonitorSpecific != nil {
		cfg.MonitorSpecific = *doc.MonitorSpecific
	}
	if doc.Divider != nil {
		cfg.Divider = *doc.Divider
	}
	if doc.WorkspaceMap != nil {
		cfg.WorkspaceMap = map[int]WorkspaceStyle{}
		ids := make([]string, 0, len(doc.WorkspaceMap))
		for key := range doc.WorkspaceMap {
			ids = append(ids, key)
		}
		sort.Strings(ids)
		for _, key := range ids {
			id, err := strconv.Atoi(key)
			if err != nil {
				return cfg, fmt.Errorf("hyprland-workspaces: workspace-map key %q is not a workspace id", key)
			}
			style := WorkspaceStyle{}
			if table, ok := doc.WorkspaceMap[key].value.(map[string]any); ok {
				if icon, ok := table["icon"].(string); ok {
					style.Icon = icon
				}
				if color, ok := table["color"].(string); ok {
					cv, err := ParseColorValue(color)
					if err != nil {
						return cfg, fmt.Errorf("hyprland-workspaces: workspace-map[%s] color: %w", key, err)
					}
					style.Color, style.ColorSet = cv, true
				}
			}
			cfg.WorkspaceMap[id] = style
		}
	}

	switch {
	case cfg.DisplayMode != DisplayModeLabel && cfg.DisplayMode != DisplayModeNone:
		return cfg, fmt.Errorf("hyprland-workspaces: display-mode %q not supported (want label or none; icons are not ported yet)", cfg.DisplayMode)
	case cfg.ActiveIndicator != ActiveBackground && cfg.ActiveIndicator != ActiveUnderline:
		return cfg, fmt.Errorf("hyprland-workspaces: invalid active-indicator %q (want background|underline)", cfg.ActiveIndicator)
	case cfg.Numbering != NumberingAbsolute && cfg.Numbering != NumberingRelative:
		return cfg, fmt.Errorf("hyprland-workspaces: invalid numbering %q (want absolute|relative)", cfg.Numbering)
	case cfg.MinWorkspace < 0 || cfg.MinWorkspace > 255:
		return cfg, fmt.Errorf("hyprland-workspaces: min-workspace-count %d outside 0-255", cfg.MinWorkspace)
	}
	return cfg, nil
}
