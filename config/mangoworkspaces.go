package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// MangoWorkspacesConfig is the mango-workspaces module config. The
// tag schema shares the sway/niri button styling, carried in Shared
// (tag-padding maps to WorkspacePad and tag-map, keyed by tag index,
// to WorkspaceMap); the tag set rules are its own.
type MangoWorkspacesConfig struct {
	HideEmpty   bool
	MinTagCount int
	Shared      CompositorWorkspacesConfig
}

// DefaultsMangoWorkspaces returns the schema defaults.
func DefaultsMangoWorkspaces() MangoWorkspacesConfig {
	shared := defaultsCompositorWorkspaces(false)
	// The fields the tag schema does not have stay inert.
	shared.MonitorSpecific = false
	return MangoWorkspacesConfig{HideEmpty: true, MinTagCount: 0, Shared: shared}
}

// applyMangoWorkspaces overlays [modules.mango-workspaces].
func applyMangoWorkspaces(md toml.MetaData, prim toml.Primitive) (MangoWorkspacesConfig, error) {
	const module = "mango-workspaces"
	cfg := DefaultsMangoWorkspaces()
	var doc struct {
		workspaceClicksDoc
		HideEmpty        *bool                `toml:"hide-empty"`
		MinTagCount      *int                 `toml:"min-tag-count"`
		DisplayMode      *string              `toml:"display-mode"`
		Divider          *string              `toml:"divider"`
		AppIconsShow     *bool                `toml:"app-icons-show"`
		AppIconsDedupe   *bool                `toml:"app-icons-dedupe"`
		AppIconsFallback *string              `toml:"app-icons-fallback"`
		AppIconsEmpty    *string              `toml:"app-icons-empty"`
		UrgentShow       *bool                `toml:"urgent-show"`
		UrgentMode       *string              `toml:"urgent-mode"`
		ActiveIndicator  *string              `toml:"active-indicator"`
		TagPadding       tomlValue            `toml:"tag-padding"`
		IconGap          tomlValue            `toml:"icon-gap"`
		IconSize         tomlValue            `toml:"icon-size"`
		LabelSize        tomlValue            `toml:"label-size"`
		ActiveColor      string               `toml:"active-color"`
		OccupiedColor    string               `toml:"occupied-color"`
		EmptyColor       string               `toml:"empty-color"`
		ContainerBgColor string               `toml:"container-bg-color"`
		BorderShow       *bool                `toml:"border-show"`
		BorderColor      string               `toml:"border-color"`
		AppIconMap       map[string]string    `toml:"app-icon-map"`
		TagMap           map[string]tomlValue `toml:"tag-map"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, fmt.Errorf("%s: %w", module, err)
	}
	s := &cfg.Shared
	if doc.HideEmpty != nil {
		cfg.HideEmpty = *doc.HideEmpty
	}
	if doc.MinTagCount != nil {
		if *doc.MinTagCount < 0 || *doc.MinTagCount > 255 {
			return cfg, fmt.Errorf("%s: min-tag-count %d outside 0-255", module, *doc.MinTagCount)
		}
		cfg.MinTagCount = *doc.MinTagCount
	}
	for _, b := range []struct {
		raw    *bool
		target *bool
	}{
		{doc.AppIconsShow, &s.AppIconsShow},
		{doc.AppIconsDedupe, &s.AppIconsDedupe},
		{doc.UrgentShow, &s.UrgentShow},
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
	if doc.DisplayMode != nil {
		s.DisplayMode = WorkspacesDisplayMode(*doc.DisplayMode)
	}
	if doc.UrgentMode != nil {
		s.UrgentMode = UrgentMode(*doc.UrgentMode)
	}
	if doc.ActiveIndicator != nil {
		s.ActiveIndicator = ActiveIndicator(*doc.ActiveIndicator)
	}
	for _, sz := range []struct {
		key    string
		raw    tomlValue
		target *Size
	}{
		{"tag-padding", doc.TagPadding, &s.WorkspacePad},
		{"icon-gap", doc.IconGap, &s.IconGap},
		{"icon-size", doc.IconSize, &s.IconSize},
		{"label-size", doc.LabelSize, &s.LabelSize},
	} {
		if err := parseSizeKey(module, sz.key, sz.raw, sz.target); err != nil {
			return cfg, err
		}
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
	} {
		if err := parseColorKey(module, c.key, c.raw, c.target); err != nil {
			return cfg, err
		}
	}
	if doc.AppIconMap != nil {
		s.AppIconMap = doc.AppIconMap
	}
	if doc.TagMap != nil {
		s.WorkspaceMap = map[string]NamedWorkspaceStyle{}
		for key, raw := range doc.TagMap {
			style, err := parseNamedWorkspaceStyle(raw.value)
			if err != nil {
				return cfg, fmt.Errorf("%s: tag-map[%s]: %w", module, key, err)
			}
			s.WorkspaceMap[key] = style
		}
	}
	doc.apply(&s.Click)

	switch s.DisplayMode {
	case DisplayModeLabel, DisplayModeIcon, DisplayModeNone:
	default:
		return cfg, fmt.Errorf("%s: invalid display-mode %q (want label|icon|none)", module, s.DisplayMode)
	}
	if s.UrgentMode != UrgentWorkspace && s.UrgentMode != UrgentApplication {
		return cfg, fmt.Errorf("%s: invalid urgent-mode %q (want workspace|application)", module, s.UrgentMode)
	}
	if s.ActiveIndicator != ActiveBackground && s.ActiveIndicator != ActiveUnderline {
		return cfg, fmt.Errorf("%s: invalid active-indicator %q (want background|underline)", module, s.ActiveIndicator)
	}
	return cfg, nil
}
