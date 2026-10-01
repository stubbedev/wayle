package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/internal/icons/icontest"
	"github.com/stubbedev/wayle/resources"
)

// TestIconsCLIMatchesRust replays testdata/icons/steps.txt, the session
// capture.sh recorded from the Rust binary, in a fresh home: every
// step's stdout, stderr and exit code must match.
func TestIconsCLIMatchesRust(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_DIRS", "")
	if err := os.CopyFS(filepath.Join(root, "input"), os.DirFS("testdata/icons/input")); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile("testdata/icons/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config", "wayle"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "wayle", "config.toml"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	steps, err := os.ReadFile("testdata/icons/steps.txt")
	if err != nil {
		t.Fatal(err)
	}
	golden := func(name string) string {
		data, err := os.ReadFile(filepath.Join("testdata", "icons", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	wd, _ := os.Getwd()
	n := 0
	for line := range strings.Lines(string(steps)) {
		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}
		n++
		for i, a := range args {
			args[i] = strings.ReplaceAll(a, "@ROOT@", root)
		}
		name := strconv.Itoa(n)
		if n < 10 {
			name = "0" + name
		}
		t.Chdir(root)
		stdout, stderr, code := runCaptured(t, false, append([]string{"icons"}, args...)...)
		t.Chdir(wd)
		if got, want := strings.ReplaceAll(stdout, root, "@ROOT@"), golden(name+".stdout"); got != want {
			t.Errorf("step %s (icons %s) stdout:\n--- got\n%s--- want\n%s", name, line, got, want)
		}
		if got, want := strings.ReplaceAll(stderr, root, "@ROOT@"), golden(name+".stderr"); got != want {
			t.Errorf("step %s (icons %s) stderr:\n--- got\n%s--- want\n%s", name, line, got, want)
		}
		if want := strings.TrimSpace(golden(name + ".code")); strconv.Itoa(code) != want {
			t.Errorf("step %s (icons %s) exit code %d, want %s", name, line, code, want)
		}
	}
}

// TestIconsInstallAndSync installs through a fake CDN (the Rust capture
// needs the network for these): what installs is reported and on disk,
// a failed fetch is reported on stderr without failing the command,
// and sync installs only what the config references and lacks.
func TestIconsInstallAndSync(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_DIRS", "")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24z"/></svg>`
	cdn := icontest.NewCDN(map[string]string{
		icons.Tabler.CDNURL("home"):       svg,
		icons.Lucide.CDNURL("bell"):       svg,
		icons.SimpleIcons.CDNURL("gmail"): svg,
	})
	newIconManager = func() (*icons.Manager, error) {
		r, err := icons.NewRegistry()
		if err != nil {
			return nil, err
		}
		return icons.NewManagerWith(r, cdn.Client()), nil
	}
	t.Cleanup(func() { newIconManager = icons.NewManager })

	stdout, stderr, code := runCaptured(t, false, "icons", "install", "tabler", "home", "gone")
	if code != 0 || stdout != "Installed: tb-home-symbolic\n" ||
		stderr != "Failed: gone - cannot fetch icon 'gone' from cdn: HTTP 404 Not Found\n\n1 installed, 1 failed\n" {
		t.Errorf("install: code %d\nstdout %q\nstderr %q", code, stdout, stderr)
	}

	dir := filepath.Join(root, "config", "wayle")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "[modules.clock]\nicon-name = \"ld-bell-symbolic\"\n[modules.cpu]\nicon-name = \"tb-home-symbolic\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(cdn.Requested())
	stdout, _, code = runCaptured(t, false, "icons", "sync")
	if code != 0 || !strings.Contains(stdout, "Installed: ld-bell-symbolic\n") {
		t.Errorf("sync: code %d\n%s", code, stdout)
	}
	for _, url := range cdn.Requested()[before:] {
		if url == icons.Tabler.CDNURL("home") {
			t.Error("sync refetched an installed icon")
		}
	}
	m, _ := newIconManager()
	if !m.IsInstalled("ld-bell-symbolic") {
		t.Error("sync did not install the referenced icon")
	}
}

// TestIconsSetupInstallsTheBundledIcons pins setup to the embedded
// resources (the packaged Rust binary looks for them at its build path,
// which a nix build does not keep, and fails): every bundled SVG lands
// in the icons directory, reported one per line, then the count.
func TestIconsSetupInstallsTheBundledIcons(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	bundled, err := fs.Glob(resources.Icons, resources.IconsDir+"/*.svg")
	if err != nil || len(bundled) == 0 {
		t.Fatalf("no bundled icons: %v", err)
	}
	dest := filepath.Join(root, "wayle", "icons", "hicolor", "scalable", "actions")
	var want strings.Builder
	for _, path := range bundled {
		want.WriteString("Installed: " + strings.TrimSuffix(filepath.Base(path), ".svg") + "\n")
	}
	want.WriteString("\n" + strconv.Itoa(len(bundled)) + " icons installed to " + dest + "\n")

	stdout, stderr, code := runCaptured(t, false, "icons", "setup")
	if code != 0 || stderr != "" || stdout != want.String() {
		t.Fatalf("setup: code %d stderr %q\n%s", code, stderr, stdout)
	}
	for _, path := range bundled {
		src, _ := resources.Icons.ReadFile(path)
		got, err := os.ReadFile(filepath.Join(dest, filepath.Base(path)))
		if err != nil || string(got) != string(src) {
			t.Errorf("%s not copied verbatim: %v", filepath.Base(path), err)
		}
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", file)
	if _, stderr, code := runCaptured(t, false, "icons", "setup"); code != 1 || !strings.HasPrefix(stderr, "Error: Failed to create icons directory: ") {
		t.Errorf("setup under a file: code %d stderr %q", code, stderr)
	}
}
