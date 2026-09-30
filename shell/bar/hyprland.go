package bar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/service/hyprland"
)

// The hyprland-workspaces module (hyprland_workspaces/): the filter
// with min-workspace-count placeholders and workspace rules, absolute
// or relative numbering, per-monitor active tracking with the
// active-on-other-monitor state, urgency tracked by window address
// from the event stream (whole-workspace or per-icon pulses), and the
// focus dispatch through hl.dsp.focus with the legacy fallback. The
// buttons render through the shared workspace widgets.

// hyprFiltered is filtering.rs's FilteredWorkspace.
type hyprFiltered struct {
	id      int
	name    string
	windows int
}

// hyprFilter is filtering.rs's FilterContext.
type hyprFilter struct {
	showSpecial     bool
	monitorSpecific bool
	minCount        int
	activeID        int
	barMonitor      string
	ignore          []string
	rules           map[int]string
}

// hyprIgnored is matches_ignore_patterns: glob patterns against the id.
func hyprIgnored(id int, patterns []string) bool {
	idStr := strconv.Itoa(id)
	for _, pattern := range patterns {
		if glob.Glob(pattern, idStr) {
			return true
		}
	}
	return false
}

// hyprFilterWorkspaces is filter_workspaces.
func hyprFilterWorkspaces(all []hyprland.Workspace, f hyprFilter) []hyprFiltered {
	maxID := f.minCount
	out := make([]hyprFiltered, 0, len(all))
	for _, ws := range all {
		if hyprIgnored(ws.ID, f.ignore) {
			continue
		}
		if ws.ID < 0 && !f.showSpecial {
			continue
		}
		// exceeds_min_count_limit
		if f.minCount > 0 && ws.ID > 0 && ws.ID > maxID && ws.ID != f.activeID && ws.Windows == 0 {
			continue
		}
		// belongs_to_different_monitor
		if f.monitorSpecific && f.barMonitor != "" && ws.Monitor != f.barMonitor {
			continue
		}
		out = append(out, hyprFiltered{id: ws.ID, name: ws.Name, windows: ws.Windows})
	}
	if f.minCount > 0 {
		out = hyprAddPlaceholders(out, all, f, maxID)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// hyprAddPlaceholders is add_placeholder_workspaces: empty buttons for
// the ids up to the count this bar should own, and one for an active
// workspace Hyprland has not created yet.
func hyprAddPlaceholders(out []hyprFiltered, all []hyprland.Workspace, f hyprFilter, maxID int) []hyprFiltered {
	existing := map[int]bool{}
	for _, ws := range out {
		existing[ws.id] = true
	}
	for _, ws := range all {
		if ws.ID > 0 && ws.ID <= maxID {
			existing[ws.ID] = true
		}
	}
	for id := 1; id <= maxID; id++ {
		if existing[id] || hyprIgnored(id, f.ignore) {
			continue
		}
		if f.monitorSpecific {
			if f.barMonitor == "" {
				continue
			}
			if rule, ok := f.rules[id]; !ok || rule != f.barMonitor {
				continue
			}
		}
		out = append(out, hyprFiltered{id: id})
		existing[id] = true
	}
	if f.activeID > 0 && !existing[f.activeID] && !hyprIgnored(f.activeID, f.ignore) && hyprIncludeActivePlaceholder(all, f) {
		out = append(out, hyprFiltered{id: f.activeID})
	}
	return out
}

// hyprIncludeActivePlaceholder is should_include_active_workspace_placeholder.
func hyprIncludeActivePlaceholder(all []hyprland.Workspace, f hyprFilter) bool {
	if !f.monitorSpecific || f.barMonitor == "" {
		return true
	}
	monitor, found := "", false
	for _, ws := range all {
		if ws.ID == f.activeID {
			monitor, found = ws.Monitor, true
			break
		}
	}
	if !found {
		monitor, found = f.rules[f.activeID]
	}
	return !found || monitor == f.barMonitor
}

// hyprMonitorWorkspaces is monitor_workspaces_sorted: the positive
// rule ids bound to one monitor.
func hyprMonitorWorkspaces(barMonitor string, rules map[int]string) []int {
	var out []int
	for id, monitor := range rules {
		if monitor == barMonitor && id > 0 {
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

// hyprDisplayID is compute_display_id with relative_workspace_number.
func hyprDisplayID(id int, numbering config.WorkspacesNumbering, barMonitor string, monitorWorkspaces []int) int {
	if numbering != config.NumberingRelative || id <= 0 || barMonitor == "" || len(monitorWorkspaces) == 0 {
		return id
	}
	for pos, wsID := range monitorWorkspaces {
		if wsID == id {
			return pos + 1
		}
	}
	return id
}

// hyprLabel is format_workspace_label.
func hyprLabel(displayID, id int, name string, useName bool) string {
	if useName && name != "" && name != strconv.Itoa(id) {
		return name
	}
	return strconv.Itoa(displayID)
}

// hyprIDClass is workspace_id_css_class.
func hyprIDClass(id int) string {
	if id < 0 {
		return "workspace-id-neg" + strconv.Itoa(-id)
	}
	return "workspace-id-" + strconv.Itoa(id)
}

// hyprState is determine_workspace_state.
func hyprState(active, activeAnyMonitor bool, windows int) string {
	switch {
	case active:
		return "active"
	case activeAnyMonitor:
		return "active-other-monitor"
	case windows > 0:
		return "occupied"
	}
	return "empty"
}

// hyprNavigate is calculate_navigation_index.
func hyprNavigate(current, direction, total int) int {
	switch {
	case direction > 0:
		return (current + 1) % total
	case current == 0:
		return total - 1
	}
	return current - 1
}

// hyprResolveIcon is hyprland helpers.rs's resolve_app_icon: user
// patterns (title:, class:, or bare class) in key order, then the
// default table against the class, case-folded.
func hyprResolveIcon(class, title string, userMap map[string]string, fallback string) string {
	keys := make([]string, 0, len(userMap))
	for key := range userMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var match bool
		switch {
		case strings.HasPrefix(key, "title:"):
			match = glob.Fold(title, strings.ToLower(strings.TrimPrefix(key, "title:")))
		case strings.HasPrefix(key, "class:"):
			match = glob.Fold(class, strings.ToLower(strings.TrimPrefix(key, "class:")))
		default:
			match = glob.Fold(class, strings.ToLower(key))
		}
		if match {
			return userMap[key]
		}
	}
	for _, entry := range appicons.Default {
		if glob.Fold(class, strings.ToLower(entry.Pattern)) {
			return entry.Icon
		}
	}
	return fallback
}

// hyprWorkspaceIcons is resolve_workspace_icons: one icon per client on
// the workspace, or per case-folded class under dedupe (the first
// client of a class picks the icon).
func hyprWorkspaceIcons(id int, clients []hyprland.Client, cfg config.CompositorWorkspacesConfig, urgent map[string]bool) []cwsAppIcon {
	var out []cwsAppIcon
	seen := map[string]int{}
	for i, c := range clients {
		if c.Workspace.ID != id {
			continue
		}
		key := strings.ToLower(c.Class)
		if cfg.AppIconsDedupe {
			if idx, ok := seen[key]; ok {
				out[idx].windowIDs = append(out[idx].windowIDs, uint64(i))
				out[idx].urgent = out[idx].urgent || urgent[c.Address]
				continue
			}
			seen[key] = len(out)
		}
		out = append(out, cwsAppIcon{
			name:      hyprResolveIcon(c.Class, c.Title, cfg.AppIconMap, cfg.AppIconsFallback),
			windowIDs: []uint64{uint64(i)},
			urgent:    urgent[c.Address],
		})
	}
	return out
}

// hyprlandWorkspaces is the component's state (mod.rs's
// HyprlandWorkspaces) over a cached compositor snapshot.
type hyprlandWorkspaces struct {
	cwsView
	hcfg config.HyprlandWorkspacesConfig
	conn *hyprland.Connection

	workspaces []hyprland.Workspace
	monitors   []hyprland.Monitor
	clients    []hyprland.Client

	activeID       int
	activeAny      map[int]bool
	focusedMonitor string
	rules          map[int]string
	urgent         map[string]bool
	blinkOn        bool
	blinkStop      func()

	filtered []hyprFiltered
	models   []cwsButtonModel
}

func newHyprlandWorkspaces(ctx ModuleContext) (Module, error) {
	if ctx.Hyprland == nil {
		return nil, errors.New("hyprland-workspaces: the compositor is not Hyprland")
	}
	hcfg := ctx.Config.HyprlandWorkspaces
	m := &hyprlandWorkspaces{
		ctx: ctx, cfg: hcfg.Shared, flavor: cwsHyprland, otherMonitor: hcfg.ActiveOnOtherMonitorColor,
		hcfg:   hcfg,
		conn:   ctx.Hyprland,
		urgent: map[string]bool{},
		rules:  map[int]string{},
	}
	m.actions = cwsActions{
		focus:    func(_ context.Context, ws cwsWorkspace) error { return m.switchTo(ws.num) },
		next:     func(context.Context) error { return m.navigate(1) },
		previous: func(context.Context) error { return m.navigate(-1) },
		last: func(context.Context) error {
			_, err := m.conn.Dispatch("workspace previous")
			return err
		},
	}
	m.boldFace = cwsBoldFace(ctx, m.labelPx())
	m.root = newCwsContainer(&m.cwsView)
	if err := m.query(); err != nil {
		return nil, err
	}
	m.initActive()
	m.loadRules()
	m.rebuild()
	if ctx.App == nil {
		return m, nil
	}
	events, err := ctx.Hyprland.Events(context.Background())
	if err != nil {
		return nil, fmt.Errorf("hyprland-workspaces: events: %w", err)
	}
	go func() {
		for ev := range events {
			m.ctx.Invoke(func() { m.handle(ev) })
		}
	}()
	return m, nil
}

// query re-reads the compositor state the Rust service keeps.
func (m *hyprlandWorkspaces) query() error {
	workspaces, err := m.conn.Workspaces()
	if err != nil {
		return err
	}
	monitors, err := m.conn.Monitors()
	if err != nil {
		return err
	}
	clients, err := m.conn.Clients()
	if err != nil {
		return err
	}
	m.workspaces, m.monitors, m.clients = workspaces, monitors, clients
	return nil
}

// loadRules is spawn_load_workspace_rules: positive numeric rules
// bound to a monitor; a failed load leaves no rules.
func (m *hyprlandWorkspaces) loadRules() {
	m.rules = map[int]string{}
	rules, err := m.conn.WorkspaceRules()
	if err != nil {
		log.Printf("hyprland-workspaces: cannot load workspace rules: %v", err)
		return
	}
	for _, rule := range rules {
		id, err := strconv.Atoi(rule.WorkspaceString)
		if err != nil || id <= 0 || rule.Monitor == nil {
			continue
		}
		m.rules[id] = *rule.Monitor
	}
}

// trackActive reports whether the bar's own monitor decides the active
// workspace (monitor-specific or the other-monitor highlight).
func (m *hyprlandWorkspaces) trackActive() bool {
	return m.cfg.MonitorSpecific || m.hcfg.HighlightActiveOnOtherMonitor
}

// initActive is initial_active_workspace, the other-monitor set, and
// the focused monitor.
func (m *hyprlandWorkspaces) initActive() {
	m.activeID = 1
	m.activeAny = map[int]bool{}
	found := false
	for _, mon := range m.monitors {
		m.activeAny[mon.ActiveWS.ID] = true
		if mon.Focused {
			m.focusedMonitor = mon.Name
		}
		if m.trackActive() && m.ctx.Connector != "" && mon.Name == m.ctx.Connector {
			m.activeID, found = mon.ActiveWS.ID, true
		}
	}
	if !found {
		if ws, err := m.conn.ActiveWorkspace(); err == nil {
			m.activeID = ws.ID
		}
	}
}

// refreshActive is refresh_active_workspace.
func (m *hyprlandWorkspaces) refreshActive() {
	m.activeAny = map[int]bool{}
	for _, mon := range m.monitors {
		m.activeAny[mon.ActiveWS.ID] = true
	}
	if m.trackActive() && m.ctx.Connector != "" {
		for _, mon := range m.monitors {
			if mon.Name == m.ctx.Connector {
				m.activeID = mon.ActiveWS.ID
				return
			}
		}
	}
	for _, mon := range m.monitors {
		if mon.Focused {
			m.activeID = mon.ActiveWS.ID
			return
		}
	}
}

func (m *hyprlandWorkspaces) filter() hyprFilter {
	return hyprFilter{
		showSpecial:     m.hcfg.ShowSpecial,
		monitorSpecific: m.cfg.MonitorSpecific,
		minCount:        m.hcfg.MinWorkspace,
		activeID:        m.activeID,
		barMonitor:      m.ctx.Connector,
		ignore:          m.cfg.WorkspaceIgnore,
		rules:           m.rules,
	}
}

// rebuild is rebuild_buttons.
func (m *hyprlandWorkspaces) rebuild() {
	m.refreshActive()
	m.filtered = hyprFilterWorkspaces(m.workspaces, m.filter())
	m.render()
}

// render is the button pass shared by rebuild_buttons and
// update_active_state: states and urgency over the current filter.
func (m *hyprlandWorkspaces) render() {
	monitorWorkspaces := hyprMonitorWorkspaces(m.ctx.Connector, m.rules)
	perIcon := m.cfg.AppIconsShow && m.cfg.UrgentMode == config.UrgentApplication
	windows := map[int]int{}
	for _, ws := range m.workspaces {
		windows[ws.ID] = ws.Windows
	}
	m.models = m.models[:0]
	for _, ws := range m.filtered {
		isUrgent := m.blinkOn && m.cfg.UrgentShow && m.hasUrgentWindow(ws.id)
		var urgentAddrs map[string]bool
		if isUrgent && perIcon {
			urgentAddrs = m.urgent
		}
		model := cwsButtonModel{
			flavor: cwsHyprland,
			ws: cwsWorkspace{
				id: uint64(ws.id), num: ws.id, name: ws.name, hasName: ws.name != "",
				active: ws.id == m.activeID, hasWindows: windows[ws.id] > 0,
			},
		}
		style, styled := m.cfg.WorkspaceMap[strconv.Itoa(ws.id)]
		if styled {
			model.icon = style.Icon
		}
		if styled && style.LabelSet {
			model.label = style.Label
		} else {
			display := hyprDisplayID(ws.id, m.hcfg.Numbering, m.ctx.Connector, monitorWorkspaces)
			model.label = hyprLabel(display, ws.id, ws.name, m.hcfg.LabelUseName)
		}
		model.hasLabel = true
		if m.cfg.AppIconsShow {
			model.appIcons = hyprWorkspaceIcons(ws.id, m.clients, m.cfg, urgentAddrs)
		}
		// apply_urgency: with urgent addresses and icons to carry them
		// the icons pulse and the button does not; otherwise the button
		// takes the workspace's urgency (the icons were resolved without
		// addresses, so none of them is marked).
		model.urgent = isUrgent && (len(urgentAddrs) == 0 || len(model.appIcons) == 0)
		model.classes = m.classes(ws.id, hyprState(model.ws.active, m.activeAny[ws.id], windows[ws.id]), model.urgent)
		m.models = append(m.models, model)
	}
	m.root.setButtons(m.models)
}

// classes is compute_static_css_classes + collect_button_css_classes.
func (m *hyprlandWorkspaces) classes(id int, state string, urgent bool) []string {
	classes := []string{"workspace", m.cfg.ActiveIndicator.CSSClass()}
	if id < 0 {
		classes = append(classes, "special")
	}
	if m.vertical() {
		classes = append(classes, "vertical")
	}
	classes = append(classes, hyprIDClass(id), state)
	if urgent {
		classes = append(classes, "urgent")
	}
	return classes
}

// hasUrgentWindow is workspace_has_urgent_window.
func (m *hyprlandWorkspaces) hasUrgentWindow(id int) bool {
	if len(m.urgent) == 0 {
		return false
	}
	for _, c := range m.clients {
		if c.Workspace.ID == id && m.urgent[c.Address] {
			return true
		}
	}
	return false
}

// clearUrgentFor is clear_urgent_windows_for_workspace.
func (m *hyprlandWorkspaces) clearUrgentFor(id int) {
	for _, c := range m.clients {
		if c.Workspace.ID == id {
			delete(m.urgent, c.Address)
		}
	}
}

// pruneUrgent is prune_stale_urgent_windows.
func (m *hyprlandWorkspaces) pruneUrgent() {
	live := map[string]bool{}
	for _, c := range m.clients {
		live[c.Address] = true
	}
	for address := range m.urgent {
		if !live[address] {
			delete(m.urgent, address)
		}
	}
}

func (m *hyprlandWorkspaces) startBlink() {
	m.stopBlink()
	m.blinkOn = true
	if m.ctx.App == nil {
		m.blinkStop = func() {}
		return
	}
	m.blinkStop = m.ctx.App.Every(cwsBlinkInterval, func() {
		m.blinkOn = !m.blinkOn
		m.render()
	})
}

func (m *hyprlandWorkspaces) stopBlink() {
	if m.blinkStop != nil {
		m.blinkStop()
		m.blinkStop = nil
	}
	m.blinkOn = false
}

func (m *hyprlandWorkspaces) stopBlinkIfNoUrgent() {
	if len(m.urgent) == 0 {
		m.stopBlink()
	}
}

// workspaceMonitor is workspace_monitor_name.
func (m *hyprlandWorkspaces) workspaceMonitor(id int) (string, bool) {
	for _, ws := range m.workspaces {
		if ws.ID == id {
			return ws.Monitor, true
		}
	}
	monitor, ok := m.rules[id]
	return monitor, ok
}

// applyWorkspaceEvent is should_apply_workspace_event.
func (m *hyprlandWorkspaces) applyWorkspaceEvent() bool {
	if m.ctx.Connector == "" {
		return true
	}
	if m.focusedMonitor != "" {
		return m.focusedMonitor == m.ctx.Connector
	}
	for _, mon := range m.monitors {
		if mon.Focused {
			return mon.Name == m.ctx.Connector
		}
	}
	return true
}

// applyActiveChange is should_apply_active_workspace_change.
func (m *hyprlandWorkspaces) applyActiveChange(id int, tracked bool) bool {
	if !tracked {
		return true
	}
	if m.ctx.Connector == "" {
		return m.applyWorkspaceEvent()
	}
	if monitor, ok := m.workspaceMonitor(id); ok {
		return monitor == m.ctx.Connector
	}
	return m.applyWorkspaceEvent()
}

// syncAfterActiveChange is sync_after_active_workspace_change: a
// placeholder set may change with the active id, so min-workspace-count
// re-filters; otherwise only the states move.
func (m *hyprlandWorkspaces) syncAfterActiveChange() {
	if m.hcfg.MinWorkspace > 0 {
		m.rebuild()
		return
	}
	m.render()
}

// relevant reports whether an event reaches update_cmd, so the frequent
// ones it ignores (titles without title: patterns, focus of a window
// that was never urgent, everything unwatched) cost no query.
func (m *hyprlandWorkspaces) relevant(ev hyprland.Event) bool {
	switch ev.Kind {
	case hyprland.EventWorkspaceV2, hyprland.EventFocusedMonV2,
		hyprland.EventCreateWspcV2, hyprland.EventDestroyWspcV2, hyprland.EventMoveWorkspaceV2,
		hyprland.EventRenameWorkspace, hyprland.EventActiveSpecialV2,
		hyprland.EventMonitorAddedV2, hyprland.EventMonitorRemovedV2,
		hyprland.EventOpenWindow, hyprland.EventCloseWindow, hyprland.EventMoveWindow, hyprland.EventMoveWindowV2,
		hyprland.EventUrgent, hyprland.EventConfigReloaded:
		return true
	case hyprland.EventActiveWindowV2:
		return m.urgent[ev.Address]
	case hyprland.EventWindowTitleV2:
		// update_app_icons_on_title_change: only title: patterns care.
		if !m.cfg.AppIconsShow {
			return false
		}
		for key := range m.cfg.AppIconMap {
			if strings.HasPrefix(key, "title:") {
				return true
			}
		}
	}
	return false
}

// handle is update_cmd over the watched events, on a fresh snapshot.
func (m *hyprlandWorkspaces) handle(ev hyprland.Event) {
	if !m.relevant(ev) {
		return
	}
	if err := m.query(); err != nil {
		log.Printf("hyprland-workspaces: %v", err)
		return
	}
	switch ev.Kind {
	case hyprland.EventWorkspaceV2:
		tracked := m.trackActive()
		if !m.applyActiveChange(ev.ID, tracked) {
			if !m.cfg.MonitorSpecific && m.hcfg.HighlightActiveOnOtherMonitor {
				m.rebuild()
			}
			return
		}
		m.clearUrgentFor(ev.ID)
		m.stopBlinkIfNoUrgent()
		delete(m.activeAny, m.activeID)
		m.activeAny[ev.ID] = true
		m.activeID = ev.ID
		m.syncAfterActiveChange()
	case hyprland.EventFocusedMonV2:
		m.focusedMonitor = ev.Name
		if !m.trackActive() || m.ctx.Connector == "" || m.ctx.Connector == ev.Name {
			m.clearUrgentFor(ev.ID)
			m.stopBlinkIfNoUrgent()
			m.activeID = ev.ID
			m.syncAfterActiveChange()
		}
	case hyprland.EventCreateWspcV2, hyprland.EventDestroyWspcV2, hyprland.EventMoveWorkspaceV2,
		hyprland.EventRenameWorkspace, hyprland.EventActiveSpecialV2,
		hyprland.EventMonitorAddedV2, hyprland.EventMonitorRemovedV2:
		m.rebuild()
	case hyprland.EventOpenWindow, hyprland.EventCloseWindow, hyprland.EventMoveWindow, hyprland.EventMoveWindowV2:
		m.pruneUrgent()
		m.stopBlinkIfNoUrgent()
		m.rebuild()
	case hyprland.EventUrgent:
		wasEmpty := len(m.urgent) == 0
		m.urgent[ev.Address] = true
		if wasEmpty {
			m.startBlink()
		}
		m.render()
	case hyprland.EventActiveWindowV2:
		delete(m.urgent, ev.Address)
		m.stopBlinkIfNoUrgent()
		m.render()
	case hyprland.EventConfigReloaded:
		m.loadRules()
		m.rebuild()
	case hyprland.EventWindowTitleV2:
		m.rebuild()
	}
}

// switchTo is switch_to_workspace: a special workspace dispatches by
// name, and the Lua dispatcher falls back to the legacy one.
func (m *hyprlandWorkspaces) switchTo(id int) error {
	selector := strconv.Itoa(id)
	if id < 0 {
		selector = ""
		for _, ws := range m.workspaces {
			if ws.ID == id && ws.Name != "" {
				selector = "name:" + ws.Name
			}
		}
		if selector == "" {
			return fmt.Errorf("workspace %d: no resolvable selector, skipping dispatch", id)
		}
	}
	arg := selector
	if strings.HasPrefix(selector, "name:") {
		arg = `"` + strings.ReplaceAll(strings.ReplaceAll(selector, `\`, `\\`), `"`, `\"`) + `"`
	}
	resp, err := m.conn.Dispatch("hl.dsp.focus({workspace = " + arg + "})")
	if err != nil {
		return fmt.Errorf("cannot switch workspace %d: %w", id, err)
	}
	if strings.HasPrefix(resp, "error:") || strings.Contains(resp, "Invalid dispatcher") {
		if _, err := m.conn.Dispatch("workspace " + selector); err != nil {
			return fmt.Errorf("cannot switch workspace %d: %w", id, err)
		}
	}
	return nil
}

// navigate is navigate_workspace: the next or previous displayed
// workspace, wrapping.
func (m *hyprlandWorkspaces) navigate(direction int) error {
	list := hyprFilterWorkspaces(m.workspaces, m.filter())
	if len(list) == 0 {
		return nil
	}
	current := 0
	for i, ws := range list {
		if ws.id == m.activeID {
			current = i
			break
		}
	}
	return m.switchTo(list[hyprNavigate(current, direction, len(list))].id)
}

func (m *hyprlandWorkspaces) Root() widget.Widget { return m.root }

func (m *hyprlandWorkspaces) ownsChrome() {}

// Stop releases the blink timer.
func (m *hyprlandWorkspaces) Stop() { m.stopBlink() }
