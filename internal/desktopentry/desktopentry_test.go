package desktopentry

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEntry(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIconLookup(t *testing.T) {
	user, system := t.TempDir(), t.TempDir()
	writeEntry(t, system, "vlc.desktop", "[Desktop Entry]\nName=VLC\nIcon=vlc\n")
	// The user dir shadows the system one.
	writeEntry(t, user, "mpv.desktop", "[Desktop Entry]\nIcon=mpv-user\n")
	writeEntry(t, system, "mpv.desktop", "[Desktop Entry]\nIcon=mpv\n")
	// Icon keys outside [Desktop Entry] do not count.
	writeEntry(t, system, "odd.desktop", "[Desktop Action new]\nIcon=wrong\n[Desktop Entry]\nName=Odd\n")
	dirs := []string{user, system}

	if icon, ok := Icon("vlc", dirs); !ok || icon != "vlc" {
		t.Errorf("vlc = %q, %v", icon, ok)
	}
	if icon, ok := Icon("vlc.desktop", dirs); !ok || icon != "vlc" {
		t.Errorf("suffixed id = %q, %v", icon, ok)
	}
	if icon, _ := Icon("mpv", dirs); icon != "mpv-user" {
		t.Errorf("mpv = %q, want the user entry", icon)
	}
	if _, ok := Icon("odd", dirs); ok {
		t.Error("an Icon outside [Desktop Entry] was read")
	}
	if _, ok := Icon("missing", dirs); ok {
		t.Error("a missing entry resolved")
	}
	if _, ok := Icon("", dirs); ok {
		t.Error("the empty id resolved")
	}
}

func TestDirsFollowXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data/home")
	t.Setenv("XDG_DATA_DIRS", "/a:/b")
	got := Dirs()
	want := []string{"/data/home/applications", "/a/applications", "/b/applications"}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dirs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
