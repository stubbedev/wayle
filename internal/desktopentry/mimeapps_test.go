package desktopentry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeApp installs a desktop entry whose Exec is sh, which is on PATH.
func writeApp(t *testing.T, dir, id, mimes string) {
	t.Helper()
	body := "[Desktop Entry]\nType=Application\nName=" + id + "\nExec=sh\nMimeType=" + mimes + "\n"
	if err := os.WriteFile(filepath.Join(dir, id), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ids(apps []App) []string {
	var out []string
	for _, a := range apps {
		out = append(out, a.ID)
	}
	return out
}

func TestRecommendedFor(t *testing.T) {
	apps := t.TempDir()
	writeApp(t, apps, "viewer.desktop", "image/png;")
	writeApp(t, apps, "editor.desktop", "image/png;text/plain;")
	writeApp(t, apps, "browser.desktop", "text/html;")
	writeApp(t, apps, "hidden.desktop", "image/png;")
	user, system := filepath.Join(t.TempDir(), "mimeapps.list"), filepath.Join(t.TempDir(), "mimeapps.list")
	_ = os.WriteFile(user, []byte("[Default Applications]\nimage/png=gone.desktop;editor.desktop;\n[Removed Associations]\nimage/png=hidden.desktop;\n"), 0o600)
	_ = os.WriteFile(system, []byte("[Added Associations]\nimage/png=browser.desktop;hidden.desktop;\n"), 0o600)

	got := ids(RecommendedFor([]string{apps}, []string{user, system}, "image/png"))
	// The installed default first, then the added browser, then the
	// declarers; the removed one nowhere, the uninstalled default skipped.
	if want := []string{"editor.desktop", "browser.desktop", "viewer.desktop"}; !slices.Equal(got, want) {
		t.Errorf("recommended = %v, want %v", got, want)
	}
	if got := ids(RecommendedFor([]string{apps}, nil, "text/plain")); !slices.Equal(got, []string{"editor.desktop"}) {
		t.Errorf("text/plain = %v", got)
	}
	if got := RecommendedFor([]string{apps}, nil, "video/mp4"); len(got) != 0 {
		t.Errorf("an unhandled type = %v", ids(got))
	}
}

func TestSetDefaultKeepsTheFile(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	path := filepath.Join(cfg, "mimeapps.list")
	_ = os.WriteFile(path, []byte("# mine\n[Default Applications]\ntext/plain=old.desktop;\nimage/png=keep.desktop;\n\n[Added Associations]\nimage/png=a.desktop;b.desktop;\n"), 0o600)
	if err := SetDefault("b.desktop", "image/png"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# mine\n[Default Applications]\ntext/plain=old.desktop;\nimage/png=b.desktop;\n\n[Added Associations]\nimage/png=b.desktop;a.desktop;\n"
	if string(data) != want {
		t.Errorf("mimeapps.list =\n%s\nwant\n%s", data, want)
	}
	// A fresh file gets both groups.
	_ = os.Remove(path)
	if err := SetDefault("x.desktop", "video/mp4"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if s := string(data); !strings.Contains(s, "[Default Applications]\nvideo/mp4=x.desktop;\n") || !strings.Contains(s, "[Added Associations]\nvideo/mp4=x.desktop;\n") {
		t.Errorf("fresh file =\n%s", s)
	}
}

func TestMimeAppsListsOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/c")
	t.Setenv("XDG_CONFIG_DIRS", "/etc/xdg")
	t.Setenv("XDG_DATA_HOME", "/d")
	t.Setenv("XDG_DATA_DIRS", "/usr/share")
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	got := MimeAppsLists()
	want := []string{
		"/c/hyprland-mimeapps.list", "/c/mimeapps.list",
		"/etc/xdg/hyprland-mimeapps.list", "/etc/xdg/mimeapps.list",
		"/d/applications/hyprland-mimeapps.list", "/d/applications/mimeapps.list",
		"/usr/share/applications/hyprland-mimeapps.list", "/usr/share/applications/mimeapps.list",
	}
	if !slices.Equal(got, want) {
		t.Errorf("lists = %v", got)
	}
}
