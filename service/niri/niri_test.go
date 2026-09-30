package niri

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeNiri answers the JSON-lines protocol: canned workspace/window
// replies, Handled for actions (recorded), and an event stream that
// emits one event line after the ack.
type fakeNiri struct {
	ln         net.Listener
	workspaces string
	windows    string
	rejectWith string

	mu      sync.Mutex
	actions []string
}

func newFakeNiri(t *testing.T, workspaces, windows string) *fakeNiri {
	t.Helper()
	path := filepath.Join(t.TempDir(), "niri.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeNiri{ln: ln, workspaces: compact(t, workspaces), windows: compact(t, windows)}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv(SocketEnv, path)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

// compact folds a fixture onto one line: the protocol is one JSON
// value per line.
func compact(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func (f *fakeNiri) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req any
		if err := json.Unmarshal(line, &req); err != nil {
			return
		}
		switch r := req.(type) {
		case string:
			switch r {
			case "Workspaces":
				_, _ = conn.Write([]byte(`{"Ok":{"Workspaces":` + f.workspaces + "}}\n"))
			case "Windows":
				_, _ = conn.Write([]byte(`{"Ok":{"Windows":` + f.windows + "}}\n"))
			case "EventStream":
				_, _ = conn.Write([]byte("{\"Ok\":\"Handled\"}\n"))
				time.Sleep(20 * time.Millisecond)
				_, _ = conn.Write([]byte("{\"WorkspaceActivated\":{\"id\":2,\"focused\":true}}\n"))
			}
		case map[string]any:
			f.mu.Lock()
			f.actions = append(f.actions, string(line[:len(line)-1]))
			f.mu.Unlock()
			if f.rejectWith != "" {
				_, _ = conn.Write([]byte(`{"Err":"` + f.rejectWith + "\"}\n"))
				continue
			}
			_, _ = conn.Write([]byte("{\"Ok\":\"Handled\"}\n"))
		}
	}
}

func (f *fakeNiri) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.actions...)
}

const fixtureWorkspaces = `[
 {"id":1,"idx":1,"name":null,"output":"DP-1","is_urgent":false,"is_active":true,"is_focused":true,"active_window_id":7},
 {"id":2,"idx":2,"name":"web","output":"DP-1","is_urgent":true,"is_active":false,"is_focused":false,"active_window_id":null}
]`

const fixtureWindows = `[
 {"id":7,"title":"term","app_id":"foot","pid":42,"workspace_id":1,"is_focused":true,"is_floating":false,"is_urgent":false,
  "layout":{"pos_in_scrolling_layout":[2,1],"tile_size":[1,1],"window_size":[1,1],"tile_pos_in_workspace_view":null,"window_offset_in_tile":[0,0]},
  "focus_timestamp":null},
 {"id":8,"title":null,"app_id":null,"pid":null,"workspace_id":null,"is_focused":false,"is_floating":true,"is_urgent":true,
  "layout":{"pos_in_scrolling_layout":null}}
]`

func TestSocketPathAndNotRunning(t *testing.T) {
	t.Setenv(SocketEnv, "")
	if IsRunning() {
		t.Fatal("empty NIRI_SOCKET reads as running")
	}
	if _, err := Connect(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Connect err = %v, want ErrNotRunning", err)
	}
	if _, _, err := Subscribe(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Subscribe err = %v, want ErrNotRunning", err)
	}
}

func TestWorkspacesAndWindowsDecode(t *testing.T) {
	newFakeNiri(t, fixtureWorkspaces, fixtureWindows)
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ws, err := conn.Workspaces()
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(ws) != 2 || ws[0].Name != nil || *ws[1].Name != "web" || *ws[0].ActiveWindowID != 7 ||
		ws[1].ActiveWindowID != nil || !ws[1].IsUrgent || *ws[0].Output != "DP-1" {
		t.Fatalf("workspaces = %+v", ws)
	}
	windows, err := conn.Windows()
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	if len(windows) != 2 || *windows[0].AppID != "foot" || *windows[0].WorkspaceID != 1 ||
		*windows[0].Layout.PosInScrollingLayout != [2]int{2, 1} {
		t.Fatalf("window 7 = %+v", windows[0])
	}
	if windows[1].AppID != nil || windows[1].WorkspaceID != nil || windows[1].Layout.PosInScrollingLayout != nil {
		t.Fatalf("window 8 nulls = %+v", windows[1])
	}
}

func TestActionsSerializeAsNiriIPC(t *testing.T) {
	f := newFakeNiri(t, "[]", "[]")
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, run := range []func() error{
		func() error { return conn.FocusWorkspaceID(ctx, 5) },
		func() error { return conn.FocusWorkspaceDown(ctx) },
		func() error { return conn.FocusWorkspaceUp(ctx) },
		func() error { return conn.FocusWorkspacePrevious(ctx) },
	} {
		if err := run(); err != nil {
			t.Fatalf("action: %v", err)
		}
	}
	want := []string{
		`{"Action":{"FocusWorkspace":{"reference":{"Id":5}}}}`,
		`{"Action":{"FocusWorkspaceDown":{}}}`,
		`{"Action":{"FocusWorkspaceUp":{}}}`,
		`{"Action":{"FocusWorkspacePrevious":{}}}`,
	}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("actions = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("action %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestActionRejectedSurfacesTheMessage(t *testing.T) {
	f := newFakeNiri(t, "[]", "[]")
	f.rejectWith = "no such workspace"
	conn, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	err = conn.FocusWorkspaceID(context.Background(), 99)
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Message != "no such workspace" {
		t.Fatalf("err = %v, want a RejectedError with niri's message", err)
	}
}

func TestSubscribeTicksAndStopCloses(t *testing.T) {
	newFakeNiri(t, "[]", "[]")
	ticks, stop, err := Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("no tick for the event line")
	}
	stop()
	select {
	case _, ok := <-ticks:
		if ok {
			// A buffered tick may drain first; the close must follow.
			if _, ok := <-ticks; ok {
				t.Fatal("ticks still open after stop")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ticks not closed after stop")
	}
}

func TestDecodeReplyRejectsShapeless(t *testing.T) {
	if _, err := decodeReply([]byte(`{}`)); err == nil {
		t.Error("empty reply decoded")
	}
	if _, err := decodeReply([]byte(`not json`)); err == nil {
		t.Error("garbage decoded")
	}
	if err := expectHandled(json.RawMessage(`{"Version":"25.11"}`), "action"); err == nil {
		t.Error("non-Handled response accepted")
	}
}
