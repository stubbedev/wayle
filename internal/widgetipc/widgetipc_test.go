package widgetipc

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSocketPathLookup(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := SocketPath(); got != "/run/user/1000/wayle/widget.sock" {
		t.Errorf("runtime dir = %q", got)
	}
	os.Unsetenv("XDG_RUNTIME_DIR")
	if got := SocketPath(); got != "/tmp/wayle-widget.sock" {
		t.Errorf("fallback = %q", got)
	}
}

func TestServerRoundTrip(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s := NewServer()
	stop, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer stop()

	pct := 42.5
	dur := uint32(1200)
	cls := "mine"
	if err := SendToast(context.Background(), ToastRequest{Label: strPtr("hello"), Percentage: &pct, DurationMS: &dur, Class: &cls}); err != nil {
		t.Fatalf("SendToast: %v", err)
	}
	select {
	case got := <-s.Toasts():
		if got.Label == nil || *got.Label != "hello" {
			t.Fatalf("label = %v", got.Label)
		}
		if got.Percentage == nil || *got.Percentage != 42.5 {
			t.Fatalf("percentage = %v", got.Percentage)
		}
		if got.DurationMS == nil || *got.DurationMS != 1200 {
			t.Fatalf("duration = %v", got.DurationMS)
		}
		if got.Class == nil || *got.Class != "mine" {
			t.Fatalf("class = %v", got.Class)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no toast request")
	}

	// A toast with neither label nor preset is a caller-side error at
	// the CLI level; the socket itself forwards it.
	if err := SendToast(context.Background(), ToastRequest{}); err != nil {
		t.Fatalf("socket forwarded reject: %v", err)
	}
	<-s.Toasts()

	// Unknown methods are JSON-RPC errors.
	conn, err := Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, _ := json.Marshal(request{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "nope"})
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(conn).ReadString('\n')
	var resp response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("reply = %q", line)
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v", resp.Error)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	// A dead socket file at the path.
	path := filepath.Join(dir, "wayle", "widget.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	stop, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen over stale socket: %v", err)
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket file left behind: %v", err)
	}
}

func strPtr(s string) *string { return &s }
