package treeman

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
)

func TestWorstBucket(t *testing.T) {
	for _, tc := range []struct {
		status Status
		want   Bucket
	}{
		{Status{Total: 1, Stable: 1}, BucketStable},
		{Status{Total: 2, Stable: 1, Up: 1}, BucketUp},
		{Status{Total: 3, Stable: 1, Up: 1, Down: 1}, BucketDown},
		{Status{Total: 4, Stable: 1, Up: 1, Down: 1, Failed: 1}, BucketFailed},
	} {
		if got := tc.status.WorstBucket(); got != tc.want {
			t.Errorf("%+v: worst = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestSocketPathLookup(t *testing.T) {
	t.Setenv("TREEMAN_SOCKET", "/custom/treeman.sock")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if p, ok := SocketPath(); !ok || p != "/custom/treeman.sock" {
		t.Errorf("env override = %q ok=%v", p, ok)
	}
	os.Unsetenv("TREEMAN_SOCKET")
	if p, ok := SocketPath(); !ok || p != "/run/user/1000/treeman.sock" {
		t.Errorf("runtime dir = %q ok=%v", p, ok)
	}
	os.Unsetenv("XDG_RUNTIME_DIR")
	t.Setenv("XDG_DATA_HOME", "/data")
	if p, _ := SocketPath(); p != "/data/treeman/treeman.sock" {
		t.Errorf("data home = %q", p)
	}
	os.Unsetenv("XDG_DATA_HOME")
	t.Setenv("HOME", "/home/u")
	if p, _ := SocketPath(); p != "/home/u/.local/share/treeman/treeman.sock" {
		t.Errorf("home fallback = %q", p)
	}
}

func TestReadParsesStatusJSON(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-treeman")
	script := "#!/bin/sh\ncat <<'JSON'\n{\"total\":3,\"stable\":1,\"up\":1,\"down\":0,\"failed\":1,\"class\":\"failed\"}\nJSON\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(bin)
	status, err := s.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status == nil || status.Total != 3 || status.Failed != 1 {
		t.Fatalf("status = %+v err=%v", status, err)
	}
	if got := status.WorstBucket(); got != BucketFailed {
		t.Errorf("worst = %v", got)
	}

	// A failing treeman reads as nil, not an error.
	s2 := New(filepath.Join(dir, "missing-treeman"))
	if status, err := s2.Read(context.Background()); err != nil || status != nil {
		t.Errorf("missing binary = %+v err=%v, want nil,nil", status, err)
	}
}

func TestSubscribeTicksOnEvents(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "treeman.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 256)
				if _, err := c.Read(buf); err != nil {
					return
				}
				// Hold the stream open, then deliver one event.
				time.Sleep(100 * time.Millisecond)
				c.Write([]byte("{\"event\":{}}\n"))
				time.Sleep(2 * time.Second)
			}(conn)
		}
	}()

	t.Setenv("TREEMAN_SOCKET", sock)
	s := New("true")
	ticks, stop, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer stop()

	// The initial read tick arrives immediately.
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("no initial tick")
	}
	// The daemon's event collapses into one debounced tick.
	select {
	case <-ticks:
	case <-time.After(3 * time.Second):
		t.Fatal("no event tick")
	}
}

// Every subscriber sees every change (one bar per output), and Stop
// returns and closes them all.
func TestEverySubscriberTicksAndStopClosesThem(t *testing.T) {
	t.Setenv("TREEMAN_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	s := New("true")
	a, _, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	feed.Notify(&s.changes)
	for name, ch := range map[string]<-chan struct{}{"first": a, "second": b} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("the %s subscriber missed the tick", name)
		}
	}
	done := make(chan struct{})
	go func() { s.Stop(); s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop deadlocked")
	}
	for _, ch := range []<-chan struct{}{a, b} {
		for range ch { // drains the pending tick, ends on close
		}
	}
}
