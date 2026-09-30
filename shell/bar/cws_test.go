package bar

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/styling"
)

// The filtering cases are niri/sway filtering.rs's tests.

func occupiedWS(id uint64, num int, output string) cwsWorkspace {
	return cwsWorkspace{id: id, num: num, output: output, hasOutput: true, hasWindows: true}
}

func emptyWS(id uint64, num int, output string) cwsWorkspace {
	return cwsWorkspace{id: id, num: num, output: output, hasOutput: true}
}

func wsIDs(list []cwsWorkspace) []uint64 {
	out := make([]uint64, len(list))
	for i, ws := range list {
		out[i] = ws.id
	}
	return out
}

func TestCwsShowsEmptyWorkspacesWithoutHideTrailing(t *testing.T) {
	named := emptyWS(3, 3, "DP-1")
	named.name, named.hasName = "web", true
	got := cwsCollectDisplayed([]cwsWorkspace{occupiedWS(1, 1, "DP-1"), emptyWS(2, 2, "DP-1"), named}, cwsFilter{})
	if !reflect.DeepEqual(wsIDs(got), []uint64{1, 2, 3}) {
		t.Errorf("ids = %v", wsIDs(got))
	}
}

func TestCwsHideTrailingEmptyDropsOnlyTheTail(t *testing.T) {
	all := []cwsWorkspace{occupiedWS(1, 1, "DP-1"), emptyWS(2, 2, "DP-1"), occupiedWS(3, 3, "DP-1"), emptyWS(4, 4, "DP-1"), emptyWS(5, 1, "HDMI-A-1")}
	got := cwsCollectDisplayed(all, cwsFilter{hideTrailingEmpty: true})
	// The inner empty 2 stays; each output's empty tail goes.
	if !reflect.DeepEqual(wsIDs(got), []uint64{1, 2, 3}) {
		t.Errorf("ids = %v, want the trailing empties of both outputs dropped", wsIDs(got))
	}
	// An occupied tail is kept.
	got = cwsCollectDisplayed([]cwsWorkspace{emptyWS(1, 1, "DP-1"), occupiedWS(2, 2, "DP-1")}, cwsFilter{hideTrailingEmpty: true})
	if !reflect.DeepEqual(wsIDs(got), []uint64{1, 2}) {
		t.Errorf("ids = %v, want an occupied tail kept", wsIDs(got))
	}
}

func TestCwsIgnoreByNameNumberAndID(t *testing.T) {
	scratch := emptyWS(3, 3, "DP-1")
	scratch.name, scratch.hasName = "scratch", true
	all := []cwsWorkspace{occupiedWS(1, 1, "DP-1"), occupiedWS(2, 2, "DP-1"), scratch, occupiedWS(40, 9, "DP-1")}
	if got := cwsCollectDisplayed(all, cwsFilter{ignore: []string{"scr*"}}); !reflect.DeepEqual(wsIDs(got), []uint64{1, 2, 40}) {
		t.Errorf("name glob: %v", wsIDs(got))
	}
	if got := cwsCollectDisplayed(all, cwsFilter{ignore: []string{"?"}}); !reflect.DeepEqual(wsIDs(got), []uint64{}) {
		t.Errorf("single-digit number glob: %v, want all ignored", wsIDs(got))
	}
	if got := cwsCollectDisplayed(all, cwsFilter{ignore: []string{"40"}}); !reflect.DeepEqual(wsIDs(got), []uint64{1, 2, 3}) {
		t.Errorf("stable id: %v", wsIDs(got))
	}
	// Case-sensitive, as the wildcard crate is.
	if got := cwsCollectDisplayed(all, cwsFilter{ignore: []string{"SCRATCH"}}); len(got) != 4 {
		t.Errorf("case-insensitive match: %v", wsIDs(got))
	}
}

func TestCwsOrdersByOutputThenNumber(t *testing.T) {
	unnumbered := occupiedWS(9, -1, "DP-1")
	all := []cwsWorkspace{occupiedWS(3, 2, "DP-2"), unnumbered, occupiedWS(1, 2, "DP-1"), occupiedWS(2, 1, "DP-2")}
	if got := cwsCollectDisplayed(all, cwsFilter{}); !reflect.DeepEqual(wsIDs(got), []uint64{1, 9, 2, 3}) {
		t.Errorf("ids = %v, want output order, number order, unnumbered last", wsIDs(got))
	}
}

func TestCwsMonitorSpecific(t *testing.T) {
	all := []cwsWorkspace{occupiedWS(1, 1, "DP-1"), occupiedWS(2, 1, "DP-2"), {id: 3, num: 1}}
	if got := cwsCollectDisplayed(all, cwsFilter{monitorSpecific: true, barMonitor: "DP-1"}); !reflect.DeepEqual(wsIDs(got), []uint64{1}) {
		t.Errorf("scoped = %v, want DP-1's only (an outputless workspace is not on any bar)", wsIDs(got))
	}
	// Scoping off, or no bar monitor, shows everything.
	if got := cwsCollectDisplayed(all, cwsFilter{monitorSpecific: false, barMonitor: "DP-1"}); len(got) != 3 {
		t.Errorf("unscoped = %v", wsIDs(got))
	}
	if got := cwsCollectDisplayed(all, cwsFilter{monitorSpecific: true}); len(got) != 3 {
		t.Errorf("no bar monitor = %v", wsIDs(got))
	}
}

func TestCwsLabelStrategies(t *testing.T) {
	for _, tc := range []struct {
		num      int
		name     string
		hasName  bool
		strategy config.LabelStrategy
		want     string
		ok       bool
	}{
		{3, "web", true, config.LabelIndex, "3", true},
		{3, "web", true, config.LabelNameOrIndex, "web", true},
		{3, "", false, config.LabelNameOrIndex, "3", true},
		{3, "", false, config.LabelNameOnly, "", false},
		{3, "web", true, config.LabelIndexAndName, "3: web", true},
		{3, "", false, config.LabelIndexAndName, "3", true},
		// sway's unnumbered workspaces
		{-1, "chat", true, config.LabelIndex, "", false},
		{-1, "chat", true, config.LabelIndexAndName, "chat", true},
		{-1, "", false, config.LabelIndexAndName, "", false},
	} {
		got, ok := cwsLabel(tc.num, tc.name, tc.hasName, tc.strategy)
		if got != tc.want || ok != tc.ok {
			t.Errorf("label(%d, %q, %s) = %q %v, want %q %v", tc.num, tc.name, tc.strategy, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCwsStylePrefersNameThenID(t *testing.T) {
	m := map[string]config.NamedWorkspaceStyle{
		"web": {Label: "W", LabelSet: true},
		"5":   {Label: "five", LabelSet: true},
	}
	named := cwsWorkspace{id: 5, name: "web", hasName: true}
	if s, ok := cwsStyleFor(named, m); !ok || s.Label != "W" {
		t.Errorf("named = %+v %v, want the name entry", s, ok)
	}
	unmatched := cwsWorkspace{id: 5, name: "mail", hasName: true}
	if s, ok := cwsStyleFor(unmatched, m); !ok || s.Label != "five" {
		t.Errorf("fallback = %+v %v, want the id entry", s, ok)
	}
	if _, ok := cwsStyleFor(cwsWorkspace{id: 6}, m); ok {
		t.Error("no entry matched but a style came back")
	}
}

func TestCwsNameClassSanitizes(t *testing.T) {
	if got := cwsNameClass("2:web ü"); got != "ws-name-2_web__" {
		t.Errorf("class = %q", got)
	}
	if got := cwsIDClass(5); got != "ws-id-5" {
		t.Errorf("id class = %q", got)
	}
}

func TestCwsOverrideColorCascade(t *testing.T) {
	red, _ := config.ParseColorValue("#ff0000")
	green, _ := config.ParseColorValue("#00ff00")
	m := map[string]config.NamedWorkspaceStyle{
		"5":   {Color: red, ColorSet: true},
		"web": {Color: green, ColorSet: true},
		"x":   {Icon: "no-color"},
	}
	// A numeric key reaches the id class; later keys win.
	if c, ok := cwsOverrideColor([]string{"ws-id-5"}, m); !ok || c != red {
		t.Errorf("id match = %v %v", c, ok)
	}
	if c, ok := cwsOverrideColor([]string{"ws-id-5", "ws-name-web"}, m); !ok || c != green {
		t.Errorf("both match = %v %v, want the later key (web)", c, ok)
	}
	// A numeric key also reaches a numeric name.
	if c, ok := cwsOverrideColor([]string{"ws-id-99", "ws-name-5"}, m); !ok || c != red {
		t.Errorf("numeric name = %v %v", c, ok)
	}
	if _, ok := cwsOverrideColor([]string{"ws-id-7", "ws-name-x"}, m); ok {
		t.Error("a colorless entry produced an override")
	}
}

func TestCwsAppIconsDedupeOrderAndUrgency(t *testing.T) {
	cfg := config.DefaultsNiriWorkspaces()
	windows := []cwsWindow{
		{id: 3, workspace: 1, hasWorkspace: true, app: appicons.Window{AppID: "firefox", HasAppID: true}, order: [2]int{2, 1}},
		{id: 1, workspace: 1, hasWorkspace: true, app: appicons.Window{AppID: "foot", HasAppID: true}, order: [2]int{1, 1}},
		{id: 2, workspace: 1, hasWorkspace: true, app: appicons.Window{AppID: "firefox", HasAppID: true}, order: [2]int{3, 1}, urgent: true},
		{id: 9, workspace: 2, hasWorkspace: true},
	}
	on := cwsWindowsOn(windows, 1)
	if len(on) != 3 || on[0].id != 1 || on[1].id != 3 || on[2].id != 2 {
		t.Fatalf("windows on 1 = %+v, want layout order", on)
	}
	icons := cwsCollectAppIcons(on, cfg, map[uint64]bool{2: true})
	if len(icons) != 2 || !reflect.DeepEqual(icons[1].windowIDs, []uint64{3, 2}) || !icons[1].urgent || icons[0].urgent {
		t.Fatalf("deduped = %+v", icons)
	}
	cfg.AppIconsDedupe = false
	if icons := cwsCollectAppIcons(on, cfg, nil); len(icons) != 3 {
		t.Fatalf("no dedupe = %+v, want one icon per window", icons)
	}
}

func TestCwsBuildModelsBlinkGatesUrgency(t *testing.T) {
	cfg := config.DefaultsSwayWorkspaces()
	urgent := occupiedWS(1, 1, "DP-1")
	urgent.urgent = true
	all := []cwsWorkspace{urgent}
	on := cwsBuildModels(cwsWorkspaces, all, nil, cfg, "DP-1", false, true)
	if !hasClass(on[0].classes, "urgent") {
		t.Errorf("blink on: classes %v miss urgent", on[0].classes)
	}
	off := cwsBuildModels(cwsWorkspaces, all, nil, cfg, "DP-1", false, false)
	if hasClass(off[0].classes, "urgent") {
		t.Errorf("blink off: classes %v carry urgent", off[0].classes)
	}
	cfg.UrgentShow = false
	if m := cwsBuildModels(cwsWorkspaces, all, nil, cfg, "DP-1", false, true); hasClass(m[0].classes, "urgent") {
		t.Error("urgent-show = false still marks the class")
	}
	cfg.UrgentShow, cfg.UrgentMode = true, config.UrgentApplication
	if m := cwsBuildModels(cwsWorkspaces, all, nil, cfg, "DP-1", false, true); !hasClass(m[0].classes, "urgent-application") {
		t.Error("urgent-mode application misses its class")
	}
}

func TestCwsClassesMatchComputeCSSClasses(t *testing.T) {
	cfg := config.DefaultsNiriWorkspaces()
	ws := occupiedWS(5, 2, "DP-1")
	ws.active, ws.focused, ws.name, ws.hasName = true, true, "web", true
	m := cwsBuildModels(cwsWorkspaces, []cwsWorkspace{ws}, nil, cfg, "", true, false)
	want := []string{"workspace", "active", "focused", "indicator-background", "vertical", "ws-id-5", "ws-name-web"}
	if !reflect.DeepEqual(m[0].classes, want) {
		t.Errorf("classes = %v, want %v", m[0].classes, want)
	}
}

func TestCwsShowRules(t *testing.T) {
	cfg := config.DefaultsNiriWorkspaces()
	labelled := cwsButtonModel{label: "1", hasLabel: true}
	if !labelled.showLabel(cfg.DisplayMode) || labelled.showIcon(cfg.DisplayMode) || labelled.showDivider(cfg) {
		t.Error("a labelled button without app icons: label only")
	}
	iconed := cwsButtonModel{label: "1", hasLabel: true, icon: "ld-globe-symbolic"}
	if iconed.showLabel(cfg.DisplayMode) || !iconed.showIcon(cfg.DisplayMode) {
		t.Error("a mapped icon replaces the label")
	}
	if iconed.showIcon(config.DisplayModeNone) || labelled.showLabel(config.DisplayModeNone) {
		t.Error("display-mode none shows something")
	}
	cfg.AppIconsShow = true
	if !labelled.showDivider(cfg) {
		t.Error("app icons on: the divider shows beside the label")
	}
	cfg.Divider = ""
	if labelled.showDivider(cfg) {
		t.Error("an empty divider shows")
	}
}

// fakeCws records the actions the bindings reach.
type fakeCws struct {
	workspaces []cwsWorkspace
	windows    []cwsWindow
	calls      chan string
	closed     bool
}

func newFakeCws(workspaces []cwsWorkspace, windows []cwsWindow) *fakeCws {
	return &fakeCws{workspaces: workspaces, windows: windows, calls: make(chan string, 16)}
}

func (f *fakeCws) snapshot() ([]cwsWorkspace, []cwsWindow, error) {
	return f.workspaces, f.windows, nil
}

func (f *fakeCws) focus(_ context.Context, ws cwsWorkspace) error {
	f.calls <- "focus:" + cwsIDClass(ws.id)
	return nil
}
func (f *fakeCws) focusNext(context.Context) error     { f.calls <- "next"; return nil }
func (f *fakeCws) focusPrevious(context.Context) error { f.calls <- "previous"; return nil }
func (f *fakeCws) focusLast(context.Context) error     { f.calls <- "last"; return nil }
func (f *fakeCws) subscribe() (<-chan struct{}, func(), error) {
	return make(chan struct{}), func() {}, nil
}
func (f *fakeCws) close() { f.closed = true }

func (f *fakeCws) expect(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-f.calls:
		if got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no call, want %q", want)
	}
}

func (f *fakeCws) expectNone(t *testing.T) {
	t.Helper()
	select {
	case got := <-f.calls:
		t.Fatalf("unexpected call %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func newTestCws(t *testing.T, cfg config.CompositorWorkspacesConfig, backend cwsBackend) *cwsModule {
	t.Helper()
	ctx := newTestContext(t, config.Defaults())
	ctx.Connector = "DP-1"
	m, err := newCwsModule(ctx, "niri", cwsWorkspaces, cfg, backend)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCwsModuleBuildsOneButtonPerDisplayedWorkspace(t *testing.T) {
	active := occupiedWS(1, 1, "DP-1")
	active.active = true
	f := newFakeCws([]cwsWorkspace{active, occupiedWS(2, 2, "DP-1"), emptyWS(3, 3, "DP-1"), occupiedWS(4, 1, "HDMI-A-1")}, nil)
	m := newTestCws(t, config.DefaultsNiriWorkspaces(), f)
	buttons := m.root.buttons()
	// niri hides the trailing empty 3; 4 is another output's.
	if len(buttons) != 2 {
		t.Fatalf("buttons = %d, want 2", len(buttons))
	}
	if !widget.HasClass(m.root, "workspaces") || !widget.HasClass(m.root, "niri") {
		t.Error("container misses workspaces/niri classes")
	}
	if !widget.HasClass(buttons[0], "active") || !widget.HasClass(buttons[1], "occupied") {
		t.Error("buttons miss their state classes")
	}
	m.Stop()
	if !f.closed {
		t.Error("Stop left the backend open")
	}
}

func TestCwsColorsFollowTheStylesheet(t *testing.T) {
	cfg := config.DefaultsNiriWorkspaces()
	m := newTestCws(t, cfg, newFakeCws(nil, nil))
	palette := styling.Default()
	resolve := func(cv config.ColorValue) render.Color { c, _ := styling.ResolveColor(cv, palette); return c }
	accent, onAccent := resolve(cfg.ActiveColor), resolve(mustToken(config.TokenFgOnAccent))

	active := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "active", "indicator-background"}})
	if active.bg != accent || active.label != onAccent || active.hoverBg != accent {
		t.Errorf("active background = %+v", active)
	}
	m.cfg.ActiveIndicator = config.ActiveUnderline
	under := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "active", "indicator-underline"}})
	if under.bg != 0 || under.underline != accent || under.label != accent {
		t.Errorf("active underline = %+v", under)
	}
	occ := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "occupied"}})
	if occ.label != resolve(cfg.OccupiedColor) || occ.bg != 0 || occ.hoverBg != styling.ColorMix(accent, transparentColor, 15) {
		t.Errorf("occupied = %+v", occ)
	}
	empty := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "empty"}})
	if empty.label != resolve(cfg.EmptyColor) || empty.emptyIcon != resolve(cfg.EmptyColor) || empty.icon != m.fg() {
		t.Errorf("empty = %+v, want empty-colored label and empty icon, a plain mapped icon", empty)
	}
	if empty.opacity != 1 {
		t.Errorf("non-urgent opacity = %v", empty.opacity)
	}
	if u := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "empty", "urgent"}}); u.opacity != 0.5 {
		t.Errorf("urgent opacity = %v, want 0.5", u.opacity)
	}
	if u := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "empty", "urgent", "urgent-application"}}); u.opacity != 1 {
		t.Errorf("urgent-application opacity = %v, want the icon to pulse instead", u.opacity)
	}
	// The override replaces every state color but not the hover mix.
	red, _ := config.ParseColorValue("#ff0000")
	m.cfg.WorkspaceMap = map[string]config.NamedWorkspaceStyle{"web": {Color: red, ColorSet: true}}
	over := m.cwsResolveColors(cwsButtonModel{classes: []string{"workspace", "occupied", "ws-name-web"}})
	if over.label != resolve(red) || over.hoverBg != styling.ColorMix(accent, transparentColor, 15) {
		t.Errorf("override = %+v", over)
	}
}

func TestCwsButtonMinSize(t *testing.T) {
	m := newTestCws(t, config.DefaultsNiriWorkspaces(), newFakeCws([]cwsWorkspace{occupiedWS(1, 1, "DP-1")}, nil))
	b := m.root.buttons()[0]
	sz := b.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 500}})
	if sz.W < 24 || sz.H < 24 {
		t.Errorf("button = %v, want at least --bar-space-lg (24px) square", sz)
	}
}

func TestCwsClicksRouteThroughTheRouter(t *testing.T) {
	f := newFakeCws([]cwsWorkspace{occupiedWS(1, 1, "DP-1"), occupiedWS(2, 2, "DP-1")}, nil)
	cfg := config.DefaultsSwayWorkspaces()
	cfg.Click.MiddleClick = config.ParseWorkspaceClickAction("focus:last")
	m := newTestCws(t, cfg, f)
	m.root.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 40}})
	m.root.Arrange(render.Rect{W: 500, H: 40})
	second := m.root.buttons()[1]
	p := widget.Point{X: second.Bounds().X + 2, Y: 10}
	router := &widget.Router{Root: m.root}

	router.Move(p)
	if !second.Hovered {
		t.Error("routed hover missed the button")
	}
	router.Press(widget.BTNLeft, p)
	router.Release(widget.BTNLeft, p)
	f.expect(t, "focus:ws-id-2")

	router.Press(widget.BTNMiddle, p)
	f.expect(t, "last")
	// Right click defaults to none.
	router.Press(widget.BTNRight, p)
	f.expectNone(t)

	if !second.ScrollInput(1) {
		t.Error("scroll was not consumed")
	}
	f.expect(t, "next")
	second.ScrollInput(-1)
	f.expect(t, "previous")
}

func TestCwsScrollIgnoresFocusThis(t *testing.T) {
	f := newFakeCws([]cwsWorkspace{occupiedWS(1, 1, "DP-1")}, nil)
	cfg := config.DefaultsSwayWorkspaces()
	cfg.Click.ScrollDown = config.ParseWorkspaceClickAction("focus:this")
	cfg.Click.ScrollUp = config.ParseWorkspaceClickAction("")
	m := newTestCws(t, cfg, f)
	b := m.root.buttons()[0]
	if !b.ScrollInput(1) || !b.ScrollInput(-1) {
		t.Error("scroll not consumed (Propagation::Stop) with inert bindings")
	}
	f.expectNone(t)
}

func TestCwsBlinkRunsOnlyWhileUrgent(t *testing.T) {
	urgent := occupiedWS(1, 1, "DP-1")
	urgent.urgent = true
	f := newFakeCws([]cwsWorkspace{urgent}, nil)
	m := newTestCws(t, config.DefaultsNiriWorkspaces(), f)
	if m.blinkStop == nil || !m.blinkOn {
		t.Fatal("urgent workspace: blink not started on")
	}
	if !widget.HasClass(m.root.buttons()[0], "urgent") {
		t.Error("blink on: button misses urgent")
	}
	m.blinkTick()
	if m.blinkOn || widget.HasClass(m.root.buttons()[0], "urgent") {
		t.Error("tick did not toggle the pulse off")
	}
	// Urgency clears: the blink stops and stays off.
	f.workspaces = []cwsWorkspace{occupiedWS(1, 1, "DP-1")}
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if m.blinkStop != nil || m.blinkOn {
		t.Error("blink still running without urgency")
	}
	// An urgent window on a displayed workspace also starts it; one on
	// a hidden workspace does not.
	f.windows = []cwsWindow{{id: 7, workspace: 99, hasWorkspace: true, urgent: true}}
	_ = m.refresh()
	if m.blinkStop != nil {
		t.Error("an urgent window on an undisplayed workspace started the blink")
	}
	f.windows[0].workspace = 1
	_ = m.refresh()
	if m.blinkStop == nil {
		t.Error("an urgent window on a displayed workspace did not start the blink")
	}
}

func TestCwsBorderShowUsesTheButtonBorder(t *testing.T) {
	cfg := config.DefaultsNiriWorkspaces()
	m := newTestCws(t, cfg, newFakeCws(nil, nil))
	if m.root.borders.any() || widget.HasClass(m.root, "border-all") {
		t.Error("border-show off draws a border")
	}
	cfg.BorderShow = true
	m = newTestCws(t, cfg, newFakeCws(nil, nil))
	if !m.root.borders.any() || !widget.HasClass(m.root, "border-all") {
		t.Error("border-show on: no border at the default all location")
	}
}

func TestCwsModuleIsNotWrappedInButtonChrome(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	row := widget.NewBox(widget.Row, 0, 0)
	m := newTestCws(t, config.DefaultsNiriWorkspaces(), newFakeCws(nil, nil))
	factories["test-cws"] = func(ModuleContext) (Module, error) { return m, nil }
	defer delete(factories, "test-cws")
	if err := appendModule(row, config.BarItem{Module: "test-cws"}, ctx); err != nil {
		t.Fatal(err)
	}
	if got := row.Children()[0]; got != widget.Widget(m.root) {
		t.Errorf("appended %T, want the container itself", got)
	}
}

func TestCwsFactoriesRequireTheirCompositor(t *testing.T) {
	t.Setenv("SWAYSOCK", "")
	t.Setenv("NIRI_SOCKET", "")
	ctx := newTestContext(t, config.Defaults())
	for _, name := range []string{"sway-workspaces", "niri-workspaces"} {
		if _, err := Create(name, ctx); err == nil {
			t.Errorf("%s without its compositor: want an error", name)
		}
	}
}
