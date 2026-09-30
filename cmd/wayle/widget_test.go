package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stubbedev/wayle/internal/widgetipc"
)

func TestWidgetUpdateReachesTheShell(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := widgetipc.NewServer()
	stop, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	stdout, stderr, code := runCaptured(t, false, "widget", "update", "gpu", `{"text":"42"}`)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	select {
	case u := <-srv.Updates():
		if u.ID != "gpu" || u.Output != `{"text":"42"}` {
			t.Errorf("update = %+v", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no update reached the shell")
	}
}

func TestWidgetUpdateWithoutShellFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	_, stderr, code := runCaptured(t, false, "widget", "update", "gpu", "42")
	want := "Error: cannot connect to wayle widget socket at " + filepath.Join(dir, "wayle", "widget.sock") + ": No such file or directory (os error 2)\n"
	if code != 1 || stderr != want {
		t.Errorf("code %d stderr %q, want %q", code, stderr, want)
	}
}

func TestToastNeedsLabelOrPreset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := widgetipc.NewServer()
	stop, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, stderr, code := runCaptured(t, false, "toast", "--icon", "x"); code != 1 || stderr != "Error: a toast needs a label or --preset\n" {
		t.Errorf("no label: code %d %q", code, stderr)
	}
	if _, stderr, code := runCaptured(t, false, "toast", "--preset", "vol", "--percentage", "50"); code != 0 {
		t.Fatalf("preset toast: code %d %q", code, stderr)
	}
	got := <-srv.Toasts()
	if got.Preset == nil || *got.Preset != "vol" || got.Percentage == nil || *got.Percentage != 50 || got.Label != nil {
		t.Errorf("toast = %+v", got)
	}
}
