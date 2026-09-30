package widgetipc

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func listen(t *testing.T) *Server {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s := NewServer()
	stop, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(stop)
	return s
}

func TestToastRoundTrip(t *testing.T) {
	s := listen(t)
	pct := 42.5
	dur := uint32(1200)
	cls := "mine"
	if err := SendToast(context.Background(), ToastRequest{Label: new("hello"), Percentage: &pct, DurationMS: &dur, Class: &cls}); err != nil {
		t.Fatalf("SendToast: %v", err)
	}
	select {
	case got := <-s.Toasts():
		if *got.Label != "hello" || *got.Percentage != 42.5 || *got.DurationMS != 1200 || *got.Class != "mine" {
			t.Fatalf("toast = %+v", got)
		}
		if got.Icon != nil || got.Preset != nil {
			t.Fatalf("absent fields arrived: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no toast request")
	}
}

func TestWidgetUpdateRoundTrip(t *testing.T) {
	s := listen(t)
	if err := SendWidgetUpdate(context.Background(), "gpu", `{"text":"42"}`); err != nil {
		t.Fatalf("SendWidgetUpdate: %v", err)
	}
	select {
	case got := <-s.Updates():
		if got.ID != "gpu" || got.Output != `{"text":"42"}` {
			t.Fatalf("update = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no update")
	}
}

// exchange writes one raw line and returns the raw reply line.
func exchange(t *testing.T, line string) string {
	t.Helper()
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(reply)
}

func TestServerRepliesLikeRust(t *testing.T) {
	s := listen(t)
	cases := []struct{ in, want string }{
		{
			`{"jsonrpc":"2.0","method":"widget.update","params":{"id":"a","output":"b"},"id":3}`,
			`{"jsonrpc":"2.0","result":"ok","id":3}`,
		},
		{
			`{"jsonrpc":"2.0","method":"nope","params":null,"id":7}`,
			`{"jsonrpc":"2.0","error":{"code":-32601,"message":"unknown method: nope"},"id":7}`,
		},
		{
			`{"jsonrpc":"2.0","method":"widget.update","params":{"id":"a"},"id":4}`,
			"{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32602,\"message\":\"invalid params: missing field `output`\"},\"id\":4}",
		},
		{
			`{"jsonrpc":"2.0","method":"widget.update","params":{"id":"a","output":"b"}}`,
			"{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32700,\"message\":\"parse error: missing field `id`\"},\"id\":0}",
		},
	}
	for _, c := range cases {
		if got := exchange(t, c.in); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	<-s.Updates() // only the valid update was published
	select {
	case u := <-s.Updates():
		t.Errorf("a rejected request was published: %+v", u)
	default:
	}
}

func TestClientErrorsMatchRust(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	path := filepath.Join(dir, "wayle", "widget.sock")
	err := SendWidgetUpdate(context.Background(), "a", "b")
	want := "cannot connect to wayle widget socket at " + path + ": No such file or directory (os error 2)"
	if err == nil || err.Error() != want {
		t.Fatalf("no shell: %v\nwant %s", err, want)
	}

	// A shell that rejects the request surfaces its message.
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(SocketPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	fake, err := net.Listen("unix", SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	go func() {
		conn, err := fake.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"nope"},"id":1}` + "\n"))
	}()
	if err := SendToast(context.Background(), ToastRequest{Label: new("x")}); err == nil || err.Error() != "widget update rejected: nope" {
		t.Fatalf("rejected: %v", err)
	}
}

func TestListenReplacesStaleSocketAndRestrictsIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	path := filepath.Join(dir, "wayle", "widget.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	stop, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen over stale socket: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v, %v", info.Mode(), err)
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket file left behind: %v", err)
	}
}
