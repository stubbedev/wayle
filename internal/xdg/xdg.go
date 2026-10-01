// Package xdg resolves wayle's XDG base directories.
package xdg

import (
	"os"
	"path/filepath"
)

// StateDir is wayle's state directory, $XDG_STATE_HOME/wayle or
// ~/.local/state/wayle (wayle-core paths.rs state_dir); false when
// neither variable is set.
func StateDir() (string, bool) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "wayle"), true
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", false
	}
	return filepath.Join(home, ".local/state/wayle"), true
}

// CacheDir is wayle's cache directory, $XDG_CACHE_HOME/wayle or
// ~/.cache/wayle; false when neither variable is set.
func CacheDir() (string, bool) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "wayle"), true
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", false
	}
	return filepath.Join(home, ".cache/wayle"), true
}
