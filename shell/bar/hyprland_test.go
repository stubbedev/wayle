package bar

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/styling"
)

func wsFixtures() ([]hyprland.Workspace, []hyprland.Monitor) {
	workspaces := []hyprland.Workspace{
		{ID: 2, Name: "2", Monitor: "DP-1", Windows: 1},
		{ID: 1, Name: "1", Monitor: "DP-1", Windows: 3},
		{ID: 9, Name: "9", Monitor: "DP-1", Windows: 0},
		{ID: 5, Name: "5", Monitor: "HDMI-A-2", Windows: 1},
		{ID: -99, Name: "special:magic", Monitor: "DP-1", Windows: 1},
	}
	monitors := []hyprland.Monitor{
		{Name: "DP-1", Focused: true},
		{Name: "HDMI-A-2"},
	}
	monitors[0].ActiveWS.ID = 1
	monitors[1].ActiveWS.ID = 5
	return workspaces, monitors
}

func TestWorkspaceModelSortsAndFlags(t *testing.T) {
	workspaces, monitors := wsFixtures()
	cfg := config.DefaultsHyprlandWorkspaces()
	model := workspaceModel(workspaces, monitors, "DP-1", cfg)

	if len(model) != 4 {
		t.Fatalf("model = %+v, want 4 entries (3 numbered + special)", model)
	}
	// Ascending id order, matching the Rust sort_by_key: specials
	// (negative) genuinely come first.
	if model[0].id != -99 || model[1].id != 1 || model[2].id != 2 || model[3].id != 9 {
		t.Errorf("ids = %v %v %v %v, want ascending", model[0].id, model[1].id, model[2].id, model[3].id)
	}
	if !model[1].active {
		t.Error("workspace 1 is the focused monitor's active workspace; active = false")
	}
	if model[2].active {
		t.Error("workspace 2 is not active")
	}
	if !model[1].occupied || !model[2].occupied {
		t.Error("workspaces 1 and 2 have windows; occupied = false")
	}
	if model[3].occupied {
		t.Error("workspace 9 is empty")
	}
}

func TestWorkspaceModelOtherMonitorIsExcluded(t *testing.T) {
	workspaces, monitors := wsFixtures()
	cfg := config.DefaultsHyprlandWorkspaces()
	model := workspaceModel(workspaces, monitors, "HDMI-A-2", cfg)
	if len(model) != 1 || model[0].id != 5 {
		t.Fatalf("HDMI-A-2 model = %+v, want only workspace 5", model)
	}
	if model[0].active {
		t.Error("HDMI-A-2 is not focused; its workspace shows active = false")
	}
}

func TestWorkspaceModelSpecialCanBeHidden(t *testing.T) {
	workspaces, monitors := wsFixtures()
	cfg := config.DefaultsHyprlandWorkspaces()
	cfg.ShowSpecial = false
	model := workspaceModel(workspaces, monitors, "DP-1", cfg)
	if len(model) != 3 {
		t.Fatalf("model = %+v, want 3 numbered workspaces", model)
	}
	for _, ws := range model {
		if ws.id < 0 {
			t.Fatalf("special workspace %d shown with show-special = false", ws.id)
		}
	}
}

func TestWorkspaceModelMinWorkspacePads(t *testing.T) {
	workspaces, monitors := wsFixtures()
	cfg := config.DefaultsHyprlandWorkspaces()
	cfg.MinWorkspace = 6
	model := workspaceModel(workspaces, monitors, "HDMI-A-2", cfg)
	if len(model) != 6 {
		t.Fatalf("model = %+v, want 6 buttons", model)
	}
	for i, ws := range model {
		if ws.id != i+1 {
			t.Errorf("padded button %d has id %d, want %d", i, ws.id, i+1)
		}
		if ws.id != 5 && (ws.occupied || ws.active) {
			t.Errorf("padded button %d is not empty", ws.id)
		}
	}
	// Workspace 5 is real (it has windows) and stays occupied.
	if !model[4].occupied {
		t.Error("real workspace 5 lost its occupied flag")
	}
}

func TestWsLabel(t *testing.T) {
	cfg := config.DefaultsHyprlandWorkspaces()
	if got := wsLabel(wsState{id: 7, name: "7"}, cfg); got != "7" {
		t.Errorf("label = %q, want the number", got)
	}
	if got := wsLabel(wsState{id: -99, name: "special:magic"}, cfg); got != "special:magic" {
		t.Errorf("special label = %q, want the name", got)
	}
	cfg.LabelUseName = true
	if got := wsLabel(wsState{id: 4, name: "web"}, cfg); got != "web" {
		t.Errorf("label-use-name label = %q, want the name", got)
	}
}

func TestWsColorByState(t *testing.T) {
	palette := styling.Default()
	cfg := config.DefaultsHyprlandWorkspaces()
	active := wsColor(wsState{active: true}, cfg, palette)
	occupied := wsColor(wsState{occupied: true}, cfg, palette)
	empty := wsColor(wsState{}, cfg, palette)
	if active == occupied || occupied == empty || active == empty {
		t.Errorf("state colors collide: active %#08x occupied %#08x empty %#08x", active, occupied, empty)
	}
	if active != palette.Primary {
		t.Errorf("active = %#08x, want the accent token", active)
	}
}

func TestWsColorMapWins(t *testing.T) {
	cfg := config.DefaultsHyprlandWorkspaces()
	cv, err := config.ParseColorValue("#ff00ff")
	if err != nil {
		t.Fatal(err)
	}
	cfg.WorkspaceMap[3] = config.WorkspaceStyle{Color: cv, ColorSet: true}
	palette := styling.Default()
	got := wsColor(wsState{id: 3, active: true}, cfg, palette)
	if got>>16&0xFF != 0xFF || got&0xFF != 0xFF {
		t.Errorf("map color = %#08x, want the magenta channels", got)
	}
	other := wsColor(wsState{id: 4, active: true}, cfg, palette)
	if other == got {
		t.Errorf("unmapped workspace picked up the map color: %#08x", other)
	}
	_ = render.RGB(0, 0, 0)
}

func TestLoadFileAppliesHyprlandWorkspaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[modules.hyprland-workspaces]
display-mode = "label"
active-color = "#89b4fa"
occupied-color = "fg-muted"
numbering = "relative"
min-workspace-count = 5
label-use-name = true
show-special = false
divider = " | "

[modules.hyprland-workspaces.workspace-map.1]
color = "#f38ba8"

[modules.hyprland-workspaces.workspace-map.10]
color = "#81c8be"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	hw := c.HyprlandWorkspaces
	if hw.ActiveColor.Hex != "#89b4fa" {
		t.Errorf("active-color = %+v", hw.ActiveColor)
	}
	if hw.OccupiedColor.Token != config.TokenFgMuted {
		t.Errorf("occupied-color = %+v", hw.OccupiedColor)
	}
	if hw.Numbering != config.NumberingRelative || hw.MinWorkspace != 5 {
		t.Errorf("numbering/min = %q/%d", hw.Numbering, hw.MinWorkspace)
	}
	if !hw.LabelUseName || hw.ShowSpecial {
		t.Errorf("label-use-name/show-special = %v/%v", hw.LabelUseName, hw.ShowSpecial)
	}
	if hw.Divider != " | " {
		t.Errorf("divider = %q", hw.Divider)
	}
	if len(hw.WorkspaceMap) != 2 {
		t.Fatalf("workspace-map = %+v, want 2 entries", hw.WorkspaceMap)
	}
	if ws := hw.WorkspaceMap[1]; !ws.ColorSet || ws.Color.Hex != "#f38ba8" {
		t.Errorf("map[1] = %+v", ws)
	}
}

func TestLoadFileRejectsBadHyprlandWorkspaces(t *testing.T) {
	for _, content := range []string{
		"[modules.hyprland-workspaces]\ndisplay-mode = \"icon\"\n",
		"[modules.hyprland-workspaces]\nactive-indicator = \"glow\"\n",
		"[modules.hyprland-workspaces]\nnumbering = \"roman\"\n",
		"[modules.hyprland-workspaces]\nmin-workspace-count = 300\n",
		"[modules.hyprland-workspaces]\nactive-color = \"not-a-token\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}

func TestNewHyprlandWorkspacesRequiresHyprland(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = nil
	if _, err := Create("hyprland-workspaces", ctx); err == nil {
		t.Fatal("no Hyprland connection: want an error, got a module")
	}
}

// fakeWSHyprland is the hyprland package's fake protocol rebuilt small:
// canned replies per request, EOT-terminated.
func startFakeWSHyprland(t *testing.T, workspacesJSON, monitorsJSON string) (*hyprland.Connection, func()) {
	t.Helper()
	dir := t.TempDir()
	commandPath := filepath.Join(dir, ".socket.sock")
	ln, err := net.Listen("unix", commandPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				request := string(buf[:n])
				var reply string
				switch request {
				case "j/workspaces":
					reply = workspacesJSON
				case "j/monitors":
					reply = monitorsJSON
				default:
					reply = "ok"
				}
				c.Write([]byte(reply))
				c.Write([]byte{'\x04'})
			}(conn)
		}
	}()
	cleanup := func() { ln.Close() }
	return hyprland.ConnectTo(commandPath, filepath.Join(dir, ".socket2.sock")), cleanup
}

func TestHyprlandWorkspacesRefreshBuildsRow(t *testing.T) {
	workspaces := `[{"id":1,"name":"1","monitor":"DP-1","monitorID":0,"windows":2},{"id":2,"name":"2","monitor":"DP-1","monitorID":0,"windows":0}]`
	monitors := `[{"id":0,"name":"DP-1","activeWorkspace":{"id":1,"name":"1"},"focused":true}]`
	conn, cleanup := startFakeWSHyprland(t, workspaces, monitors)
	defer cleanup()

	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Hyprland = conn
	ctx.Connector = "DP-1"

	module, err := Create("hyprland-workspaces", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	row, ok := module.Root().(*widget.Box)
	if !ok {
		t.Fatalf("Root = %T, want the row box", module.Root())
	}
	children := row.Children()
	if len(children) != 1 {
		t.Fatalf("row children = %d, want the rebuilt section row", len(children))
	}
	inner, ok := children[0].(*widget.Box)
	if !ok {
		t.Fatalf("section = %T, want a box", children[0])
	}
	if got := len(inner.Children()); got != 2 {
		t.Errorf("buttons = %d, want one per workspace", got)
	}
}
