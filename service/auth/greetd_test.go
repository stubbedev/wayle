package auth

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGreetd plays greetd over one end of a pipe: it records every
// request and answers from a script keyed by request order.
type fakeGreetd struct {
	t        *testing.T
	conn     net.Conn
	requests []map[string]any
	raw      []string
}

func (f *fakeGreetd) read() map[string]any {
	f.t.Helper()
	var head [4]byte
	if _, err := io.ReadFull(f.conn, head[:]); err != nil {
		f.t.Errorf("fake greetd read: %v", err)
		return nil
	}
	buf := make([]byte, binary.NativeEndian.Uint32(head[:]))
	if _, err := io.ReadFull(f.conn, buf); err != nil {
		f.t.Errorf("fake greetd read body: %v", err)
		return nil
	}
	var req map[string]any
	if err := json.Unmarshal(buf, &req); err != nil {
		f.t.Errorf("fake greetd: bad JSON %s: %v", buf, err)
	}
	f.requests = append(f.requests, req)
	f.raw = append(f.raw, string(buf))
	return req
}

func (f *fakeGreetd) reply(body string) {
	f.t.Helper()
	frame := binary.NativeEndian.AppendUint32(nil, uint32(len(body)))
	if _, err := f.conn.Write(append(frame, body...)); err != nil {
		f.t.Errorf("fake greetd write: %v", err)
	}
}

// runGreetd runs a login against a scripted greetd; serve plays the
// daemon side.
func runGreetd(t *testing.T, username string, ask Ask, cmd func() []string, serve func(*fakeGreetd)) (*fakeGreetd, error) {
	t.Helper()
	client, server := net.Pipe()
	f := &fakeGreetd{t: t, conn: server}
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(f)
	}()
	g := NewGreetd(client, cmd, []string{"XDG_SESSION_TYPE=wayland"})
	err := g.Run(username, ask)
	_ = g.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fake greetd did not finish")
	}
	_ = server.Close()
	return f, err
}

// TestGreetdLogin pins the happy path and the wire format: the
// username up front, the stashed password for the secret prompt, null
// for an info message, and the session argv read at start_session.
func TestGreetdLogin(t *testing.T) {
	session := []string{"sway"}
	cmd := func() []string { return session }
	var seen []Prompt
	f, err := runGreetd(t, "alice", func(p Prompt) (string, bool) {
		seen = append(seen, p)
		session = []string{"niri", "--session"} // the picker changed mid-login
		if p.WantsInput() {
			return "hunter2", true
		}
		return "", false
	}, cmd, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"auth_message","auth_message_type":"info","auth_message":"Welcome"}`)
		f.read()
		f.reply(`{"type":"auth_message","auth_message_type":"secret","auth_message":"Password:"}`)
		f.read()
		f.reply(`{"type":"success"}`)
		f.read()
		f.reply(`{"type":"success"}`)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantRaw := []string{
		`{"type":"create_session","username":"alice"}`,
		`{"type":"post_auth_message_response","response":null}`,
		`{"type":"post_auth_message_response","response":"hunter2"}`,
		`{"type":"start_session","cmd":["niri","--session"],"env":["XDG_SESSION_TYPE=wayland"]}`,
	}
	if strings.Join(f.raw, "\n") != strings.Join(wantRaw, "\n") {
		t.Errorf("wire =\n%s\nwant\n%s", strings.Join(f.raw, "\n"), strings.Join(wantRaw, "\n"))
	}
	if len(seen) != 2 || seen[0].Kind != PromptInfo || seen[1].Kind != PromptSecret || seen[1].Text != "Password:" {
		t.Errorf("prompts = %+v", seen)
	}
}

// TestGreetdAuthErrorCancels: a greetd error ends the login with its
// description and cancels the session.
func TestGreetdAuthErrorCancels(t *testing.T) {
	f, err := runGreetd(t, "alice", func(Prompt) (string, bool) { return "wrong", true }, func() []string { return nil }, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"auth_message","auth_message_type":"secret","auth_message":"Password:"}`)
		f.read()
		f.reply(`{"type":"error","error_type":"auth_error","description":"pam_authenticate: AUTH_ERR"}`)
		f.read()
	})
	if err == nil || err.Error() != "pam_authenticate: AUTH_ERR" {
		t.Fatalf("Run = %v, want the greetd description", err)
	}
	if last := f.requests[len(f.requests)-1]; last["type"] != "cancel_session" {
		t.Errorf("last request = %v, want cancel_session", last)
	}
}

// TestGreetdCancelledPrompt: a cancelled input prompt cancels the
// session instead of answering it.
func TestGreetdCancelledPrompt(t *testing.T) {
	f, err := runGreetd(t, "alice", func(Prompt) (string, bool) { return "", false }, func() []string { return nil }, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"auth_message","auth_message_type":"visible","auth_message":"OTP:"}`)
		f.read()
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run = %v, want ErrCancelled", err)
	}
	if len(f.requests) != 2 || f.requests[1]["type"] != "cancel_session" {
		t.Errorf("requests = %v", f.requests)
	}
}

// TestGreetdAsksForUsername: without a username the backend asks for
// one first; cancelling that ends the login before greetd hears of it.
func TestGreetdAsksForUsername(t *testing.T) {
	var first Prompt
	f, err := runGreetd(t, "", func(p Prompt) (string, bool) {
		if first.Text == "" {
			first = p
			return "bob", true
		}
		return "pw", true
	}, func() []string { return []string{"sway"} }, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"success"}`)
		f.read()
		f.reply(`{"type":"success"}`)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if first.Kind != PromptVisible || first.Text != "Username" || f.requests[0]["username"] != "bob" {
		t.Errorf("username prompt %+v, create_session %v", first, f.requests[0])
	}

	_, err = runGreetd(t, "", func(Prompt) (string, bool) { return "", false }, func() []string { return nil }, func(*fakeGreetd) {})
	if !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled username prompt = %v", err)
	}
}

func TestGreetdStartSessionFailure(t *testing.T) {
	_, err := runGreetd(t, "alice", func(Prompt) (string, bool) { return "", false }, func() []string { return []string{"missing"} }, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"success"}`)
		f.read()
		f.reply(`{"type":"error","error_type":"error","description":"exec failed"}`)
	})
	if err == nil || err.Error() != "exec failed" {
		t.Fatalf("Run = %v", err)
	}
	_, err = runGreetd(t, "alice", func(Prompt) (string, bool) { return "", false }, func() []string { return nil }, func(f *fakeGreetd) {
		f.read()
		f.reply(`{"type":"success"}`)
		f.read()
		f.reply(`{"type":"auth_message","auth_message_type":"info","auth_message":"?"}`)
	})
	if err == nil || err.Error() != "unexpected response to start_session" {
		t.Fatalf("Run = %v", err)
	}
}

// TestGreetdRejectsUnknownVariants: responses serde would refuse are
// errors, not silently mapped.
func TestGreetdRejectsUnknownVariants(t *testing.T) {
	for _, body := range []string{
		`{"type":"auth_message","auth_message_type":"fingerprint","auth_message":"touch"}`,
		`{"type":"error","error_type":"weird","description":"x"}`,
		`{"type":"hello"}`,
		`not json`,
	} {
		_, err := runGreetd(t, "alice", func(Prompt) (string, bool) { return "x", true }, func() []string { return nil }, func(f *fakeGreetd) {
			f.read()
			f.reply(body)
		})
		if err == nil {
			t.Errorf("response %s accepted", body)
		}
	}
}

func TestGreetdFromEnv(t *testing.T) {
	t.Setenv(GreetdSockEnv, "")
	if _, err := NewGreetdFromEnv(nil, nil); err == nil || !strings.Contains(err.Error(), "not running under greetd") {
		t.Errorf("unset GREETD_SOCK = %v", err)
	}
	path := filepath.Join(t.TempDir(), "greetd.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	t.Setenv(GreetdSockEnv, path)
	g, err := NewGreetdFromEnv(func() []string { return nil }, nil)
	if err != nil {
		t.Fatalf("NewGreetdFromEnv: %v", err)
	}
	_ = g.Close()
	_ = os.Remove(path)
	if _, err := NewGreetdFromEnv(nil, nil); err == nil {
		t.Error("a missing socket connected")
	}
}
