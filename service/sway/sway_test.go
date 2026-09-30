package sway

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSway speaks the i3-ipc protocol: one workspace reply for
// GET_WORKSPACES, event frames on demand.
type fakeSway struct {
	ln         net.Listener
	workspaces []Workspace
}

func newFakeSway(t *testing.T, workspaces []Workspace) *fakeSway {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sway-ipc.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSway{ln: ln, workspaces: workspaces}
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
		case msgRunCommand:
			_ = writeFrame(conn, msgType, []byte(`[{"success": true}]`))
		case msgSubscribe:
			_ = writeFrame(conn, msgType, []byte(`{"success": true}`))
			// One workspace event after the reply.
			time.Sleep(50 * time.Millisecond)
			_ = writeFrame(conn, 0x80000001, []byte(`{"change":"focus"}`))
		}
	}
}

func TestSocketPath(t *testing.T) {
	os.Unsetenv("SWAYSOCK")
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
	})
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
	if err := conn.SwitchTo(context.Background(), "3"); err != nil {
		t.Fatalf("SwitchTo: %v", err)
	}
}

func TestSubscribeTicks(t *testing.T) {
	newFakeSway(t, nil)
	ticks, stop, err := Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer stop()
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("no workspace event")
	}
}
