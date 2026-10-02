package xdg

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// UserDir is a user directory from user-dirs.dirs (glib's
// g_get_user_special_dir): key is the name between XDG_ and _DIR
// (DOCUMENTS, PICTURES, ...), the value's $HOME expanded; false when
// the file or the key is missing, or the value is relative.
func UserDir(key string) (string, bool) {
	home := os.Getenv("HOME")
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		if home == "" {
			return "", false
		}
		config = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(config, "user-dirs.dirs")) //nolint:gosec // the user's own config file
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	want := "XDG_" + key + "_DIR"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.TrimSpace(name) != want {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			return "", false
		}
		value = value[1 : len(value)-1]
		if rest, ok := strings.CutPrefix(value, "$HOME"); ok {
			if home == "" {
				return "", false
			}
			value = home + rest
		}
		if !filepath.IsAbs(value) {
			return "", false
		}
		return filepath.Clean(value), true
	}
	return "", false
}
