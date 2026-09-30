package bar

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
)

// serveUnix accepts on a fresh socket and hands each connection to fn.
func serveUnix(t *testing.T, name string, fn func(net.Conn)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
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
			go func() {
				defer conn.Close()
				fn(conn)
			}()
		}
	}()
	return path
}

// fakeI3 answers GET_WORKSPACES (1) and GET_TREE (4) with canned JSON.
func fakeI3(t *testing.T, workspaces, tree string) {
	t.Helper()
	path := serveUnix(t, "sway.sock", func(conn net.Conn) {
		for {
			head := make([]byte, 14)
			if _, err := io.ReadFull(conn, head); err != nil {
				return
			}
			payload := make([]byte, binary.LittleEndian.Uint32(head[6:]))
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			msgType := binary.LittleEndian.Uint32(head[10:])
			body := "[]"
			switch msgType {
			case 1:
				body = workspaces
			case 4:
				body = tree
			}
			out := make([]byte, 14, 14+len(body))
			copy(out, "i3-ipc")
			binary.LittleEndian.PutUint32(out[6:], uint32(len(body)))
			binary.LittleEndian.PutUint32(out[10:], msgType)
			_, _ = conn.Write(append(out, body...))
		}
	})
	t.Setenv("SWAYSOCK", path)
}

func TestSwayBackendMapsTheSnapshot(t *testing.T) {
	fakeI3(t,
		`[{"id":10,"num":1,"name":"1","visible":true,"focused":true,"urgent":false,"output":"DP-1"},
		  {"id":20,"num":2,"name":"2","visible":false,"focused":false,"urgent":true,"output":"DP-1"},
		  {"id":30,"num":-1,"name":"chat","visible":true,"focused":false,"urgent":false,"output":"DP-1"}]`,
		`{"id":1,"type":"root","nodes":[{"id":2,"type":"output","nodes":[
		  {"id":10,"type":"workspace","nodes":[{"id":11,"type":"con","app_id":"foot","name":"t","nodes":[],"floating_nodes":[]}],"floating_nodes":[]},
		  {"id":20,"type":"workspace","nodes":[],"floating_nodes":[]},
		  {"id":30,"type":"workspace","nodes":[],"floating_nodes":[]}]}]}`)
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Connector = "DP-1"
	module, err := Create("sway-workspaces", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*cwsModule)
	defer m.Stop()
	if len(m.models) != 3 {
		t.Fatalf("models = %d, want 3", len(m.models))
	}
	one, two, chat := m.models[0], m.models[1], m.models[2]
	if !one.ws.active || !one.ws.hasWindows || !one.ws.focused {
		t.Errorf("ws 1 = %+v, want visible/occupied/focused", one.ws)
	}
	if two.ws.hasWindows || !two.ws.urgent {
		t.Errorf("ws 2 = %+v, want empty and urgent", two.ws)
	}
	// Unnumbered sorts last; name-or-index labels it by name.
	if chat.ws.id != 30 || chat.label != "chat" {
		t.Errorf("last = %+v, want the unnumbered chat workspace", chat)
	}
	// Occupancy comes from the tree, not sway's visible flag.
	if !chat.ws.active || chat.ws.hasWindows {
		t.Errorf("chat = %+v, want visible (active) but empty", chat.ws)
	}
	if m.root.buttons()[0].model.label != "1" {
		t.Errorf("label = %q", m.root.buttons()[0].model.label)
	}
}

// fakeNiriSocket answers Workspaces/Windows lines with canned JSON.
func fakeNiriSocket(t *testing.T, workspaces, windows string) {
	t.Helper()
	path := serveUnix(t, "niri.sock", func(conn net.Conn) {
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch line {
			case "\"Workspaces\"\n":
				_, _ = conn.Write([]byte(`{"Ok":{"Workspaces":` + workspaces + "}}\n"))
			case "\"Windows\"\n":
				_, _ = conn.Write([]byte(`{"Ok":{"Windows":` + windows + "}}\n"))
			default:
				_, _ = conn.Write([]byte("{\"Ok\":\"Handled\"}\n"))
			}
		}
	})
	t.Setenv("NIRI_SOCKET", path)
}

func TestNiriBackendMapsTheSnapshot(t *testing.T) {
	fakeNiriSocket(t,
		`[{"id":7,"idx":1,"name":null,"output":"DP-1","is_urgent":false,"is_active":true,"is_focused":true,"active_window_id":1},`+
			`{"id":8,"idx":2,"name":"web","output":"DP-1","is_urgent":false,"is_active":false,"is_focused":false,"active_window_id":null},`+
			`{"id":9,"idx":3,"name":null,"output":"DP-1","is_urgent":false,"is_active":false,"is_focused":false,"active_window_id":null}]`,
		`[{"id":1,"title":"a","app_id":"foot","workspace_id":7,"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{"pos_in_scrolling_layout":[2,1]}},`+
			`{"id":2,"title":"b","app_id":"firefox","workspace_id":7,"is_focused":false,"is_floating":true,"is_urgent":false,"layout":{"pos_in_scrolling_layout":null}},`+
			`{"id":3,"title":"c","app_id":"kitty","workspace_id":7,"is_focused":false,"is_floating":false,"is_urgent":false,"layout":{"pos_in_scrolling_layout":[1,1]}}]`)
	cfg := config.Defaults()
	cfg.NiriWorkspaces.AppIconsShow = true
	cfg.NiriWorkspaces.AppIconsDedupe = false
	ctx := newTestContext(t, cfg)
	ctx.Connector = "DP-1"
	module, err := Create("niri-workspaces", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*cwsModule)
	defer m.Stop()
	// idx 3 is the empty tail niri always keeps; the default hides it.
	if len(m.models) != 2 {
		t.Fatalf("models = %+v, want the trailing empty hidden", m.models)
	}
	first, web := m.models[0], m.models[1]
	if first.label != "1" || !first.ws.hasWindows || web.label != "web" || web.ws.hasWindows {
		t.Errorf("models = %+v / %+v", first, web)
	}
	// Scrolling order: column 1 (kitty), column 2 (foot), floating last.
	var ids []uint64
	for _, icon := range first.appIcons {
		ids = append(ids, icon.windowIDs[0])
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Errorf("app icon order = %v, want [3 1 2]", ids)
	}
}
