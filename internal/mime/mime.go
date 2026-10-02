// Package mime guesses a file's mimetype from its name through the
// system's shared-mime-info database, and names the generic icon for
// it: what gio's g_content_type_guess (with no data) and
// g_content_type_get_generic_icon_name answer, in pure Go.
package mime

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Unknown is what a name no glob claims guesses as.
const Unknown = "application/octet-stream"

// glob is one globs2 line.
type glob struct {
	weight        int
	mime          string
	pattern       string
	caseSensitive bool
}

// Database is a loaded shared-mime-info glob table plus the generic
// icon map.
type Database struct {
	literal map[string]glob
	globs   []glob
	icons   map[string]string
	// aliases maps an alias to its canonical type; parents a type to
	// the types it subclasses.
	aliases map[string]string
	parents map[string][]string
}

var (
	systemOnce sync.Once
	system     *Database
)

// System loads the database from the XDG data directories once:
// $XDG_DATA_HOME (or ~/.local/share) first, then $XDG_DATA_DIRS (or
// /usr/local/share:/usr/share). A missing database guesses Unknown for
// everything.
func System() *Database {
	systemOnce.Do(func() { system = Load(DataDirs()) })
	return system
}

// DataDirs is the XDG data directory search order.
func DataDirs() []string {
	var dirs []string
	if home := os.Getenv("XDG_DATA_HOME"); home != "" {
		dirs = append(dirs, home)
	} else if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for d := range strings.SplitSeq(data, ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// Load reads mime/globs2, generic-icons, aliases and subclasses from each data dir; an
// earlier directory's entry for the same pattern wins.
func Load(dirs []string) *Database {
	db := &Database{
		literal: map[string]glob{}, icons: map[string]string{},
		aliases: map[string]string{}, parents: map[string][]string{},
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		db.readGlobs(filepath.Join(dir, "mime", "globs2"), seen)
		db.readIcons(filepath.Join(dir, "mime", "generic-icons"))
		db.readPairs(filepath.Join(dir, "mime", "aliases"), func(alias, canonical string) {
			if _, ok := db.aliases[alias]; !ok {
				db.aliases[alias] = canonical
			}
		})
		db.readPairs(filepath.Join(dir, "mime", "subclasses"), func(child, parent string) {
			db.parents[child] = append(db.parents[child], parent)
		})
	}
	return db
}

func (db *Database) readGlobs(path string, seen map[string]bool) {
	f, err := os.Open(path) //nolint:gosec // a system data file
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}
		weight, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		g := glob{weight: weight, mime: parts[1], pattern: parts[2]}
		if len(parts) > 3 {
			for flag := range strings.SplitSeq(parts[3], ",") {
				if flag == "cs" {
					g.caseSensitive = true
				}
			}
		}
		key := g.pattern + "\x00" + g.mime
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.ContainsAny(g.pattern, "*?[") {
			if _, ok := db.literal[g.pattern]; !ok {
				db.literal[g.pattern] = g
			}
			continue
		}
		if !g.caseSensitive {
			g.pattern = strings.ToLower(g.pattern)
		}
		db.globs = append(db.globs, g)
	}
}

func (db *Database) readIcons(path string) {
	f, err := os.Open(path) //nolint:gosec // a system data file
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		mime, icon, ok := strings.Cut(sc.Text(), ":")
		if !ok || strings.HasPrefix(mime, "#") {
			continue
		}
		if _, exists := db.icons[mime]; !exists {
			db.icons[mime] = icon
		}
	}
}

// TypeByName guesses the mimetype of a file from its base name: a
// literal name first, then the heaviest matching glob (the longest on a
// tie), else Unknown.
func (db *Database) TypeByName(path string) string {
	name := filepath.Base(path)
	if g, ok := db.literal[name]; ok {
		return g.mime
	}
	lower := strings.ToLower(name)
	var best glob
	found := false
	for _, g := range db.globs {
		candidate := lower
		if g.caseSensitive {
			candidate = name
		}
		if ok, _ := filepath.Match(g.pattern, candidate); !ok {
			continue
		}
		if !found || g.weight > best.weight || (g.weight == best.weight && len(g.pattern) > len(best.pattern)) {
			best, found = g, true
		}
	}
	if !found {
		return Unknown
	}
	return best.mime
}

// GenericIcon names the generic icon for a mimetype: the database's
// generic-icons entry, else `<media>-x-generic` (image-x-generic,
// text-x-generic), which is gio's rule.
func (db *Database) GenericIcon(mimeType string) string {
	if icon, ok := db.icons[mimeType]; ok {
		return icon
	}
	media, _, ok := strings.Cut(mimeType, "/")
	if !ok || media == "" {
		return "text-x-generic-symbolic"
	}
	return media + "-x-generic"
}

// readPairs feeds each "a b" line of a two-column mime file to add.
func (db *Database) readPairs(path string, add func(a, b string)) {
	f, err := os.Open(path) //nolint:gosec // a system data file
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		a, b, ok := strings.Cut(sc.Text(), " ")
		if ok && !strings.HasPrefix(a, "#") {
			add(a, b)
		}
	}
}

// canonical resolves an alias.
func (db *Database) canonical(mimeType string) string {
	if c, ok := db.aliases[mimeType]; ok {
		return c
	}
	return mimeType
}

// IsA reports whether mimeType is base or a subtype of it, with
// aliases resolved: g_content_type_is_a, shared-mime-info's
// is_subclass. Beyond the subclasses file every text/* is a
// text/plain and every non-inode type an application/octet-stream.
func (db *Database) IsA(mimeType, base string) bool {
	mimeType, base = db.canonical(mimeType), db.canonical(base)
	seen := map[string]bool{}
	var walk func(t string) bool
	walk = func(t string) bool {
		if t == base {
			return true
		}
		if seen[t] {
			return false
		}
		seen[t] = true
		if base == "text/plain" && strings.HasPrefix(t, "text/") {
			return true
		}
		if base == "application/octet-stream" && !strings.HasPrefix(t, "inode/") {
			return true
		}
		for _, p := range db.parents[t] {
			if walk(db.canonical(p)) {
				return true
			}
		}
		return false
	}
	return walk(mimeType)
}
