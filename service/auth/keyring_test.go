package auth

import (
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKeyring serves one control-socket exchange: it captures the
// request bytes and answers with the given result (and length).
func fakeKeyring(t *testing.T, length, result uint32) (path string, got <-chan []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "control")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	ch := make(chan []byte, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		head := make([]byte, 13)
		if _, err := io.ReadFull(conn, head); err != nil {
			ch <- nil
			return
		}
		arg := make([]byte, binary.BigEndian.Uint32(head[9:13]))
		_, _ = io.ReadFull(conn, arg)
		ch <- append(head, arg...)
		var resp [8]byte
		binary.BigEndian.PutUint32(resp[0:4], length)
		binary.BigEndian.PutUint32(resp[4:8], result)
		_, _ = conn.Write(resp[:])
	}()
	return path, ch
}

// TestKeyringUnlockWire pins GKD_CONTROL_OP_UNLOCK: the credentials
// byte, then big-endian packet length, op, and the password.
func TestKeyringUnlockWire(t *testing.T) {
	path, got := fakeKeyring(t, 8, keyringResultOK)
	if err := keyringUnlock(path, "hunter2"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	msg := <-got
	want := []byte{0, 0, 0, 0, 19, 0, 0, 0, 1, 0, 0, 0, 7}
	want = append(want, "hunter2"...)
	if string(msg) != string(want) {
		t.Errorf("request = %v, want %v", msg, want)
	}
}

func TestKeyringUnlockFailures(t *testing.T) {
	for _, tc := range []struct {
		length, result uint32
		want           string
	}{
		{8, keyringDenied, "denied"},
		{8, keyringNoDaemon, "no daemon"},
		{8, 2, "daemon returned 2"},
		{12, keyringResultOK, "unexpected response length 12"},
	} {
		path, _ := fakeKeyring(t, tc.length, tc.result)
		err := keyringUnlock(path, "pw")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("result %d/len %d: err = %v, want %q", tc.result, tc.length, err, tc.want)
		}
	}
	if err := keyringUnlock(filepath.Join(t.TempDir(), "absent"), "pw"); err == nil {
		t.Error("a missing daemon socket reported success")
	}
}

func TestKeyringControlPath(t *testing.T) {
	t.Setenv("GNOME_KEYRING_CONTROL", "/run/user/1000/keyring-x")
	if got := keyringControlPath(); got != "/run/user/1000/keyring-x/control" {
		t.Errorf("GNOME_KEYRING_CONTROL path = %q", got)
	}
	t.Setenv("GNOME_KEYRING_CONTROL", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := keyringControlPath(); got != "/run/user/1000/keyring/control" {
		t.Errorf("runtime-dir path = %q", got)
	}
}
