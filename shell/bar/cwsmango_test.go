package bar

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// nineTags is methods.rs's nine_tags as a mango frame fragment.
func nineTags(active, occupied int) string {
	parts := make([]string, 0, 9)
	for i := 1; i <= 9; i++ {
		count := 0
		if i == occupied {
			count = 1
		}
		parts = append(parts, fmt.Sprintf(`{"index":%d,"is_active":%v,"is_urgent":false,"layout":"T","client_count":%d}`, i, i == active, count))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// fakeMangoSocket streams one monitors frame and one clients frame and
// records dispatches.
func fakeMangoSocket(t *testing.T, monitors, clients string) chan string {
	t.Helper()
	dispatched := make(chan string, 8)
	path := serveUnix(t, "mango.sock", func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return
		}
		switch line {
		case "watch all-monitors\n":
			_, _ = conn.Write([]byte(`{"monitors":` + monitors + "}\n"))
			time.Sleep(2 * time.Second)
		case "watch all-clients\n":
			_, _ = conn.Write([]byte(`{"clients":` + clients + "}\n"))
			time.Sleep(2 * time.Second)
		default:
			dispatched <- strings.TrimSuffix(line, "\n")
			_, _ = conn.Write([]byte("{}\n"))
		}
	})
	t.Setenv("MANGO_INSTANCE_SIGNATURE", path)
	return dispatched
}

// newTestMango builds the module and waits until both frames landed.
func newTestMango(t *testing.T, cfg *config.Config, connector string) *cwsModule {
	t.Helper()
	ctx := newTestContext(t, cfg)
	ctx.Connector = connector
	module, err := Create("mango-workspaces", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*cwsModule)
	t.Cleanup(m.Stop)
	backend := m.backend.(*mangoBackend)
	deadline := time.Now().Add(2 * time.Second)
	for !backend.watch.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("frames never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	return m
}

func tagIndices(m *cwsModule) []uint64 {
	out := make([]uint64, 0, len(m.models))
	for _, model := range m.models {
		out = append(out, model.ws.id)
	}
	return out
}

func TestMangoHideEmptyAndMinTagCount(t *testing.T) {
	monitors := `[{"name":"DP-1","active":true,"tags":` + nineTags(1, 1) + `,"active_tags":[1],"active_client":{"id":null},"keymode":"","keyboardlayout":""}]`
	for _, tc := range []struct {
		hide bool
		min  int
		want []uint64
	}{
		{false, 0, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{true, 0, []uint64{1}},
		{true, 5, []uint64{1, 2, 3, 4, 5}},
	} {
		fakeMangoSocket(t, monitors, "[]")
		cfg := config.Defaults()
		cfg.MangoWorkspaces.HideEmpty, cfg.MangoWorkspaces.MinTagCount = tc.hide, tc.min
		m := newTestMango(t, cfg, "DP-1")
		if got := tagIndices(m); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("hide=%v min=%d: tags %v, want %v", tc.hide, tc.min, got, tc.want)
		}
	}
}

func TestMangoChoosesTheBarMonitorThenTheActiveOne(t *testing.T) {
	mon := func(name string, active bool, activeTag int) string {
		return fmt.Sprintf(`{"name":%q,"active":%v,"tags":%s,"active_tags":[%d],"active_client":{"id":null},"keymode":"","keyboardlayout":""}`, name, active, nineTags(activeTag, 0), activeTag)
	}
	monitors := "[" + mon("DP-1", false, 2) + "," + mon("DP-2", true, 7) + "]"
	fakeMangoSocket(t, monitors, "[]")
	if got := tagIndices(newTestMango(t, config.Defaults(), "DP-1")); !reflect.DeepEqual(got, []uint64{2}) {
		t.Errorf("bar DP-1 = %v, want its own active tag 2", got)
	}
	fakeMangoSocket(t, monitors, "[]")
	if got := tagIndices(newTestMango(t, config.Defaults(), "HDMI-A-1")); !reflect.DeepEqual(got, []uint64{7}) {
		t.Errorf("unknown bar monitor = %v, want the active DP-2's tag 7", got)
	}
}

func TestMangoTagButtonsAndClients(t *testing.T) {
	monitors := `[{"name":"DP-1","active":true,"tags":[{"index":1,"is_active":true,"is_urgent":false,"layout":"T","client_count":2},{"index":3,"is_active":false,"is_urgent":false,"layout":"T","client_count":1}],"active_tags":[1],"active_client":{"id":null},"keymode":"","keyboardlayout":""}]`
	clients := `[{"id":6,"title":"a","appid":"foot","monitor":"DP-1","tags":[1,3],"is_urgent":false,"is_focused":true},` +
		`{"id":3,"title":"b","appid":"firefox","monitor":"DP-2","tags":[1],"is_urgent":false,"is_focused":false},` +
		`{"id":2,"title":"c","appid":"kitty","monitor":"DP-1","tags":[1],"is_urgent":false,"is_focused":false}]`
	fakeMangoSocket(t, monitors, clients)
	cfg := config.Defaults()
	cfg.MangoWorkspaces.Shared.AppIconsShow = true
	cfg.MangoWorkspaces.Shared.AppIconsDedupe = false
	cfg.MangoWorkspaces.Shared.WorkspaceMap = map[string]config.NamedWorkspaceStyle{"3": {Label: "web", LabelSet: true}}
	m := newTestMango(t, cfg, "DP-1")
	if len(m.models) != 2 {
		t.Fatalf("models = %+v", m.models)
	}
	one, three := m.models[0], m.models[1]
	if one.label != "1" || three.label != "web" {
		t.Errorf("labels = %q, %q, want the index and the tag-map label", one.label, three.label)
	}
	// DP-2's client is not this monitor's; client 6 sits on both tags.
	var ids []uint64
	for _, icon := range one.appIcons {
		ids = append(ids, icon.windowIDs...)
	}
	if !reflect.DeepEqual(ids, []uint64{6, 2}) {
		t.Errorf("tag 1 clients = %v, want [6 2] in list order", ids)
	}
	if len(three.appIcons) != 1 || three.appIcons[0].windowIDs[0] != 6 {
		t.Errorf("tag 3 clients = %+v", three.appIcons)
	}
	want := []string{"workspace", "active", "indicator-background", "tag-1"}
	if !reflect.DeepEqual(one.classes, want) {
		t.Errorf("classes = %v, want %v (no focused, tag-N)", one.classes, want)
	}
	if !widget.HasClass(m.root, "mango") {
		t.Error("container misses the mango class")
	}
}

func TestMangoClicksDispatch(t *testing.T) {
	monitors := `[{"name":"DP-1","active":true,"tags":` + nineTags(1, 1) + `,"active_tags":[1],"active_client":{"id":null},"keymode":"","keyboardlayout":""}]`
	dispatched := fakeMangoSocket(t, monitors, "[]")
	cfg := config.Defaults()
	cfg.MangoWorkspaces.Shared.Click.MiddleClick = config.ParseWorkspaceClickAction("focus:last")
	m := newTestMango(t, cfg, "DP-1")
	b := m.root.buttons()[0]
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-dispatched:
			if got != want {
				t.Fatalf("dispatched %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no dispatch, want %q", want)
		}
	}
	b.ClickAt(widget.Point{})
	expect("dispatch view,1")
	b.ScrollInput(1)
	expect("dispatch viewtoright")
	b.ScrollInput(-1)
	expect("dispatch viewtoleft")
	// focus:last has no mango action: nothing is sent.
	b.PointerButton(widget.BTNMiddle)
	select {
	case got := <-dispatched:
		t.Fatalf("focus:last dispatched %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMangoShowRules(t *testing.T) {
	tag := cwsButtonModel{flavor: cwsTags, label: "1", hasLabel: true}
	iconed := cwsButtonModel{flavor: cwsTags, label: "1", hasLabel: true, icon: "ld-globe-symbolic"}
	if !iconed.showLabel(config.DisplayModeLabel) || iconed.showIcon(config.DisplayModeLabel) {
		t.Error("label mode: tags show the label even with a mapped icon, and no icon")
	}
	if iconed.showLabel(config.DisplayModeIcon) || !iconed.showIcon(config.DisplayModeIcon) {
		t.Error("icon mode: the mapped icon replaces the label")
	}
	if !tag.showLabel(config.DisplayModeIcon) {
		t.Error("icon mode without a mapped icon falls back to the label")
	}
	if tag.showLabel(config.DisplayModeNone) || iconed.showIcon(config.DisplayModeNone) {
		t.Error("none shows something")
	}
}

func TestMangoOverrideTargetsTagClass(t *testing.T) {
	red, _ := config.ParseColorValue("#ff0000")
	m := map[string]config.NamedWorkspaceStyle{"4": {Color: red, ColorSet: true}}
	if c, ok := cwsOverrideColor([]string{"tag-4"}, m); !ok || c != red {
		t.Errorf("tag-4 override = %v %v", c, ok)
	}
	if _, ok := cwsOverrideColor([]string{"tag-5"}, m); ok {
		t.Error("tag-5 picked up tag 4's color")
	}
}
