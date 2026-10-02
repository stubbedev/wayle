package desktopentry

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The mimeapps.list groups (the XDG mime-apps spec).
const (
	groupDefault = "Default Applications"
	groupAdded   = "Added Associations"
	groupRemoved = "Removed Associations"
)

// MimeTypes is the entry's MimeType list.
func (a App) MimeTypes() []string { return a.file.List(DesktopGroup, "MimeType") }

// MimeAppsLists is the mimeapps.list search order, highest priority
// first: in $XDG_CONFIG_HOME, each $XDG_CONFIG_DIRS entry, then each
// applications directory, the current desktops' $desktop-mimeapps.list
// before the plain mimeapps.list.
func MimeAppsLists() []string {
	var dirs []string
	if home := os.Getenv("XDG_CONFIG_HOME"); home != "" {
		dirs = append(dirs, home)
	} else if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".config"))
	}
	cfg := os.Getenv("XDG_CONFIG_DIRS")
	if cfg == "" {
		cfg = "/etc/xdg"
	}
	for d := range strings.SplitSeq(cfg, ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs, ApplicationDirs()...)
	var lists []string
	for _, d := range dirs {
		for _, desktop := range CurrentDesktops() {
			lists = append(lists, filepath.Join(d, strings.ToLower(desktop)+"-mimeapps.list"))
		}
		lists = append(lists, filepath.Join(d, "mimeapps.list"))
	}
	return lists
}

// associations is what the lists say about one type.
type associations struct {
	defaults, added []string
	removed         map[string]bool
}

// readAssociations walks the lists, highest priority first. A removal
// hides an association from its own list and every lower one, so it
// is applied as the lists are read.
func readAssociations(lists []string, mimeType string) associations {
	as := associations{removed: map[string]bool{}}
	for _, path := range lists {
		kf, err := LoadKeyFile(path)
		if err != nil {
			continue
		}
		for _, id := range kf.List(groupRemoved, mimeType) {
			as.removed[id] = true
		}
		for _, id := range kf.List(groupDefault, mimeType) {
			if !as.removed[id] && !slices.Contains(as.defaults, id) {
				as.defaults = append(as.defaults, id)
			}
		}
		for _, id := range kf.List(groupAdded, mimeType) {
			if !as.removed[id] && !slices.Contains(as.added, id) {
				as.added = append(as.added, id)
			}
		}
	}
	return as
}

// RecommendedFor is g_app_info_get_recommended_for_type: the installed
// apps for mimeType - the default first, then the added associations,
// then every app declaring the type - without the removed ones.
func RecommendedFor(dirs, lists []string, mimeType string) []App {
	as := readAssociations(lists, mimeType)
	all := AllApps(dirs)
	byID := make(map[string]App, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	var out []App
	seen := map[string]bool{}
	add := func(id string) {
		if a, ok := byID[id]; ok && !seen[id] && !as.removed[id] {
			seen[id] = true
			out = append(out, a)
		}
	}
	// The default counts only when installed, as gio skips a stale one.
	for _, id := range as.defaults {
		if _, ok := byID[id]; ok {
			add(id)
			break
		}
	}
	for _, id := range as.added {
		add(id)
	}
	for _, a := range all {
		if slices.Contains(a.MimeTypes(), mimeType) {
			add(a.ID)
		}
	}
	return out
}

// SetDefault is g_app_info_set_as_default_for_type: id becomes the
// default for mimeType in the user's mimeapps.list and the first of
// its added associations; the rest of the file is kept.
func SetDefault(id, mimeType string) error {
	lists := MimeAppsLists()
	var path string
	for _, l := range lists {
		if filepath.Base(l) == "mimeapps.list" {
			path = l
			break
		}
	}
	if path == "" {
		return os.ErrNotExist
	}
	data, err := os.ReadFile(path) //nolint:gosec // the user's own mimeapps.list
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := splitLines(data)
	lines = setListKey(lines, groupDefault, mimeType, []string{id})
	var added []string
	if kf, err := ParseKeyFile(data); err == nil {
		added = kf.List(groupAdded, mimeType)
	}
	added = append([]string{id}, slices.DeleteFunc(added, func(s string) bool { return s == id })...)
	lines = setListKey(lines, groupAdded, mimeType, added)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // ~/.config, readable as any config dir
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644) //nolint:gosec // mimeapps.list is world-readable config
}

func splitLines(data []byte) []string {
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// setListKey sets key=v1;v2; in group, adding the group (at the end) or
// the key (at the end of its group) when missing.
func setListKey(lines []string, group, key string, values []string) []string {
	entry := key + "=" + strings.Join(values, ";") + ";"
	start, end := -1, len(lines)
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if start >= 0 {
				end = i
				break
			}
			if trimmed == "["+group+"]" {
				start = i
			}
		}
	}
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, "["+group+"]", entry)
	}
	for i := start + 1; i < end; i++ {
		if k, _, ok := strings.Cut(lines[i], "="); ok && strings.TrimSpace(k) == key {
			lines[i] = entry
			return lines
		}
	}
	// After the group's last non-blank line.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	return slices.Insert(lines, at, entry)
}
