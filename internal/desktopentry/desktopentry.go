// Package desktopentry resolves freedesktop .desktop files by id: the
// lookup gio's DesktopAppInfo::new does for the media module's
// application icons (media helpers.rs lookup_desktop_entry).
package desktopentry

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Dirs lists the applications directories in lookup order:
// $XDG_DATA_HOME/applications (default ~/.local/share), then each
// $XDG_DATA_DIRS entry (default /usr/local/share:/usr/share).
func Dirs() []string {
	var dirs []string
	if home := os.Getenv("XDG_DATA_HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "applications"))
	} else if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local/share/applications"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for dir := range strings.SplitSeq(data, ":") {
		if dir != "" {
			dirs = append(dirs, filepath.Join(dir, "applications"))
		}
	}
	return dirs
}

// Icon returns the Icon key of the desktop entry with the given id
// (with or without the .desktop suffix), searching dirs in order; false
// when no entry exists or it names no icon.
func Icon(id string, dirs []string) (string, bool) {
	if id == "" {
		return "", false
	}
	name := id
	if !strings.HasSuffix(name, ".desktop") {
		name += ".desktop"
	}
	for _, dir := range dirs {
		f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // XDG applications dirs
		if err != nil {
			continue
		}
		icon, ok := entryIcon(bufio.NewScanner(f))
		_ = f.Close()
		return icon, ok
	}
	return "", false
}

// entryIcon reads Icon= from the [Desktop Entry] group only.
func entryIcon(scanner *bufio.Scanner) (string, bool) {
	inEntry := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		if value, ok := strings.CutPrefix(line, "Icon="); ok {
			value = strings.TrimSpace(value)
			return value, value != ""
		}
	}
	return "", false
}
