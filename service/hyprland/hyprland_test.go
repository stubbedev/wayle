package hyprland

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeHyprland is a scriptable IPC server: it answers command-socket
// requests from a table and can push event lines to the first event
// subscriber.
type fakeHyprland struct {
	commandPath string
	eventPath   string
	replies     map[string]string
	requests    chan string
	events      chan net.Conn
}

func startFake(t *testing.T, replies map[string]string) *fakeHyprland {
	t.Helper()
	dir := t.TempDir()
	f := &fakeHyprland{
		commandPath: filepath.Join(dir, ".socket.sock"),
		eventPath:   filepath.Join(dir, ".socket2.sock"),
		replies:     replies,
		requests:    make(chan string, 16),
		events:      make(chan net.Conn, 1),
	}
	commandLn, err := net.Listen("unix", f.commandPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := commandLn.Accept()
			if err != nil {
				return
			}
			go f.serveCommand(conn)
		}
	}()
	t.Cleanup(func() { commandLn.Close() })

	eventLn, err := net.Listen("unix", f.eventPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := eventLn.Accept()
			if err != nil {
				return
			}
			f.events <- conn
		}
	}()
	t.Cleanup(func() { eventLn.Close() })
	return f
}

func (f *fakeHyprland) serveCommand(conn net.Conn) {
	defer conn.Close()
	// One request per connection, small enough for a single read; the
	// client keeps its write side open while it waits for the reply.
	tmp := make([]byte, 4096)
	n, _ := conn.Read(tmp)
	request := string(tmp[:n])
	select {
	case f.requests <- request:
	default:
	}
	reply, ok := f.replies[request]
	if !ok {
		reply = "invalid request"
	}
	conn.Write([]byte(reply))
	conn.Write([]byte{endOfTransmission})
}

// PushEvents waits for the first event subscriber and writes lines to
// it, like the compositor pushing its event stream.
func (f *fakeHyprland) PushEvents(t *testing.T, lines ...string) {
	t.Helper()
	select {
	case conn := <-f.events:
		w := bufio.NewWriter(conn)
		for _, line := range lines {
			w.WriteString(line + "\n")
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event subscriber connected")
	}
}

func TestCommandRoundTrip(t *testing.T) {
	fake := startFake(t, map[string]string{
		"j/workspaces":         `[{"id":1,"name":"1","monitor":"DP-1","monitorID":0,"windows":2,"hasfullscreen":false,"ispersistent":false,"lastwindow":"0x1","lastwindowtitle":"term","tiled_layout":"dwindle"}]`,
		"j/monitors":           `[{"id":0,"name":"DP-1","activeWorkspace":{"id":2,"name":"2"},"focused":true}]`,
		"dispatch workspace 3": "ok",
	})
	conn := ConnectTo(fake.commandPath, fake.eventPath)

	workspaces, err := conn.Workspaces()
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].Name != "1" || workspaces[0].Windows != 2 {
		t.Fatalf("workspaces = %+v", workspaces)
	}
	if got := <-fake.requests; got != "j/workspaces" {
		t.Errorf("request = %q, want j/workspaces", got)
	}

	monitors, err := conn.Monitors()
	if err != nil {
		t.Fatalf("Monitors: %v", err)
	}
	if len(monitors) != 1 || !monitors[0].Focused || monitors[0].ActiveWS.ID != 2 {
		t.Fatalf("monitors = %+v", monitors)
	}
	if got := <-fake.requests; got != "j/monitors" {
		t.Errorf("monitors request = %q", got)
	}

	ack, err := conn.Dispatch("workspace 3")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if ack != "ok" {
		t.Errorf("ack = %q", ack)
	}
	if got := <-fake.requests; got != "dispatch workspace 3" {
		t.Errorf("dispatch request = %q", got)
	}
}

func TestCommandBadJSONIsAnError(t *testing.T) {
	fake := startFake(t, map[string]string{"j/workspaces": "{not json"})
	conn := ConnectTo(fake.commandPath, fake.eventPath)
	if _, err := conn.Workspaces(); err == nil {
		t.Fatal("malformed reply: want an error, got nil")
	}
}

func TestEvents(t *testing.T) {
	fake := startFake(t, nil)
	conn := ConnectTo(fake.commandPath, fake.eventPath)
	events, err := conn.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	fake.PushEvents(t,
		"workspace>>2",
		"workspacev2>>2,2",
		"focusedmon>>DP-1,2",
		"createworkspace>>4",
		"destroyworkspace>>9",
		"openwindow>>0x1,3,kitty,ed",
		"screencast>>1,0",
	)

	want := []Event{
		{Kind: EventWorkspace, Name: "2", ID: -1, Payload: "2"},
		{Kind: EventWorkspaceV2, ID: 2, Name: "2", Payload: "2,2"},
		{Kind: EventFocusedMon, Name: "DP-1", ID: 2, Payload: "DP-1,2"},
		{Kind: EventCreateWorkspc, Name: "4", ID: -1, Payload: "4"},
		{Kind: EventDestroyWorkspc, Name: "9", ID: -1, Payload: "9"},
		{Kind: EventOpenWindow, Name: "3", ID: -1, Payload: "0x1,3,kitty,ed"},
		{Kind: "screencast", ID: -1, Payload: "1,0"},
	}
	for _, expected := range want {
		select {
		case got, ok := <-events:
			if !ok {
				t.Fatal("event channel closed early")
			}
			if got.Kind != expected.Kind || got.Name != expected.Name || got.ID != expected.ID || got.Payload != expected.Payload {
				t.Fatalf("event %+v, want %+v", got, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %+v", expected)
		}
	}
}

func TestParseEventRejectsMissingSeparator(t *testing.T) {
	if _, ok := ParseEvent("garbage-line"); ok {
		t.Fatal("line without >>: want a failed parse")
	}
}

func TestPathsFromEnvironment(t *testing.T) {
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "sig_1")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	command, events, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/run/user/1000", "hypr", "sig_1", ".socket.sock"); command != want {
		t.Errorf("command path = %q, want %q", command, want)
	}
	if want := filepath.Join("/run/user/1000", "hypr", "sig_1", ".socket2.sock"); events != want {
		t.Errorf("event path = %q, want %q", events, want)
	}

	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if _, _, err := Paths(); err == nil {
		t.Error("missing signature: want an error, got nil")
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "sig_1")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, _, err := Paths(); err == nil {
		t.Error("missing XDG_RUNTIME_DIR: want an error, got nil")
	}
}
