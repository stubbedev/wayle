package icons

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	t.Setenv("HOME", "/home/u")
	if got, _ := DefaultPath(); got != "/data/wayle/icons" {
		t.Errorf("with XDG_DATA_HOME: %q", got)
	}
	// Set but empty is taken as given, as std::env::var does.
	t.Setenv("XDG_DATA_HOME", "")
	if got, _ := DefaultPath(); got != "wayle/icons" {
		t.Errorf("with an empty XDG_DATA_HOME: %q, want the relative wayle/icons", got)
	}
	_ = os.Unsetenv("XDG_DATA_HOME")
	if got, _ := DefaultPath(); got != "/home/u/.local/share/wayle/icons" {
		t.Errorf("from HOME: %q", got)
	}
	_ = os.Unsetenv("HOME")
	if _, err := DefaultPath(); !errors.Is(err, ErrHomeNotSet) {
		t.Errorf("neither set: err = %v, want ErrHomeNotSet", err)
	}
}

func TestEnsureSetup(t *testing.T) {
	base := t.TempDir()
	r := RegistryAt(base)
	if r.IsValid() {
		t.Fatal("an empty base is valid")
	}
	if err := r.EnsureSetup(); err != nil {
		t.Fatal(err)
	}
	if !r.IsValid() {
		t.Error("not valid after EnsureSetup")
	}
	if got := readFile(t, filepath.Join(base, "index.theme")); got != indexThemeContent {
		t.Errorf("index.theme = %q", got)
	}
	// An existing index.theme is the user's and stays.
	writeIcon(t, base, "index.theme", "mine")
	if err := r.EnsureSetup(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(base, "index.theme")); got != "mine" {
		t.Errorf("index.theme overwritten: %q", got)
	}

	file := writeIcon(t, t.TempDir(), "file", "")
	if err := RegistryAt(file).EnsureSetup(); !isDirectoryError(err) {
		t.Errorf("under a file: err = %v, want a DirectoryError", err)
	}
}

func TestThemeSearchPaths(t *testing.T) {
	data := t.TempDir()
	system := filepath.Join(data, "wayle", "icons")
	if err := os.MkdirAll(system, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_DIRS", data+"::/nonexistent")
	r := RegistryAt("/user/icons")

	got := r.ThemeSearchPaths([]string{"/usr/share/icons", system, "/user/icons", "/home/u/.icons"})
	want := []string{"/user/icons", system, "/usr/share/icons", "/home/u/.icons"}
	if !slices.Equal(got, want) {
		t.Errorf("ThemeSearchPaths = %v, want %v (the roots first, never twice)", got, want)
	}
	// A refresh over its own output changes nothing.
	if again := r.ThemeSearchPaths(got); !slices.Equal(again, want) {
		t.Errorf("refreshed = %v, want %v", again, want)
	}
}

func TestRegistryWatch(t *testing.T) {
	r := RegistryAt(t.TempDir())
	if err := r.EnsureSetup(); err != nil {
		t.Fatal(err)
	}
	dir := r.IconsDir()
	existing := writeIcon(t, dir, "ld-a-symbolic.svg", "")
	changes := make(chan struct{}, 16)
	stop, err := r.Watch(func() { changes <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	expect := func(what string, want bool) {
		t.Helper()
		select {
		case <-changes:
			if !want {
				t.Errorf("%s: a change was reported", what)
			}
		case <-time.After(300 * time.Millisecond):
			if want {
				t.Errorf("%s: no change reported", what)
			}
		}
	}

	// Rewriting an icon in place resolves the same file: no refresh.
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	expect("rewrite in place", false)

	writeIcon(t, dir, "ld-b-symbolic.svg", "")
	expect("install", true)
	if err := os.Rename(filepath.Join(dir, "ld-b-symbolic.svg"), filepath.Join(dir, "ld-c-symbolic.svg")); err != nil {
		t.Fatal(err)
	}
	expect("rename", true)
	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
	expect("remove", true)

	stop()
	writeIcon(t, dir, "ld-d-symbolic.svg", "")
	expect("after stop", false)

	if _, err := RegistryAt(filepath.Join(t.TempDir(), "absent")).Watch(func() {}); err == nil {
		t.Error("watching a missing directory succeeded")
	}
}

func TestErrorTexts(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&FetchError{Slug: "x", Source: "cdn", Status: 404}, "cannot fetch icon 'x' from cdn: HTTP 404 Not Found"},
		{&FetchError{Slug: "x", Source: "cdn", Status: 413}, "cannot fetch icon 'x' from cdn: HTTP 413 Payload Too Large"},
		{&FetchError{Slug: "x", Source: "cdn", Status: 599}, "cannot fetch icon 'x' from cdn: HTTP 599 <unknown status code>"},
		{&WriteError{Path: "/p", Err: syscall.EACCES}, "cannot write icon to '/p'"},
		{&DeleteError{Name: "n", Err: syscall.EACCES}, "cannot delete icon 'n'"},
		{&InvalidSourceError{Name: "nope", Available: "a, b"}, "unknown icon source 'nope', expected one of: a, b"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("%T: %q, want %q", tc.err, got, tc.want)
		}
	}
	if got := IOErrorString(&os.PathError{Op: "open", Path: "/x", Err: syscall.ENOENT}); got != "No such file or directory (os error 2)" {
		t.Errorf("IOErrorString(ENOENT) = %q", got)
	}
	if got := IOErrorString(errors.New("plain")); got != "plain" {
		t.Errorf("IOErrorString(plain) = %q", got)
	}
}

func TestSources(t *testing.T) {
	if s, err := SourceByCLIName("tabler-filled"); err != nil || s.Prefix != "tbf" {
		t.Errorf("tabler-filled = %+v %v", s, err)
	}
	_, err := SourceByCLIName("nope")
	if want := "unknown icon source 'nope', expected one of: tabler, tabler-filled, simple-icons, material, lucide"; err == nil || err.Error() != want {
		t.Errorf("unknown source: %v, want %q", err, want)
	}
	for _, tc := range []struct {
		source       Source
		url, install string
	}{
		{Tabler, "https://unpkg.com/@tabler/icons@latest/icons/outline/home.svg", "tb-home-symbolic"},
		{TablerFilled, "https://unpkg.com/@tabler/icons@latest/icons/filled/home.svg", "tbf-home-symbolic"},
		{SimpleIcons, "https://unpkg.com/simple-icons@latest/icons/home.svg", "si-home-symbolic"},
		{Material, "https://cdn.jsdelivr.net/npm/@material-symbols/svg-400/outlined/home.svg", "md-home-symbolic"},
		{Lucide, "https://unpkg.com/lucide-static@latest/icons/home.svg", "ld-home-symbolic"},
	} {
		if got := tc.source.CDNURL("home"); got != tc.url {
			t.Errorf("%s url = %q", tc.source.CLIName, got)
		}
		if got := tc.source.InstalledName("home"); got != tc.install {
			t.Errorf("%s installed name = %q", tc.source.CLIName, got)
		}
	}
	if got := AllPrefixes(); !slices.Equal(got, []string{"tb", "tbf", "si", "md", "ld", "cm"}) {
		t.Errorf("AllPrefixes = %v", got)
	}
}

func isDirectoryError(err error) bool {
	_, ok := errors.AsType[*DirectoryError](err)
	return ok
}
