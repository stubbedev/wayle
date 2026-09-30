package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The launcher socket's lifetime contract (wayle/tests/
// launcher_session.rs), against the real CLI process: the shell treats
// EOF on the session socket as the client dying and tears the surface
// down, so the CLI must hold its write half open for the whole session
// and close it only when it exits. A regression makes every -show
// session flash open and vanish with the CLI exiting 1 in silence.

// TestMain lets the test binary be the CLI: with WAYLE_TEST_CLI set it
// runs main on its arguments instead of the tests.
// fakeDaemon is the runtime dir the CLI is pointed at, plus its
// listener.
type fakeDaemon struct {
	runtimeDir string
	listener   net.Listener
}

func bindDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	// Short path on purpose: a unix socket path must fit in SUN_LEN.
	dir, err := os.MkdirTemp("", "wl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(filepath.Join(dir, "wayle"), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "wayle", "launcher.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return &fakeDaemon{runtimeDir: dir, listener: ln}
}

// cliProcess is a running CLI process; done yields its Wait result once.
type cliProcess struct {
	cmd  *exec.Cmd
	done chan error
}

// spawn starts the CLI against this daemon.
func (d *fakeDaemon) spawn(t *testing.T, stdin io.Reader, args ...string) *cliProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "WAYLE_TEST_CLI=1", "XDG_RUNTIME_DIR="+d.runtimeDir)
	cmd.Stdin = stdin
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &cliProcess{cmd: cmd, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return c
}

// exited reports whether the CLI has exited, without waiting.
func (c *cliProcess) exited() bool {
	select {
	case err := <-c.done:
		c.done <- err
		return true
	default:
		return false
	}
}

// wait blocks for the CLI's exit.
func (c *cliProcess) wait() error {
	err := <-c.done
	c.done <- err
	return err
}

// acceptSession accepts the session, asserts the open frame, and
// replies opened.
func (d *fakeDaemon) acceptSession(t *testing.T) (*bufio.Reader, net.Conn) {
	t.Helper()
	conn, err := d.listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	reader := bufio.NewReader(conn)
	open, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(open, `"type":"open"`) {
		t.Fatalf("first frame was not open: %q %v", open, err)
	}
	if _, err := conn.Write([]byte("{\"type\":\"opened\"}\n")); err != nil {
		t.Fatal(err)
	}
	return reader, conn
}

// expectSilence asserts the client neither writes nor half-closes
// within the window.
func expectSilence(t *testing.T, conn net.Conn, reader *bufio.Reader, what string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	b, err := reader.ReadByte()
	switch {
	case err == nil:
		t.Fatalf("unexpected client byte %q %s", b, what)
	case errors.Is(err, io.EOF):
		t.Fatalf("daemon saw EOF %s: the CLI half-closed its socket", what)
	default:
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("unexpected socket error %s: %v", what, err)
		}
	}
}

// Positive: a live -show session keeps the socket open both ways, so
// the daemon never sees the EOF that would cancel it.
func TestALiveShowSessionNeverLooksLikeADeadClient(t *testing.T) {
	d := bindDaemon(t)
	c := d.spawn(t, nil, "launcher", "-show", "drun")
	reader, conn := d.acceptSession(t)
	expectSilence(t, conn, reader, "while the session was live")
	if c.exited() {
		t.Fatal("CLI exited while the session was still open")
	}
}

// Negative: the socket closes exactly once, when the CLI exits on a
// terminal frame - the EOF the daemon relies on to reap a dead client.
func TestClientExitClosesTheSocketAndReturnsTheDaemonCode(t *testing.T) {
	d := bindDaemon(t)
	c := d.spawn(t, nil, "launcher", "-show", "drun")
	reader, conn := d.acceptSession(t)
	if _, err := conn.Write([]byte("{\"type\":\"cancelled\",\"code\":1}\n")); err != nil {
		t.Fatal(err)
	}
	err := c.wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("cancel must exit with rofi's code 1, got %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Errorf("socket must reach EOF once the CLI is gone, got %v", err)
	}
}

// Positive: a result frame prints rofi-style and exits with its code.
func TestAResultPrintsAndExitsWithItsCode(t *testing.T) {
	d := bindDaemon(t)
	cmd := exec.Command(os.Args[0], "launcher", "-dmenu", "-format", "i")
	cmd.Env = append(os.Environ(), "WAYLE_TEST_CLI=1", "XDG_RUNTIME_DIR="+d.runtimeDir)
	cmd.Stdin = strings.NewReader("alpha\nbravo\n")
	out := &strings.Builder{}
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader, conn := d.acceptSession(t)
	_, _ = reader.ReadString('\n') // rows
	_, _ = reader.ReadString('\n') // rows-done
	_, _ = conn.Write([]byte("{\"type\":\"result\",\"code\":10,\"selected\":[{\"index\":1,\"text\":\"bravo\"}],\"filter\":\"b\"}\n"))
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 10 {
		t.Fatalf("exit = %v, want 10", err)
	}
	if out.String() != "1\n" {
		t.Errorf("stdout = %q, want the index", out.String())
	}
}

// Positive: -dmenu owns the write half - rows and the EOF marker reach
// the daemon, the one case that legitimately writes.
func TestADmenuSessionStreamsRowsThenSignalsEOF(t *testing.T) {
	d := bindDaemon(t)
	d.spawn(t, strings.NewReader("alpha\nbravo\n"), "launcher", "-dmenu")
	reader, _ := d.acceptSession(t)
	rows, _ := reader.ReadString('\n')
	if !strings.Contains(rows, `"type":"rows"`) || !strings.Contains(rows, "alpha") || !strings.Contains(rows, "bravo") {
		t.Fatalf("expected a rows frame, got %q", rows)
	}
	done, _ := reader.ReadString('\n')
	if !strings.Contains(done, `"type":"rows-done"`) {
		t.Fatalf("expected rows-done after stdin EOF, got %q", done)
	}
}

// Negative: finishing the rows must not look like the client dying -
// a pump that dropped its write half made every -dmenu menu vanish the
// instant its rows arrived.
func TestADmenuSessionStaysOpenAfterItsRowsAreDone(t *testing.T) {
	d := bindDaemon(t)
	c := d.spawn(t, strings.NewReader("alpha\nbravo\n"), "launcher", "-dmenu")
	reader, conn := d.acceptSession(t)
	_, _ = reader.ReadString('\n')
	done, _ := reader.ReadString('\n')
	if !strings.Contains(done, `"type":"rows-done"`) {
		t.Fatalf("expected rows-done first, got %q", done)
	}
	expectSilence(t, conn, reader, "once the rows were done")
	if c.exited() {
		t.Fatal("CLI exited while the dmenu session was still open")
	}
}

// Negative: no shell running is a clean failure, not a hang.
func TestNoShellIsExitOne(t *testing.T) {
	cmd := exec.Command(os.Args[0], "launcher", "-show", "drun")
	cmd.Env = append(os.Environ(), "WAYLE_TEST_CLI=1", "XDG_RUNTIME_DIR="+t.TempDir())
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !strings.Contains(stderr.String(), "cannot connect to wayle launcher socket") {
		t.Errorf("exit = %v, stderr = %q", err, stderr.String())
	}
}
