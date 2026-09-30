package logging

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDirFollowsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got, _ := Dir(); got != "/xdg/state/wayle" {
		t.Errorf("Dir = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got, _ := Dir(); got != "/home/u/.local/state/wayle" {
		t.Errorf("fallback = %q", got)
	}
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Error("no HOME and no XDG_STATE_HOME resolved")
	}
}

func TestDailyFileRollsAndPrunes(t *testing.T) {
	dir := t.TempDir()
	// Stale files: nine older days of this prefix and one of another.
	for day := 1; day <= 9; day++ {
		name := filepath.Join(dir, "wayle.2026-01-0"+string(rune('0'+day))+".log")
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dir, "wayle-shell.2026-01-01.log")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)
	w := &dailyFile{dir: dir, prefix: "wayle", now: func() time.Time { return now }}
	defer w.Close()
	if _, err := w.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // past midnight: a new file
	if _, err := w.Write([]byte("two\n")); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "wayle.2026-09-30.log"))
	second, _ := os.ReadFile(filepath.Join(dir, "wayle.2026-10-01.log"))
	if string(first) != "one\n" || string(second) != "two\n" {
		t.Errorf("files = %q, %q", first, second)
	}
	entries, _ := os.ReadDir(dir)
	var ours []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "wayle.") {
			ours = append(ours, e.Name())
		}
	}
	if len(ours) != daysToKeep || !slices.Contains(ours, "wayle.2026-10-01.log") {
		t.Errorf("kept %v", ours)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another prefix's log was pruned: %v", err)
	}
}

func TestSetupRoutesTheStandardLogger(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	defer log.SetOutput(os.Stderr)
	var console bytes.Buffer
	closer, err := Setup("wayle-shell", &console)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("hello")
	_ = closer.Close()
	dir, _ := Dir()
	files, _ := filepath.Glob(filepath.Join(dir, "wayle-shell.*.log"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	body, _ := os.ReadFile(files[0])
	if !strings.Contains(string(body), "hello") || !strings.Contains(console.String(), "hello") {
		t.Errorf("file %q console %q", body, console.String())
	}

	// File-only (the CLI): nothing on the console.
	console.Reset()
	closer, err = Setup("wayle", nil)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("quiet")
	_ = closer.Close()
	if console.Len() != 0 {
		t.Errorf("CLI logged to the console: %q", console.String())
	}
}
