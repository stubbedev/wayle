package sway

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeSway speaks the i3-ipc protocol: canned GET_WORKSPACES and
// GET_TREE replies, recorded RUN_COMMANDs, and a subscription that
// races one event ahead of its ack and sends another after it.
type fakeSway struct {
	ln         net.Listener
	workspaces []Workspace
	tree       string
	inputs     string
	failWith   string

	mu        sync.Mutex
	commands  []string
	subscribe string
}

func newFakeSway(t *testing.T, workspaces []Workspace, tree string) *fakeSway {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sway-ipc.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSway{ln: ln, workspaces: workspaces, tree: tree}
	t.Cleanup(func() { _ = ln.Close() })
	go f.accept()
	t.Setenv("SWAYSOCK", path)
	return f
}

func (f *fakeSway) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.serve(conn)
	}
}

func (f *fakeSway) serve(conn net.Conn) {
	defer conn.Close()
	for {
		head := make([]byte, 14)
		if err := readFull(conn, head); err != nil {
			return
		}
		length := uint32(head[6]) | uint32(head[7])<<8 | uint32(head[8])<<16 | uint32(head[9])<<24
		msgType := uint32(head[10]) | uint32(head[11])<<8 | uint32(head[12])<<16 | uint32(head[13])<<24
		payload := make([]byte, length)
		if err := readFull(conn, payload); err != nil {
			return
		}
		switch msgType {
		case msgGetWorkspaces:
			body, _ := json.Marshal(f.workspaces)
			_ = writeFrame(conn, msgType, body)
		case msgGetTree:
			_ = writeFrame(conn, msgType, []byte(f.tree))
		case msgGetInputs:
			_ = writeFrame(conn, msgType, []byte(f.inputs))
		case msgRunCommand:
			f.mu.Lock()
			f.commands = append(f.commands, string(payload))
			f.mu.Unlock()
			if f.failWith != "" {
				_ = writeFrame(conn, msgType, []byte(`[{"success": false, "error": "`+f.failWith+`"}]`))
				continue
			}
			_ = writeFrame(conn, msgType, []byte(`[{"success": true}]`))
		case msgSubscribe:
			f.mu.Lock()
			f.subscribe = string(payload)
			f.mu.Unlock()
			// An event racing ahead of the ack must be skipped.
			_ = writeFrame(conn, eventBit, []byte(`{"change":"init"}`))
			_ = writeFrame(conn, msgType, []byte(`{"success": true}`))
			time.Sleep(20 * time.Millisecond)
			_ = writeFrame(conn, eventBit|3, []byte(`{"change":"new"}`))
		}
	}
}

const fixtureTree = `{"id":1,"type":"root","nodes":[
 {"id":2,"type":"output","name":"DP-1","nodes":[
  {"id":10,"type":"workspace","name":"1","nodes":[
    {"id":11,"type":"con","nodes":[
      {"id":12,"type":"con","name":"vim","app_id":"foot","focused":true,"nodes":[],"floating_nodes":[]},
      {"id":13,"type":"con","name":"Steam","app_id":null,"window_properties":{"class":"steam"},"urgent":true,"nodes":[],"floating_nodes":[]}
    ],"floating_nodes":[]}
  ],"floating_nodes":[
    {"id":14,"type":"floating_con","name":null,"app_id":"pavucontrol","nodes":[],"floating_nodes":[]}
  ]},
  {"id":20,"type":"workspace","name":"2","nodes":[],"floating_nodes":[]}
 ]}
]}`

func TestSocketPath(t *testing.T) {
	_ = os.Unsetenv("SWAYSOCK")
	if SocketPath() != "" || IsRunning() {
		t.Fatal("unset SWAYSOCK reads running")
	}
	t.Setenv("SWAYSOCK", "/run/sway.sock")
	if got := SocketPath(); got != "/run/sway.sock" || !IsRunning() {
		t.Errorf("path = %q", got)
	}
}

func TestWorkspacesRoundTrip(t *testing.T) {
	newFakeSway(t, []Workspace{
		{ID: 1, Num: 1, Name: "1", Visible: true, Output: "DP-1"},
		{ID: 2, Num: 2, Name: "2:web", Focused: true, Output: "DP-1"},
	}, "{}")
	conn, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	ws, err := conn.Workspaces()
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(ws) != 2 || ws[0].Name != "1" || !ws[1].Focused {
		t.Fatalf("workspaces = %+v", ws)
	}
}

func TestWindowsWalksTheTree(t *testing.T) {
	newFakeSway(t, nil, fixtureTree)
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	windows, err := conn.Windows()
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	if len(windows) != 3 {
		t.Fatalf("windows = %+v, want the three leaves (split containers and workspaces are not windows)", windows)
	}
	vim, steam, pav := windows[0], windows[1], windows[2]
	if vim.ID != 12 || vim.AppID != "foot" || vim.Title != "vim" || !vim.Focused || vim.WorkspaceID != 10 || !vim.HasWorkspace {
		t.Errorf("vim = %+v", vim)
	}
	if steam.AppID != "steam" || !steam.HasAppID || !steam.Urgent {
		t.Errorf("steam = %+v, want the X11 class as app id", steam)
	}
	if !pav.Floating || pav.WorkspaceID != 10 || pav.HasTitle {
		t.Errorf("pavucontrol = %+v, want a floating window on workspace 10 without a title", pav)
	}
}

func TestFocusCommands(t *testing.T) {
	f := newFakeSway(t, nil, "{}")
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, run := range []func() error{
		func() error { return conn.FocusWorkspace(ctx, Workspace{Num: 3, Name: "3"}) },
		func() error { return conn.FocusWorkspace(ctx, Workspace{Num: -1, Name: `my "ws"`}) },
		func() error { return conn.FocusNextOnOutput(ctx) },
		func() error { return conn.FocusPrevOnOutput(ctx) },
		func() error { return conn.FocusBackAndForth(ctx) },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"workspace --no-auto-back-and-forth number 3",
		`workspace --no-auto-back-and-forth "my \"ws\""`,
		"workspace next_on_output",
		"workspace prev_on_output",
		"workspace back_and_forth",
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != len(want) {
		t.Fatalf("commands = %q", f.commands)
	}
	for i := range want {
		if f.commands[i] != want[i] {
			t.Errorf("command %d = %q, want %q", i, f.commands[i], want[i])
		}
	}
}

func TestRunCommandFailureSurfaces(t *testing.T) {
	f := newFakeSway(t, nil, "{}")
	f.failWith = "no such workspace"
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.FocusNextOnOutput(context.Background()); err == nil || err.Error() != "sway: no such workspace" {
		t.Fatalf("err = %v, want sway's error", err)
	}
}

func TestQuoteEscapes(t *testing.T) {
	if got := Quote(`a\b"c`); got != `"a\\b\"c"` {
		t.Errorf("Quote = %s", got)
	}
}

func TestSubscribeSkipsEarlyEventAndTicks(t *testing.T) {
	f := newFakeSway(t, nil, "{}")
	ticks, stop, err := Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	f.mu.Lock()
	sub := f.subscribe
	f.mu.Unlock()
	if sub != `["workspace","window"]` {
		t.Errorf("subscribed to %s, want workspace and window", sub)
	}
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("no tick for the event after the ack")
	}
	stop()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ticks:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("ticks not closed after stop")
		}
	}
}

func TestKeyboardLayoutIsTheFirstKeyboardWithALayout(t *testing.T) {
	f := newFakeSway(t, nil, "{}")
	// A pointer never counts, nor a keyboard without xkb state; the
	// first keyboard that reports a layout wins over later ones.
	f.inputs = `[
	 {"identifier":"1:1:mouse","type":"pointer","xkb_active_layout_name":"ignored"},
	 {"identifier":"0:0:power","type":"keyboard"},
	 {"identifier":"1:1:at","type":"keyboard","xkb_active_layout_name":"German"},
	 {"identifier":"2:2:usb","type":"keyboard","xkb_active_layout_name":"English (US)"}
	]`
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	inputs, err := conn.Inputs()
	if err != nil {
		t.Fatalf("Inputs: %v", err)
	}
	if got := KeyboardLayout(inputs); got != "German" {
		t.Errorf("layout = %q, want German", got)
	}
	// No keyboard with a layout: none at all.
	if got := KeyboardLayout(inputs[:2]); got != "" {
		t.Errorf("layout = %q, want empty", got)
	}
}

func TestSubscribeToNamesItsStreams(t *testing.T) {
	f := newFakeSway(t, nil, "{}")
	_, stop, err := SubscribeTo("input")
	if err != nil {
		t.Fatalf("SubscribeTo: %v", err)
	}
	defer stop()
	f.mu.Lock()
	sub := f.subscribe
	f.mu.Unlock()
	if sub != `["input"]` {
		t.Errorf("subscribed to %s, want input only", sub)
	}
}
