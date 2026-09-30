package bar

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/internal/glob"
)

// The sway and niri workspace modules are one Rust design twice over
// (sway_workspaces/ and niri_workspaces/ differ only in the IPC
// types): this file is their shared, pure half — filtering.rs,
// helpers.rs, and methods.rs's button model — and cwsmodule.go is the
// widget half. The backends map their compositor's snapshot onto
// cwsWorkspace/cwsWindow.

// cwsFlavor picks between the two Rust button families: the sway/niri
// workspace buttons and mango's tag buttons (mango_workspaces/), which
// differ in the label/icon rules, the CSS classes, and the tag set.
type cwsFlavor int

const (
	cwsWorkspaces cwsFlavor = iota
	cwsTags
	// cwsHyprland is hyprland_workspaces/: the module builds its own
	// models; the flavor picks its identity rules and custom-icon class.
	cwsHyprland
)

// cwsWorkspace is filtering.rs's WorkspaceSnapshot. num is sway's
// workspace number (-1 for a purely named workspace) or niri's idx.
type cwsWorkspace struct {
	id         uint64
	num        int
	name       string
	hasName    bool
	output     string
	hasOutput  bool
	urgent     bool
	active     bool
	focused    bool
	hasWindows bool
}

// cwsWindow is one compositor window as the button model reads it.
// order is the backend's windows_on_workspace sort key ahead of the
// id: niri's scrolling (column, tile), zero for sway (id order).
type cwsWindow struct {
	id           uint64
	workspace    uint64
	hasWorkspace bool
	app          appicons.Window
	urgent       bool
	order        [2]int
}

// cwsFilter is filtering.rs's FilterContext.
type cwsFilter struct {
	monitorSpecific   bool
	barMonitor        string
	hideTrailingEmpty bool
	ignore            []string
}

// cwsCollectDisplayed is collect_displayed: trailing empties computed
// over the full set, then monitor scoping, ignore patterns, and the
// trailing drop, ordered by output then number.
func cwsCollectDisplayed(all []cwsWorkspace, f cwsFilter) []cwsWorkspace {
	var trailing map[uint64]bool
	if f.hideTrailingEmpty {
		trailing = cwsTrailingEmpties(all)
	}
	out := make([]cwsWorkspace, 0, len(all))
	for _, ws := range all {
		if !cwsVisibleOnMonitor(ws, f) {
			continue
		}
		if cwsIgnored(ws, f.ignore) {
			continue
		}
		if trailing[ws.id] {
			continue
		}
		out = append(out, ws)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.output != b.output {
			return a.output < b.output
		}
		if an, bn := cwsSortNum(a.num), cwsSortNum(b.num); an != bn {
			return an < bn
		}
		return a.id < b.id
	})
	return out
}

// cwsSortNum sorts unnumbered workspaces (num < 0) after numbered
// ones on the same output.
func cwsSortNum(num int) int {
	if num < 0 {
		return math.MaxInt
	}
	return num
}

func cwsVisibleOnMonitor(ws cwsWorkspace, f cwsFilter) bool {
	if !f.monitorSpecific || f.barMonitor == "" {
		return true
	}
	return ws.hasOutput && ws.output == f.barMonitor
}

// cwsTrailingEmpties is compute_trailing_empties: per output, the
// highest-numbered workspace, when it has no windows.
func cwsTrailingEmpties(all []cwsWorkspace) map[uint64]bool {
	type tail struct {
		num   int
		id    uint64
		empty bool
	}
	last := map[string]tail{}
	for _, ws := range all {
		if !ws.hasOutput {
			continue
		}
		candidate := tail{num: ws.num, id: ws.id, empty: !ws.hasWindows}
		if cur, ok := last[ws.output]; !ok || candidate.num > cur.num {
			last[ws.output] = candidate
		}
	}
	out := map[uint64]bool{}
	for _, t := range last {
		if t.empty {
			out[t.id] = true
		}
	}
	return out
}

// cwsIgnored is is_ignored: each pattern against the name, then the
// number, then the stable id.
func cwsIgnored(ws cwsWorkspace, patterns []string) bool {
	num := strconv.Itoa(ws.num)
	id := strconv.FormatUint(ws.id, 10)
	for _, pattern := range patterns {
		if ws.hasName && glob.Wildcard(pattern, ws.name) {
			return true
		}
		if glob.Wildcard(pattern, num) || glob.Wildcard(pattern, id) {
			return true
		}
	}
	return false
}

// cwsLabel is sway helpers.rs's label_for, which niri's is a special
// case of (niri's idx is never negative): the number shows only when
// non-negative, and the result is absent for name-only without a name
// or index on an unnumbered workspace.
func cwsLabel(num int, name string, hasName bool, strategy config.LabelStrategy) (string, bool) {
	numLabel, hasNum := "", num >= 0
	if hasNum {
		numLabel = strconv.Itoa(num)
	}
	switch strategy {
	case config.LabelIndex:
		return numLabel, hasNum
	case config.LabelNameOrIndex:
		if hasName {
			return name, true
		}
		return numLabel, hasNum
	case config.LabelNameOnly:
		return name, hasName
	case config.LabelIndexAndName:
		switch {
		case hasNum && hasName:
			return numLabel + ": " + name, true
		case hasNum:
			return numLabel, true
		case hasName:
			return name, true
		}
	}
	return "", false
}

// cwsStyleFor is helpers.rs's workspace_style: the workspace-map entry
// for the name first, then for the stable id. It drives the label and
// icon overrides.
func cwsStyleFor(ws cwsWorkspace, m map[string]config.NamedWorkspaceStyle) (config.NamedWorkspaceStyle, bool) {
	if ws.hasName {
		if style, ok := m[ws.name]; ok {
			return style, true
		}
	}
	style, ok := m[strconv.FormatUint(ws.id, 10)]
	return style, ok
}

// cwsTagClass is mango's tag_css_class.
func cwsTagClass(index uint64) string { return "tag-" + strconv.FormatUint(index, 10) }

// cwsIDClass is workspace_id_css_class.
func cwsIDClass(id uint64) string { return "ws-id-" + strconv.FormatUint(id, 10) }

// cwsNameClass is workspace_name_css_class: non-identifier characters
// become underscores.
func cwsNameClass(name string) string {
	var b strings.Builder
	b.WriteString("ws-name-")
	for _, r := range name {
		if (r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')) || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

// cwsOverrideColor resolves the --ws-override-color the styling.rs
// provider would give this button: every workspace-map key with a
// color emits rules for ws-name-<key> (and ws-id-<key> when numeric),
// in key order, so the last matching key wins the cascade.
func cwsOverrideColor(classes []string, m map[string]config.NamedWorkspaceStyle) (config.ColorValue, bool) {
	has := make(map[string]bool, len(classes))
	for _, c := range classes {
		has[c] = true
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out config.ColorValue
	found := false
	for _, key := range keys {
		style := m[key]
		if !style.ColorSet {
			continue
		}
		match := has[cwsNameClass(key)]
		if id, err := strconv.ParseUint(key, 10, 64); err == nil && has[cwsIDClass(id)] {
			match = true
		}
		// hyprland's workspace-map is keyed by the (signed) id.
		if id, err := strconv.Atoi(key); err == nil && has[hyprIDClass(id)] {
			match = true
		}
		// mango's tag_map_css_class: a numeric key targets tag-N.
		if index, err := strconv.ParseUint(key, 10, 32); err == nil && has[cwsTagClass(index)] {
			match = true
		}
		if match {
			out, found = style.Color, true
		}
	}
	return out, found
}

// cwsAppIcon is button/mod.rs's AppIconInit plus the urgency the
// populate pass resolves.
type cwsAppIcon struct {
	name      string
	windowIDs []uint64
	urgent    bool
}

// cwsButtonModel is NiriWorkspaceButtonInit/SwayWorkspaceButtonInit
// (MangoTagButtonInit under cwsTags).
type cwsButtonModel struct {
	flavor   cwsFlavor
	ws       cwsWorkspace
	label    string
	hasLabel bool
	icon     string
	// urgent is the blinking workspace flag (snapshot urgency while
	// the blink is on).
	urgent   bool
	appIcons []cwsAppIcon
	classes  []string
}

// cwsWindowsOn is windows_on_workspace: the workspace's windows in the
// backend's order, id breaking ties.
func cwsWindowsOn(windows []cwsWindow, workspace uint64) []cwsWindow {
	out := make([]cwsWindow, 0, 4)
	for _, w := range windows {
		if w.hasWorkspace && w.workspace == workspace {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.order[0] != b.order[0] {
			return a.order[0] < b.order[0]
		}
		if a.order[1] != b.order[1] {
			return a.order[1] < b.order[1]
		}
		return a.id < b.id
	})
	return out
}

// cwsCollectAppIcons is collect_app_icons plus populate_app_icons's
// urgency: one icon per window, or per distinct icon under dedupe.
func cwsCollectAppIcons(windows []cwsWindow, cfg config.CompositorWorkspacesConfig, urgent map[uint64]bool) []cwsAppIcon {
	out := make([]cwsAppIcon, 0, len(windows))
	for _, w := range windows {
		name := appicons.Resolve(w.app, cfg.AppIconMap, cfg.AppIconsFallback)
		if cfg.AppIconsDedupe {
			merged := false
			for i := range out {
				if out[i].name == name {
					out[i].windowIDs = append(out[i].windowIDs, w.id)
					out[i].urgent = out[i].urgent || urgent[w.id]
					merged = true
					break
				}
			}
			if merged {
				continue
			}
		}
		out = append(out, cwsAppIcon{name: name, windowIDs: []uint64{w.id}, urgent: urgent[w.id]})
	}
	return out
}

// cwsAnyUrgent is any_urgent: a displayed workspace, or a window on
// one, wants attention.
func cwsAnyUrgent(displayed []cwsWorkspace, windows []cwsWindow) bool {
	ids := make(map[uint64]bool, len(displayed))
	for _, ws := range displayed {
		if ws.urgent {
			return true
		}
		ids[ws.id] = true
	}
	for _, w := range windows {
		if w.urgent && w.hasWorkspace && ids[w.workspace] {
			return true
		}
	}
	return false
}

// cwsBuildModels is rebuild_buttons's model pass: filter, then one
// button init per displayed workspace. Tags arrive already selected
// and ordered (the mango backend's displayed_tags).
func cwsBuildModels(flavor cwsFlavor, all []cwsWorkspace, windows []cwsWindow, cfg config.CompositorWorkspacesConfig, barMonitor string, vertical, blinkOn bool) []cwsButtonModel {
	displayed := all
	if flavor == cwsWorkspaces {
		displayed = cwsCollectDisplayed(all, cwsFilter{
			monitorSpecific:   cfg.MonitorSpecific,
			barMonitor:        barMonitor,
			hideTrailingEmpty: cfg.HideTrailingEmpty,
			ignore:            cfg.WorkspaceIgnore,
		})
	}
	out := make([]cwsButtonModel, 0, len(displayed))
	for _, ws := range displayed {
		on := cwsWindowsOn(windows, ws.id)
		urgentWindows := map[uint64]bool{}
		if blinkOn {
			for _, w := range on {
				if w.urgent {
					urgentWindows[w.id] = true
				}
			}
		}
		m := cwsButtonModel{flavor: flavor, ws: ws, urgent: ws.urgent && blinkOn}
		if cfg.AppIconsShow {
			m.appIcons = cwsCollectAppIcons(on, cfg, urgentWindows)
		}
		var style config.NamedWorkspaceStyle
		var styled bool
		if flavor == cwsTags {
			// tag_style: the tag-map entry for the index; the label
			// falls back to the index itself.
			style, styled = cfg.WorkspaceMap[strconv.FormatUint(ws.id, 10)]
		} else {
			style, styled = cwsStyleFor(ws, cfg.WorkspaceMap)
		}
		switch {
		case styled && style.LabelSet:
			m.label, m.hasLabel = style.Label, true
		case flavor == cwsTags:
			m.label, m.hasLabel = strconv.FormatUint(ws.id, 10), true
		default:
			m.label, m.hasLabel = cwsLabel(ws.num, ws.name, ws.hasName, cfg.LabelStrategy)
		}
		if styled {
			m.icon = style.Icon
		}
		m.classes = cwsClasses(m, cfg, vertical)
		out = append(out, m)
	}
	return out
}

// cwsClasses is compute_css_classes.
func cwsClasses(m cwsButtonModel, cfg config.CompositorWorkspacesConfig, vertical bool) []string {
	classes := []string{"workspace", cwsState(m.ws)}
	if m.ws.focused && m.flavor == cwsWorkspaces {
		classes = append(classes, "focused")
	}
	if m.urgent && cfg.UrgentShow {
		classes = append(classes, "urgent")
		if cfg.UrgentMode == config.UrgentApplication {
			classes = append(classes, "urgent-application")
		}
	}
	classes = append(classes, cfg.ActiveIndicator.CSSClass())
	if vertical {
		classes = append(classes, "vertical")
	}
	if m.flavor == cwsTags {
		return append(classes, cwsTagClass(m.ws.id))
	}
	classes = append(classes, cwsIDClass(m.ws.id))
	if m.ws.hasName {
		classes = append(classes, cwsNameClass(m.ws.name))
	}
	return classes
}

// cwsState is the active > occupied > empty state class.
func cwsState(ws cwsWorkspace) string {
	switch {
	case ws.active:
		return "active"
	case ws.hasWindows:
		return "occupied"
	}
	return "empty"
}

// hasClass reports whether classes carries name.
func hasClass(classes []string, name string) bool { return slices.Contains(classes, name) }

// showLabel is button/methods.rs's show_label. Workspaces: a mapped
// icon replaces the label, display-mode none hides it. Tags: label
// mode always shows it, icon mode only without a mapped icon.
func (m cwsButtonModel) showLabel(mode config.WorkspacesDisplayMode) bool {
	if !m.hasLabel || m.label == "" {
		return false
	}
	if m.flavor == cwsHyprland {
		// populate_identity: a mapped icon, else the label; nothing
		// under display-mode none.
		return mode != config.DisplayModeNone && m.icon == ""
	}
	if m.flavor == cwsTags {
		switch mode {
		case config.DisplayModeLabel:
			return true
		case config.DisplayModeIcon:
			return m.icon == ""
		}
		return false
	}
	return m.icon == "" && mode != config.DisplayModeNone
}

// showIcon is show_icon. Workspaces: a mapped icon shows in every mode
// but none. Tags: only in icon mode.
func (m cwsButtonModel) showIcon(mode config.WorkspacesDisplayMode) bool {
	if m.flavor == cwsTags {
		return mode == config.DisplayModeIcon && m.icon != ""
	}
	if m.flavor == cwsHyprland {
		return mode != config.DisplayModeNone && m.icon != ""
	}
	return m.icon != "" && mode != config.DisplayModeNone
}

// showDivider is show_divider (hyprland: should_show_divider, which
// only looks at the display mode).
func (m cwsButtonModel) showDivider(cfg config.CompositorWorkspacesConfig) bool {
	if m.flavor == cwsHyprland {
		return cfg.AppIconsShow && cfg.Divider != "" && cfg.DisplayMode != config.DisplayModeNone
	}
	return cfg.AppIconsShow && cfg.Divider != "" && (m.showLabel(cfg.DisplayMode) || m.showIcon(cfg.DisplayMode))
}
