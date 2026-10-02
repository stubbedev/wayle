package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserDir(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if _, ok := UserDir("DOCUMENTS"); ok {
		t.Error("no user-dirs.dirs resolved a directory")
	}
	body := "# written by xdg-user-dirs-update\n" +
		"XDG_DOCUMENTS_DIR=\"$HOME/Docs\"\n" +
		"XDG_PICTURES_DIR=\"/srv/pics/\"\n" +
		"XDG_MUSIC_DIR=relative-unquoted\n" +
		"XDG_VIDEOS_DIR=\"Videos\"\n"
	if err := os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"DOCUMENTS": "/home/u/Docs", "PICTURES": "/srv/pics"} {
		if got, ok := UserDir(key); !ok || got != want {
			t.Errorf("UserDir(%s) = %q %v, want %q", key, got, ok, want)
		}
	}
	for _, key := range []string{"MUSIC", "VIDEOS", "DOWNLOAD"} {
		if got, ok := UserDir(key); ok {
			t.Errorf("UserDir(%s) = %q, want none (unquoted, relative or absent)", key, got)
		}
	}
}
