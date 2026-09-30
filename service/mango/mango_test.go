package mango

import (
	"bufio"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// monitorsFrame is types.rs's ALL_MONITORS_FRAME fixture.
const monitorsFrame = `{"monitors":[{"name":"eDP-1","active":true,"x":0,"y":0,"width":1920,"height":1080,"scale":1,"layout_index":0,"layout_symbol":"T","last_open_surface":"foot","tags":[{"index":1,"is_active":true,"is_urgent":false,"layout":"T","client_count":2},{"index":2,"is_active":false,"is_urgent":true,"layout":"DW","client_count":0}],"active_tags":[1],"active_client":{"id":42,"title":"nvim","appid":"foot"},"keymode":"default","keyboardlayout":"English (US)"}]}`

const clientsFrame = `{"clients":[{"id":42,"title":"nvim","appid":"foot","monitor":"eDP-1","tags":[1,3],"is_urgent":false,"is_focused":true},{"id":7,"title":"","appid":"","monitor":"eDP-1","tags":[2],"is_urgent":true,"is_focused":false}]}`

// fakeMango answers dispatches (recorded; "dispatch bad" is rejected)
// and streams one frame per watch subscription.
type fakeMango struct {
	mu       sync.Mutex
	commands []string
}

func newFakeMango(t *testing.T) *fakeMango {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mango.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv(SocketEnv, path)
	f := &fakeMango{}
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

func (f *fakeMango) serve(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return
	}
	line = line[:len(line)-1]
	switch line {
	case watchMonitors:
		_, _ = conn.Write([]byte("not json\n" + monitorsFrame + "\n"))
		time.Sleep(time.Second)
	case watchClients:
		_, _ = conn.Write([]byte(clientsFrame + "\n"))
		time.Sleep(time.Second)
	default:
		f.mu.Lock()
		f.commands = append(f.commands, line)
		f.mu.Unlock()
		if line == "dispatch bad" {
			_, _ = conn.Write([]byte(`{"error":"unknown dispatcher"}` + "\n"))
			return
		}
		_, _ = conn.Write([]byte(`{"ok":true}` + "\n"))
	}
}

func TestNotRunning(t *testing.T) {
	t.Setenv(SocketEnv, "")
	if IsRunning() {
		t.Fatal("empty env reads running")
	}
	if err := ViewLeft(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Watch(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Watch err = %v", err)
	}
}

func TestDispatchCommands(t *testing.T) {
	f := newFakeMango(t)
	for _, run := range []func() error{func() error { return ViewTag(3) }, ViewLeft, ViewRight} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"dispatch view,3", "dispatch viewtoleft", "dispatch viewtoright"}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 3 {
		t.Fatalf("commands = %q", f.commands)
	}
	for i := range want {
		if f.commands[i] != want[i] {
			t.Errorf("command %d = %q, want %q", i, f.commands[i], want[i])
		}
	}
}

func TestDispatchRejected(t *testing.T) {
	newFakeMango(t)
	var rejected *RejectedError
	if err := Dispatch("bad"); !errors.As(err, &rejected) || rejected.Message != "unknown dispatcher" {
		t.Fatalf("err = %v, want mango's rejection", err)
	}
}

func TestWatchAppliesFramesAndSkipsGarbage(t *testing.T) {
	newFakeMango(t)
	w, err := Watch()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.Ready() {
		t.Fatal("ready before any frame")
	}
	deadline := time.After(2 * time.Second)
	for {
		if w.Ready() {
			monitors, clients := w.State()
			if len(monitors) != 1 || len(clients) != 2 {
				t.Fatalf("ready with %d monitors / %d clients", len(monitors), len(clients))
			}
			m := monitors[0]
			if m.Name != "eDP-1" || !m.IsActive || len(m.Tags) != 2 || !m.Tags[1].IsUrgent || m.Tags[0].ClientCount != 2 {
				t.Fatalf("monitor = %+v", m)
			}
			if !clients[0].HasAppID || clients[0].AppID != "foot" || len(clients[0].Tags) != 2 {
				t.Fatalf("client 42 = %+v", clients[0])
			}
			if clients[1].HasTitle || clients[1].HasAppID {
				t.Fatalf("client 7 = %+v, want empty strings read as unset", clients[1])
			}
			return
		}
		select {
		case <-w.Ticks():
		case <-deadline:
			t.Fatal("frames never applied")
		}
	}
}
