package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// recordRust, when set to the Rust wayle binary, re-records
// testdata/wallpaper-cli.golden from it (against the Go daemon, the
// same private bus and the same scenarios) instead of checking the Go
// CLI:
//
//	go test ./cmd/wayle -run TestWallpaperCLIMatchesRust -record-rust "$(command -v wayle)"
var recordRust = flag.String("record-rust", "", "record the wallpaper CLI golden from this Rust wayle binary")

// wallpaperScenarios run against a fresh daemon each: DP-1 and DP-2
// registered, @DIR@ holding a.png and b.png, @EMPTY@ holding no image.
// "no-daemon" scenarios run with nothing owning the name.
var wallpaperScenarios = []struct {
	name     string
	noDaemon bool
	steps    [][]string
}{
	{name: "info-fresh", steps: [][]string{{"info"}, {"info", "--monitor", "DP-1"}}},
	{name: "set-all", steps: [][]string{{"set", "@DIR@/a.png"}, {"info", "--monitor", "DP-1"}, {"info", "--monitor", "DP-2"}}},
	{name: "set-monitor-fit", steps: [][]string{
		{"set", "@DIR@/b.png", "--fit", "center", "--monitor", "DP-2"},
		{"info", "--monitor", "DP-2"},
		{"info", "--monitor", "DP-1"},
		{"info"},
	}},
	{name: "set-tile", steps: [][]string{{"set", "@DIR@/a.png", "-f", "tile"}}},
	{name: "set-missing", steps: [][]string{{"set", "/nonexistent/x.png"}, {"set", "/nonexistent/x.png", "--monitor", "DP-1"}}},
	{name: "cycle", steps: [][]string{
		{"cycle", "@DIR@"},
		{"info", "--monitor", "DP-1"},
		{"next"},
		{"info", "--monitor", "DP-2"},
		{"previous"},
		{"info", "--monitor", "DP-1"},
		{"stop"},
		{"info"},
	}},
	// Shuffle picks random images, so only the start message is compared.
	{name: "cycle-flags", steps: [][]string{{"cycle", "@DIR@", "-i", "60", "-m", "shuffle"}, {"cycle", "@DIR@", "--interval=5", "--mode=sequential"}}},
	{name: "cycle-missing", steps: [][]string{{"cycle", "/nonexistent/dir"}}},
	{name: "cycle-empty", steps: [][]string{{"cycle", "@EMPTY@"}}},
	{name: "next-without-cycle", steps: [][]string{{"next"}, {"previous"}, {"stop"}}},
	{name: "theming", steps: [][]string{{"theming-monitor", "DP-1"}, {"theming-monitor", ""}}},
	{name: "not-running", noDaemon: true, steps: [][]string{{"info"}, {"stop"}, {"set", "@DIR@/a.png"}}},
}

// startBus runs a private dbus-daemon and points the session address
// at it.
func startBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(bin, "--session", "--nofork", "--nopidfile", "--print-address=1", "--address="+addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("dbus-daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	addr = strings.TrimSpace(line)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	return addr
}

func runScenario(t *testing.T, sc struct {
	name     string
	noDaemon bool
	steps    [][]string
},
) string {
	t.Helper()
	addr := startBus(t)
	dir, empty := t.TempDir(), t.TempDir()
	for _, n := range []string{"a.png", "b.png"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !sc.noDaemon {
		conn, err := dbus.Connect(addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		svc := wallpaper.New(wallpaper.Options{Extractor: extract.Config{Tool: extract.None}})
		svc.RegisterMonitor("DP-1")
		svc.RegisterMonitor("DP-2")
		release, err := wallpaper.Export(conn, svc)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
	}
	var log strings.Builder
	for _, step := range sc.steps {
		args := make([]string, len(step))
		for i, a := range step {
			args[i] = strings.NewReplacer("@DIR@", dir, "@EMPTY@", empty).Replace(a)
		}
		fmt.Fprintf(&log, "$ wallpaper %q\n", step)
		var stdout, stderr string
		code := 0
		if *recordRust != "" {
			stdout, stderr, code = runRust(t, args)
		} else {
			var out bytes.Buffer
			if err := wallpaperCommand(context.Background(), args, &out); err != nil {
				stderr, code = "Error: "+err.Error()+"\n", 1
			}
			stdout = out.String()
		}
		fmt.Fprintf(&log, "%s[stderr] %s[exit %d]\n", stdout, stderr, code)
	}
	return strings.NewReplacer(dir, "@DIR@", empty, "@EMPTY@").Replace(log.String())
}

func runRust(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(*recordRust, append([]string{"wallpaper"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/config",
		"XDG_CACHE_HOME="+home+"/cache", "XDG_DATA_HOME="+home+"/data", "XDG_STATE_HOME="+home+"/state", "RUST_LOG=off")
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return o.String(), e.String(), code
}

// TestWallpaperCLIMatchesRust replays the scenarios through the Go CLI
// and compares stdout, the error line, and the exit code with what the
// Rust CLI printed against the same daemon state.
func TestWallpaperCLIMatchesRust(t *testing.T) {
	golden := filepath.Join("testdata", "wallpaper-cli.golden")
	var all strings.Builder
	for _, sc := range wallpaperScenarios {
		fmt.Fprintf(&all, "### %s\n", sc.name)
		t.Run(sc.name, func(t *testing.T) { all.WriteString(runScenario(t, sc)) })
	}
	if *recordRust != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(all.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if all.String() != string(want) {
		t.Errorf("Go wallpaper CLI output differs from the Rust recording:\n--- got\n%s\n--- want\n%s", all.String(), want)
	}
}

func TestWallpaperCLIRejectsBadArgsBeforeDialing(t *testing.T) {
	// No bus at all: every case must fail in parsing, not connecting.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/bus")
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"set"},
		{"set", "a.png", "--fit", "cover"},
		{"set", "a.png", "--fit"},
		{"set", "a.png", "--bogus", "x"},
		{"cycle", "dir", "--interval", "-5"},
		{"cycle", "dir", "--mode", "random"},
		{"stop", "extra"},
		{"theming-monitor"},
	} {
		err := wallpaperCommand(context.Background(), args, &bytes.Buffer{})
		if err == nil || strings.Contains(err.Error(), "D-Bus") {
			t.Errorf("%q: err = %v, want a parse error", args, err)
		}
	}
}

func TestWallpaperFlagsForms(t *testing.T) {
	aliases := map[string]string{"-f": "--fit", "--fit": "--fit", "--monitor": "--monitor"}
	pos, flags, err := wallpaperFlags([]string{"a.png", "--fit=fit", "--monitor", "DP-1"}, aliases)
	if err != nil || len(pos) != 1 || flags["--fit"] != "fit" || flags["--monitor"] != "DP-1" {
		t.Errorf("= %v %v %v", pos, flags, err)
	}
	if _, flags, _ := wallpaperFlags([]string{"-f", "center"}, aliases); flags["--fit"] != "center" {
		t.Errorf("short alias = %v", flags)
	}
}
