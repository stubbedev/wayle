package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("HOME", "/home/user")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != filepath.Join("/xdg", "wayle") {
		t.Fatalf("Dir = %q, want /xdg/wayle", dir)
	}
}

func TestDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/user")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != filepath.Join("/home/user", ".config", "wayle") {
		t.Fatalf("Dir = %q, want /home/user/.config/wayle", dir)
	}
}

func TestDirWithoutHomeOrXDGIsAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Fatal("Dir with neither XDG_CONFIG_HOME nor HOME set: want error, got nil")
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoverMainPrefersYAML(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.yaml": "a = 1",
		"config.yml":  "b = 2",
		"config.toml": "c = 3",
	})
	if got := DiscoverMain(dir); filepath.Base(got) != "config.yaml" {
		t.Fatalf("DiscoverMain = %q, want config.yaml", got)
	}
}

func TestDiscoverMainPrefersYMLSecond(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.yml":  "b = 2",
		"config.toml": "c = 3",
	})
	if got := DiscoverMain(dir); filepath.Base(got) != "config.yml" {
		t.Fatalf("DiscoverMain = %q, want config.yml", got)
	}
}

func TestDiscoverMainDefaultsToTOML(t *testing.T) {
	dir := t.TempDir()
	got := DiscoverMain(dir)
	if filepath.Base(got) != "config.toml" {
		t.Fatalf("DiscoverMain = %q, want config.toml", got)
	}
}
