package bar

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
)

func occupiedHypr(id int, monitor string) hyprland.Workspace {
	return hyprland.Workspace{ID: id, Name: itoa(id), Windows: 1, Monitor: monitor}
}

func emptyHypr(id int, monitor string) hyprland.Workspace {
	return hyprland.Workspace{ID: id, Name: itoa(id), Monitor: monitor}
}

func itoa(i int) string { return strconv.Itoa(i) }

func filteredIDs(list []hyprFiltered) []int {
	out := make([]int, len(list))
	for i, ws := range list {
		out[i] = ws.id
	}
	return out
}

// The cases are filtering.rs's tests.
func TestHyprFilterWorkspaces(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ws       []hyprland.Workspace
		f        hyprFilter
		want     []int
		has, not []int
	}{
		{
			name: "monitor specific", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(2, "DP-2"), occupiedHypr(3, "DP-1")},
			f: hyprFilter{monitorSpecific: true, activeID: 1, barMonitor: "DP-1"}, want: []int{1, 3},
		},
		{
			name: "special excluded", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(-99, "DP-1")},
			f: hyprFilter{activeID: 1}, want: []int{1},
		},
		{
			name: "special included", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(-99, "DP-1")},
			f: hyprFilter{showSpecial: true, activeID: 1}, want: []int{-99, 1},
		},
		{
			name: "ignore", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(2, "DP-1"), occupiedHypr(10, "DP-1")},
			f: hyprFilter{activeID: 1, ignore: []string{"10"}}, want: []int{1, 2},
		},
		{
			name: "placeholders", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{minCount: 3, activeID: 1}, want: []int{1, 2, 3},
		},
		{
			name: "active placeholder", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{minCount: 3, activeID: 5}, has: []int{5},
		},
		{
			name: "occupied beyond limit", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(9, "DP-1")},
			f: hyprFilter{minCount: 8, activeID: 1}, has: []int{9},
		},
		{
			name: "empty beyond limit", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), emptyHypr(9, "DP-1")},
			f: hyprFilter{minCount: 8, activeID: 1}, not: []int{9},
		},
		{
			name: "active on other monitor", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(9, "DP-2")},
			f: hyprFilter{monitorSpecific: true, minCount: 8, activeID: 9, barMonitor: "DP-1"}, not: []int{9},
		},
		{
			name: "active on other monitor by rule", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{monitorSpecific: true, minCount: 8, activeID: 9, barMonitor: "DP-1", rules: map[int]string{9: "DP-2"}}, not: []int{9},
		},
		{
			name: "occupied beyond limit on bar monitor", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(9, "DP-1")},
			f: hyprFilter{monitorSpecific: true, minCount: 8, activeID: 1, barMonitor: "DP-1"}, has: []int{9},
		},
		{
			name: "occupied beyond limit on other monitor", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1"), occupiedHypr(9, "DP-2")},
			f: hyprFilter{monitorSpecific: true, minCount: 8, activeID: 1, barMonitor: "DP-1"}, not: []int{9},
		},
		{
			name: "active placeholder by bar rule", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{monitorSpecific: true, minCount: 8, activeID: 9, barMonitor: "DP-1", rules: map[int]string{9: "DP-1"}}, has: []int{9},
		},
		{
			name: "placeholders need rules", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{monitorSpecific: true, minCount: 5, activeID: 1, barMonitor: "DP-1", rules: map[int]string{1: "DP-1", 3: "DP-1"}}, has: []int{1, 3}, not: []int{2, 4, 5},
		},
		{
			name: "global placeholders", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{minCount: 5, activeID: 1}, want: []int{1, 2, 3, 4, 5},
		},
		{
			name: "placeholders ignore", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{minCount: 5, activeID: 1, ignore: []string{"3", "5"}}, want: []int{1, 2, 4},
		},
		{
			name: "active ignore", ws: []hyprland.Workspace{occupiedHypr(1, "DP-1")},
			f: hyprFilter{minCount: 5, activeID: 10, ignore: []string{"10"}}, not: []int{10},
		},
	} {
		got := filteredIDs(hyprFilterWorkspaces(tc.ws, tc.f))
		if tc.want != nil && !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ids %v, want %v", tc.name, got, tc.want)
		}
		for _, id := range tc.has {
			if !containsInt(got, id) {
				t.Errorf("%s: ids %v miss %d", tc.name, got, id)
			}
		}
		for _, id := range tc.not {
			if containsInt(got, id) {
				t.Errorf("%s: ids %v carry %d", tc.name, got, id)
			}
		}
	}
}

func containsInt(list []int, v int) bool { return slices.Contains(list, v) }

func TestHyprRelativeNumberingAndNavigation(t *testing.T) {
	rules := map[int]string{5: "DP-1", 3: "DP-1", 7: "DP-2", -1: "DP-1"}
	monWS := hyprMonitorWorkspaces("DP-1", rules)
	if !reflect.DeepEqual(monWS, []int{3, 5}) {
		t.Fatalf("monitor workspaces = %v, want sorted positives", monWS)
	}
	if got := hyprDisplayID(5, config.NumberingRelative, "DP-1", monWS); got != 2 {
		t.Errorf("relative 5 = %d, want 2", got)
	}
	if got := hyprDisplayID(10, config.NumberingRelative, "DP-1", monWS); got != 10 {
		t.Errorf("unruled 10 = %d, want its id", got)
	}
	if got := hyprDisplayID(5, config.NumberingAbsolute, "DP-1", monWS); got != 5 {
		t.Errorf("absolute = %d", got)
	}
	if got := hyprDisplayID(5, config.NumberingRelative, "", monWS); got != 5 {
		t.Errorf("no bar monitor = %d", got)
	}
	for _, tc := range [][4]int{{4, 1, 5, 0}, {0, -1, 5, 4}, {2, 1, 5, 3}, {2, -1, 5, 1}} {
		if got := hyprNavigate(tc[0], tc[1], tc[2]); got != tc[3] {
			t.Errorf("navigate(%d,%d,%d) = %d, want %d", tc[0], tc[1], tc[2], got, tc[3])
		}
	}
}

func TestHyprLabelsAndClasses(t *testing.T) {
	if got := hyprLabel(2, 5, "5", true); got != "2" {
		t.Errorf("default name with use-name = %q, want the display id", got)
	}
	if got := hyprLabel(5, 5, "web", true); got != "web" {
		t.Errorf("custom name = %q", got)
	}
	if got := hyprLabel(5, 5, "web", false); got != "5" {
		t.Errorf("use-name off = %q", got)
	}
	if got := hyprLabel(-98, -98, "special:magic", false); got != "-98" {
		t.Errorf("special without use-name = %q, want the id as Rust renders it", got)
	}
	if hyprIDClass(-98) != "workspace-id-neg98" || hyprIDClass(3) != "workspace-id-3" {
		t.Error("id classes wrong")
	}
	if hyprState(true, true, 1) != "active" || hyprState(false, true, 1) != "active-other-monitor" ||
		hyprState(false, false, 1) != "occupied" || hyprState(false, false, 0) != "empty" {
		t.Error("state order wrong")
	}
}

func TestHyprResolveIcon(t *testing.T) {
	user := map[string]string{"title:*youtube*": "yt", "class:Steam": "steam", "foot": "term"}
	if got := hyprResolveIcon("firefox", "Music - YouTube", user, "fb"); got != "yt" {
		t.Errorf("title = %q", got)
	}
	if got := hyprResolveIcon("steam", "", user, "fb"); got != "steam" {
		t.Errorf("class: prefix, case-folded = %q", got)
	}
	if got := hyprResolveIcon("Foot", "", user, "fb"); got != "term" {
		t.Errorf("bare pattern = %q", got)
	}
	if got := hyprResolveIcon("Firefox", "", nil, "fb"); got != "si-firefox-symbolic" {
		t.Errorf("default table (case-insensitive here) = %q", got)
	}
	if got := hyprResolveIcon("zz-none", "", nil, "fb"); got != "fb" {
		t.Errorf("fallback = %q", got)
	}
}

func TestHyprWorkspaceIconsDedupeByClass(t *testing.T) {
	clients := []hyprland.Client{
		{Address: "a", Class: "Kitty"},
		{Address: "b", Class: "kitty"},
		{Address: "c", Class: "firefox"},
		{Address: "d", Class: "kitty"},
	}
	for i := range clients {
		clients[i].Workspace.ID = 1
	}
	clients[3].Workspace.ID = 2
	cfg := config.DefaultsHyprlandWorkspaces().View()
	icons := hyprWorkspaceIcons(1, clients, cfg, map[string]bool{"b": true})
	if len(icons) != 2 || len(icons[0].windowIDs) != 2 || !icons[0].urgent || icons[1].urgent {
		t.Fatalf("deduped = %+v", icons)
	}
	cfg.AppIconsDedupe = false
	if icons := hyprWorkspaceIcons(1, clients, cfg, nil); len(icons) != 3 {
		t.Fatalf("no dedupe = %+v", icons)
	}
}

// fakeHypr is a scriptable Hyprland: canned replies per request, the
// dispatches recorded, and event lines pushed to the subscriber.
type fakeHypr struct {
	mu       sync.Mutex
	replies  map[string]string
	dispatch []string
	events   chan net.Conn
}

func startFakeHypr(t *testing.T, replies map[string]string) (*fakeHypr, *hyprland.Connection) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeHypr{replies: replies, events: make(chan net.Conn, 1)}
	cmdPath, evPath := filepath.Join(dir, ".socket.sock"), filepath.Join(dir, ".socket2.sock")
	serve := func(path string, fn func(net.Conn)) {
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go fn(conn)
			}
		}()
	}
	serve(cmdPath, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		req := string(buf[:n])
		f.mu.Lock()
		reply, ok := f.replies[req]
		if cmd, isDispatch := strings.CutPrefix(req, "dispatch "); isDispatch {
			f.dispatch = append(f.dispatch, cmd)
			if !ok {
				reply = "ok"
			}
		} else if !ok {
			reply = "[]"
		}
		f.mu.Unlock()
		_, _ = conn.Write(append([]byte(reply), '\x04'))
	})
	serve(evPath, func(conn net.Conn) { f.events <- conn })
	return f, hyprland.ConnectTo(cmdPath, evPath)
}

func (f *fakeHypr) set(req, reply string) {
	f.mu.Lock()
	f.replies[req] = reply
	f.mu.Unlock()
}

func (f *fakeHypr) dispatched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dispatch...)
}

const (
	hyprWorkspacesJSON = `[{"id":1,"name":"1","monitor":"DP-1","windows":2},{"id":2,"name":"2","monitor":"DP-1","windows":0},` +
		`{"id":3,"name":"3","monitor":"DP-2","windows":1},{"id":-98,"name":"special:magic","monitor":"DP-1","windows":1}]`
	hyprMonitorsJSON = `[{"id":0,"name":"DP-1","activeWorkspace":{"id":1,"name":"1"},"focused":true},` +
		`{"id":1,"name":"DP-2","activeWorkspace":{"id":3,"name":"3"},"focused":false}]`
	hyprClientsJSON = `[{"address":"0xaa","class":"kitty","title":"t","workspace":{"id":1,"name":"1"}},` +
		`{"address":"0xbb","class":"firefox","title":"f","workspace":{"id":1,"name":"1"}},` +
		`{"address":"0xcc","class":"foot","title":"x","workspace":{"id":3,"name":"3"}}]`
)

func newTestHypr(t *testing.T, mutate func(*config.Config)) (*fakeHypr, *hyprlandWorkspaces) {
	t.Helper()
	f, conn := startFakeHypr(t, map[string]string{
		"j/workspaces":      hyprWorkspacesJSON,
		"j/monitors":        hyprMonitorsJSON,
		"j/clients":         hyprClientsJSON,
		"j/workspacerules":  `[]`,
		"j/activeworkspace": `{"id":1,"name":"1","monitor":"DP-1","windows":2}`,
	})
	cfg := config.Defaults()
	if mutate != nil {
		mutate(cfg)
	}
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = conn
	ctx.Connector = "DP-1"
	module, err := Create("hyprland-workspaces", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return f, module.(*hyprlandWorkspaces)
}

func modelIDs(models []cwsButtonModel) []int {
	out := make([]int, len(models))
	for i, m := range models {
		out[i] = m.ws.num
	}
	return out
}

func TestNewHyprlandWorkspacesRequiresHyprland(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	ctx.Hyprland = nil
	if _, err := Create("hyprland-workspaces", ctx); err == nil {
		t.Fatal("no Hyprland connection: want an error, got a module")
	}
}

func TestHyprlandModuleBuildsTheBarMonitorsRow(t *testing.T) {
	_, m := newTestHypr(t, nil)
	if got := modelIDs(m.models); !reflect.DeepEqual(got, []int{-98, 1, 2}) {
		t.Fatalf("ids = %v, want DP-1's workspaces with the special first", got)
	}
	special, one, two := m.models[0], m.models[1], m.models[2]
	if !hasClass(one.classes, "active") || !hasClass(two.classes, "empty") || !hasClass(special.classes, "special") {
		t.Errorf("classes = %v / %v / %v", one.classes, two.classes, special.classes)
	}
	want := []string{"workspace", "indicator-background", "workspace-id-1", "active"}
	if !reflect.DeepEqual(one.classes, want) {
		t.Errorf("classes = %v, want %v", one.classes, want)
	}
	if one.label != "1" || special.label != "-98" {
		t.Errorf("labels = %q %q", one.label, special.label)
	}
	if !widget.HasClass(m.root, "workspaces") || widget.HasClass(m.root, "hyprland") {
		t.Error("the hyprland container is plain .workspaces")
	}
}

func TestHyprlandActiveOnOtherMonitor(t *testing.T) {
	_, m := newTestHypr(t, func(c *config.Config) { c.HyprlandWorkspaces.MonitorSpecific = false })
	var three *cwsButtonModel
	for i := range m.models {
		if m.models[i].ws.num == 3 {
			three = &m.models[i]
		}
	}
	if three == nil || !hasClass(three.classes, "active-other-monitor") {
		t.Fatalf("workspace 3 (DP-2's active) = %+v", three)
	}
	colors := m.cwsResolveColors(*three)
	if colors.bg != m.resolve(m.hcfg.ActiveOnOtherMonitorColor) {
		t.Errorf("other-monitor bg = %#x", colors.bg)
	}
}

func TestHyprlandUrgencyFromEvents(t *testing.T) {
	_, m := newTestHypr(t, nil)
	m.handle(hyprland.Event{Kind: hyprland.EventUrgent, Address: "bb"})
	if m.blinkStop == nil || !m.blinkOn {
		t.Fatal("urgent window did not start the blink")
	}
	if !hasClass(m.models[1].classes, "urgent") {
		t.Errorf("workspace 1 classes %v miss urgent", m.models[1].classes)
	}
	// Focusing another window keeps it; focusing the urgent one clears.
	m.handle(hyprland.Event{Kind: hyprland.EventActiveWindowV2, Address: "aa"})
	if len(m.urgent) != 1 {
		t.Fatal("focusing an unrelated window cleared urgency")
	}
	m.handle(hyprland.Event{Kind: hyprland.EventActiveWindowV2, Address: "bb"})
	if len(m.urgent) != 0 || m.blinkStop != nil || hasClass(m.models[1].classes, "urgent") {
		t.Error("focusing the urgent window left the pulse running")
	}
	// Switching to the urgent window's workspace clears it too.
	m.handle(hyprland.Event{Kind: hyprland.EventUrgent, Address: "aa"})
	m.handle(hyprland.Event{Kind: hyprland.EventWorkspaceV2, ID: 1, Name: "1"})
	if len(m.urgent) != 0 || m.blinkStop != nil {
		t.Error("activating the workspace kept its urgency")
	}
}

func TestHyprlandPerIconUrgency(t *testing.T) {
	_, m := newTestHypr(t, func(c *config.Config) {
		c.HyprlandWorkspaces.AppIconsShow = true
		c.HyprlandWorkspaces.UrgentMode = config.UrgentApplication
	})
	m.handle(hyprland.Event{Kind: hyprland.EventUrgent, Address: "bb"})
	one := m.models[1]
	if hasClass(one.classes, "urgent") {
		t.Error("per-icon mode marked the whole button")
	}
	if len(one.appIcons) != 2 || one.appIcons[0].urgent || !one.appIcons[1].urgent {
		t.Errorf("icons = %+v, want firefox's icon urgent", one.appIcons)
	}
	// The blink's off phase clears the icon.
	m.blinkOn = false
	m.render()
	if m.models[1].appIcons[1].urgent {
		t.Error("off phase kept the icon urgent")
	}
}

func TestHyprlandStaleUrgencyPrunedOnClientChange(t *testing.T) {
	f, m := newTestHypr(t, nil)
	m.handle(hyprland.Event{Kind: hyprland.EventUrgent, Address: "bb"})
	f.set("j/clients", `[{"address":"0xaa","class":"kitty","title":"t","workspace":{"id":1,"name":"1"}}]`)
	m.handle(hyprland.Event{Kind: hyprland.EventCloseWindow, Address: "bb"})
	if len(m.urgent) != 0 || m.blinkStop != nil {
		t.Error("closed urgent window still tracked")
	}
}

func TestHyprlandMinCountPlaceholdersAndRelativeRules(t *testing.T) {
	f, m := newTestHypr(t, func(c *config.Config) {
		c.HyprlandWorkspaces.MinWorkspaceCount = 4
		c.HyprlandWorkspaces.Numbering = config.NumberingRelative
	})
	// No rules: monitor-specific placeholders need rules.
	if got := modelIDs(m.models); !reflect.DeepEqual(got, []int{-98, 1, 2}) {
		t.Errorf("without rules = %v", got)
	}
	f.set("j/workspacerules", `[{"workspaceString":"2","monitor":"DP-1"},{"workspaceString":"4","monitor":"DP-1"},{"workspaceString":"3","monitor":"DP-2"}]`)
	m.handle(hyprland.Event{Kind: hyprland.EventConfigReloaded})
	if got := modelIDs(m.models); !reflect.DeepEqual(got, []int{-98, 1, 2, 4}) {
		t.Errorf("with rules = %v, want placeholder 4", got)
	}
	// Relative numbering: 2 and 4 are DP-1's first and second.
	labels := []string{m.models[2].label, m.models[3].label}
	if !reflect.DeepEqual(labels, []string{"1", "2"}) {
		t.Errorf("relative labels = %v", labels)
	}
}

func TestHyprlandClicksDispatch(t *testing.T) {
	f, m := newTestHypr(t, nil)
	wait := func(n int) []string {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if got := f.dispatched(); len(got) >= n {
				return got
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("dispatches = %v, want %d", f.dispatched(), n)
		return nil
	}
	buttons := m.root.buttons()
	buttons[2].ClickAt(widget.Point{})
	if got := wait(1); got[0] != "hl.dsp.focus({workspace = 2})" {
		t.Errorf("focus = %q", got[0])
	}
	// The special dispatches by quoted name; a Lua error falls back.
	f.set(`dispatch hl.dsp.focus({workspace = "name:special:magic"})`, "error: unknown")
	buttons[0].ClickAt(widget.Point{})
	got := wait(3)
	if got[1] != `hl.dsp.focus({workspace = "name:special:magic"})` || got[2] != "workspace name:special:magic" {
		t.Errorf("special dispatches = %q", got[1:])
	}
	// Scroll down from active 1 walks to 2 in the displayed order.
	buttons[1].ScrollInput(1)
	if got := wait(4); got[3] != "hl.dsp.focus({workspace = 2})" {
		t.Errorf("next = %q", got[3])
	}
	m.cfg.Click.MiddleClick = config.ParseWorkspaceClickAction("focus:last")
	buttons[1].PointerButton(widget.BTNMiddle)
	if got := wait(5); got[4] != "workspace previous" {
		t.Errorf("last = %q", got[4])
	}
}

func TestHyprlandModuleIsUnwrapped(t *testing.T) {
	_, m := newTestHypr(t, nil)
	if _, ok := Module(m).(interface{ ownsChrome() }); !ok {
		t.Error("hyprland workspaces get the module-button wrapper")
	}
}

func TestLoadFileAppliesHyprlandWorkspaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[modules.hyprland-workspaces]
display-mode = "icon"
active-color = "#89b4fa"
numbering = "relative"
min-workspace-count = 5
label-use-name = true
show-special = false
highlight-active-on-other-monitor = false
active-on-other-monitor-color = "#123456"
urgent-mode = "application"
app-icons-show = true
workspace-ignore = ["1?"]
left-click = "focus:last"

[modules.hyprland-workspaces.workspace-map.-98]
color = "#f38ba8"

[modules.hyprland-workspaces.workspace-map.10]
label = "ten"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	hw := c.HyprlandWorkspaces
	shared := hw.View()
	if shared.DisplayMode != config.DisplayModeIcon || shared.ActiveColor.Hex != "#89b4fa" ||
		hw.Numbering != config.NumberingRelative || hw.MinWorkspaceCount != 5 || !hw.LabelUseName || hw.ShowSpecial ||
		hw.HighlightActiveOnOtherMonitor || hw.ActiveOnOtherMonitorColor.Hex != "#123456" ||
		shared.UrgentMode != config.UrgentApplication || !shared.AppIconsShow ||
		shared.WorkspaceIgnore[0] != "1?" || shared.Click.LeftClick.Kind != config.WorkspaceClickFocusLast {
		t.Fatalf("config = %+v", hw)
	}
	if s := shared.WorkspaceMap["-98"]; s.Color == nil {
		t.Errorf("map[-98] = %+v", s)
	}
	if s := shared.WorkspaceMap["10"]; s.Label == nil || *s.Label != "ten" {
		t.Errorf("map[10] = %+v", s)
	}
}

func TestLoadFileRejectsBadHyprlandWorkspaces(t *testing.T) {
	for _, content := range []string{
		"display-mode = \"text\"",
		"active-indicator = \"glow\"",
		"numbering = \"roman\"",
		"min-workspace-count = 300",
		"active-color = \"not-a-token\"",
		"urgent-mode = \"loud\"",
		"[modules.hyprland-workspaces.workspace-map.web]\ncolor = \"#fff\"",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "[modules.hyprland-workspaces]\n" + content + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}

func TestHyprlandIgnoredEventsCostNoQuery(t *testing.T) {
	f, m := newTestHypr(t, nil)
	// A broken command socket would fail any query; ignored events must
	// not reach it.
	f.set("j/workspaces", "not json")
	before := len(m.models)
	for _, ev := range []hyprland.Event{
		{Kind: hyprland.EventWindowTitleV2, Address: "aa", Title: "x"},
		{Kind: hyprland.EventActiveWindowV2, Address: "aa"},
		{Kind: hyprland.EventSubmap, Name: "resize"},
	} {
		if m.relevant(ev) {
			t.Errorf("%s is relevant without a reason", ev.Kind)
		}
		m.handle(ev)
	}
	if len(m.models) != before {
		t.Error("an ignored event rebuilt the row")
	}
	// A title: pattern makes title events count.
	m.cfg.AppIconsShow = true
	m.cfg.AppIconMap = map[string]string{"title:*vim*": "ld-code-symbolic"}
	if !m.relevant(hyprland.Event{Kind: hyprland.EventWindowTitleV2}) {
		t.Error("title event ignored despite a title: pattern")
	}
}
