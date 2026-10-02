// Package pwtest starts a private PipeWire daemon for tests, so
// producers and consumers link without touching the user's session.
package pwtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// daemonConf is a private PipeWire daemon: the native protocol, the
// client-node and adapter modules a pw_stream needs, link creation,
// and a dummy driver so a linked graph runs with no devices.
const daemonConf = `
context.properties = {
    core.daemon = true
    core.name   = pipewire-0
    support.dbus = false
}
context.spa-libs = {
    support.* = support/libspa-support
    video.convert.* = videoconvert/libspa-videoconvert
}
context.modules = [
    { name = libpipewire-module-protocol-native }
    { name = libpipewire-module-metadata }
    { name = libpipewire-module-spa-node-factory }
    { name = libpipewire-module-client-node }
    { name = libpipewire-module-access }
    { name = libpipewire-module-adapter }
    { name = libpipewire-module-link-factory }
]
context.objects = [
    { factory = spa-node-factory
        args = {
            factory.name    = support.node.driver
            node.name       = Dummy-Driver
            node.group      = pipewire.dummy
            priority.driver = 20000
        }
    }
]
`

// Start runs a private pipewire for the test and points the
// client library at it. A missing pipewire fails the test: the .#go
// devShell provides one.
func Start(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("pipewire")
	if err != nil {
		t.Fatalf("pipewire not on PATH: %v", err)
	}
	dir, err := os.MkdirTemp("", "pwt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	conf := filepath.Join(dir, "test.conf")
	if err := os.WriteFile(conf, []byte(daemonConf), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIPEWIRE_RUNTIME_DIR", dir)
	t.Setenv("PIPEWIRE_REMOTE", "pipewire-0")
	cmd := exec.Command(bin, "-c", conf) //nolint:gosec // the daemon from PATH, for a test
	// The daemon alone gets the private runtime dir: the test keeps its
	// own, where a headless compositor's socket may live.
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+dir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	sock := filepath.Join(dir, "pipewire-0")
	for range 100 {
		if _, err := os.Stat(sock); err == nil {
			return dir
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pipewire did not create its socket")
	return ""
}
