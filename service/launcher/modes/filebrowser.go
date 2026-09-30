package modes

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/shlex"
	"github.com/stubbedev/wayle/service/launcher"
)

// maxEntries caps the collected entries: a recursive walk of a home
// directory is unbounded otherwise.
const maxEntries = 50_000

// FileSort is the file browser's ordering.
type FileSort uint8

// File orderings; the time orders are newest first.
const (
	FileSortName FileSort = iota
	FileSortMtime
	FileSortAtime
	FileSortCtime
)

// FileBrowserConfig is the file browser's knobs.
type FileBrowserConfig struct {
	// Directory is the start directory; empty is $HOME.
	Directory        string
	Sorting          FileSort
	DirectoriesFirst bool
	ShowHidden       bool
	// Command opens a picked file; empty is xdg-open.
	Command string
	// Recursive lists every file below (recursivebrowser).
	Recursive bool
}

// DefaultFileBrowserConfig is FileBrowserConfig::default.
func DefaultFileBrowserConfig() FileBrowserConfig {
	return FileBrowserConfig{DirectoriesFirst: true}
}

type fileEntryKind uint8

const (
	entryParent fileEntryKind = iota
	entryDir
	entryFile
)

type fileEntry struct {
	kind fileEntryKind
	path string
}

// FileBrowser is filebrowser and recursivebrowser (filebrowser.rs).
type FileBrowser struct {
	cfg     FileBrowserConfig
	current string
	entries []fileEntry
}

// NewFileBrowser roots the browser at the configured directory.
func NewFileBrowser(cfg FileBrowserConfig) *FileBrowser {
	current := os.Getenv("HOME")
	if cfg.Directory != "" {
		current = expandHome(cfg.Directory)
	}
	return &FileBrowser{cfg: cfg, current: current}
}

// Name is filebrowser or recursivebrowser.
func (f *FileBrowser) Name() string {
	if f.cfg.Recursive {
		return "recursivebrowser"
	}
	return "filebrowser"
}

type listed struct {
	path  string
	isDir bool
}

func (f *FileBrowser) state() launcher.ModeState {
	var entries []listed
	if f.cfg.Recursive {
		entries = walkFiles(f.current, f.cfg.ShowHidden)
	} else {
		entries = listDir(f.current, f.cfg.ShowHidden)
	}
	sortEntries(entries, f.cfg.Sorting, f.cfg.DirectoriesFirst)
	f.entries = f.entries[:0]
	items := make([]launcher.Item, 0, len(entries)+1)
	if parent := filepath.Dir(f.current); !f.cfg.Recursive && parent != f.current {
		f.entries = append(f.entries, fileEntry{kind: entryParent, path: parent})
		items = append(items, launcher.Item{Display: "..", MatchText: "..", Icon: launcher.IconName("folder-symbolic")})
	}
	for _, e := range entries {
		items = append(items, entryItem(e, f.current, f.cfg.Recursive))
		kind := entryFile
		if e.isDir {
			kind = entryDir
		}
		f.entries = append(f.entries, fileEntry{kind: kind, path: e.path})
	}
	return launcher.ModeState{Items: items, Prompt: displayDir(f.current)}
}

func (f *FileBrowser) open(path string) {
	opener := strings.TrimSpace(f.cfg.Command)
	if opener == "" {
		opener = "xdg-open"
	}
	launcher.RunShell(opener + " " + shlex.MustQuote(path))
}

// Load lists the current directory.
func (f *FileBrowser) Load(context.Context) launcher.ModeState { return f.state() }

// Activate enters a directory, opens a file, or follows a typed path.
func (f *FileBrowser) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	if i, ok := target.Row(); ok && int(i) < len(f.entries) {
		e := f.entries[i]
		if e.kind == entryFile {
			f.open(e.path)
			return launcher.ActionClose{}
		}
		f.current = e.path
		return launcher.ActionReload{State: f.state()}
	}
	custom, ok := kind.(launcher.ActivateCustom)
	if !ok {
		return launcher.ActionNothing{}
	}
	path := expandHome(strings.TrimSpace(custom.Text))
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return launcher.ActionNothing{}
	case st.IsDir():
		f.current = path
		return launcher.ActionReload{State: f.state()}
	case st.Mode().IsRegular():
		f.open(path)
		return launcher.ActionClose{}
	}
	return launcher.ActionNothing{}
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		return os.Getenv("HOME") + rest
	}
	return path
}

func displayDir(path string) string {
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(path, home) {
		return strings.Replace(path, home, "~", 1)
	}
	return path
}

func isHidden(path string) bool { return strings.HasPrefix(filepath.Base(path), ".") }

func listDir(dir string, showHidden bool) []listed {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []listed
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if !showHidden && isHidden(path) {
			continue
		}
		if len(out) >= maxEntries {
			break
		}
		st, err := os.Stat(path)
		out = append(out, listed{path: path, isDir: err == nil && st.IsDir()})
	}
	return out
}

// walkFiles is a breadth-first recursive walk, symlinks skipped (no
// cycles), capped at maxEntries files.
func walkFiles(root string, showHidden bool) []listed {
	var out []listed
	queue := []string{root}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if !showHidden && isHidden(path) {
				continue
			}
			if e.Type()&os.ModeSymlink != 0 {
				continue
			}
			if e.IsDir() {
				queue = append(queue, path)
			} else {
				out = append(out, listed{path: path})
			}
			if len(out) >= maxEntries {
				log.Printf("launcher: recursive browser hit the %d entry cap; listing truncated", maxEntries)
				return out
			}
		}
	}
	return out
}

// fileTime is the sort key for the time orders; unreadable is the
// epoch. Creation time comes from statx's birth time.
func fileTime(path string, sorting FileSort) time.Time {
	switch sorting {
	case FileSortMtime:
		if st, err := os.Stat(path); err == nil {
			return st.ModTime()
		}
	case FileSortAtime, FileSortCtime:
		var sx unix.Statx_t
		if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_ATIME|unix.STATX_BTIME, &sx); err == nil {
			ts := sx.Atime
			if sorting == FileSortCtime {
				if sx.Mask&unix.STATX_BTIME == 0 {
					return time.Unix(0, 0)
				}
				ts = sx.Btime
			}
			return time.Unix(ts.Sec, int64(ts.Nsec))
		}
	}
	return time.Unix(0, 0)
}

func sortEntries(entries []listed, sorting FileSort, dirsFirst bool) {
	slices.SortStableFunc(entries, func(a, b listed) int {
		if dirsFirst && a.isDir != b.isDir {
			if a.isDir {
				return -1
			}
			return 1
		}
		if sorting == FileSortName {
			return strings.Compare(asciiLower(filepath.Base(a.path)), asciiLower(filepath.Base(b.path)))
		}
		return fileTime(b.path, sorting).Compare(fileTime(a.path, sorting))
	})
}

// asciiLower folds ASCII case only, as OsStr::to_ascii_lowercase.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// entryItem is a row: a folder icon for a directory, a thumbnail
// request (generated lazily, only for the rows drawn) for a file.
func entryItem(e listed, base string, recursive bool) launcher.Item {
	name := filepath.Base(e.path)
	if recursive {
		if rel, err := filepath.Rel(base, e.path); err == nil {
			name = rel
		}
	}
	var icon launcher.Icon = launcher.IconName("folder-symbolic")
	if !e.isDir {
		icon = launcher.IconThumbnail{Path: e.path, Fallback: launcher.MimeIcon(e.path)}
	}
	return launcher.Item{Display: name, MatchText: name, Icon: icon}
}
