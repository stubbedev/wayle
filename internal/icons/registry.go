package icons

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/fswatch"
)

// This file ports registry.rs minus the GTK IconTheme wiring: the icon
// directory layout, index.theme, and the system icon paths.

const systemIconsPath = "/usr/share/wayle/icons"

const indexThemeContent = `[Icon Theme]
Name=Wayle Icons
Comment=Icons installed by Wayle
Directories=hicolor/scalable/actions

[hicolor/scalable/actions]
Size=48
MinSize=16
MaxSize=512
Type=Scalable
`

// Registry locates wayle's icon theme directory.
type Registry struct{ base string }

// NewRegistry uses the default directory.
func NewRegistry() (*Registry, error) {
	base, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Registry{base: base}, nil
}

// RegistryAt uses base as the theme root.
func RegistryAt(base string) *Registry { return &Registry{base: base} }

// DefaultPath is $XDG_DATA_HOME/wayle/icons or ~/.local/share/wayle/icons.
// As in Rust, a set-but-empty XDG_DATA_HOME is taken as given.
func DefaultPath() (string, error) {
	dataHome, ok := os.LookupEnv("XDG_DATA_HOME")
	if !ok {
		home, ok := os.LookupEnv("HOME")
		if !ok {
			return "", ErrHomeNotSet
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "wayle", "icons"), nil
}

// BasePath is the theme root.
func (r *Registry) BasePath() string { return r.base }

// IconsDir is <base>/hicolor/scalable/actions.
func (r *Registry) IconsDir() string {
	return filepath.Join(r.base, "hicolor", "scalable", "actions")
}

// EnsureSetup creates the directory tree and index.theme.
func (r *Registry) EnsureSetup() error {
	dir := r.IconsDir()
	if !exists(dir) {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return &DirectoryError{Path: dir, Err: err}
		}
	}
	index := filepath.Join(r.base, "index.theme")
	if !exists(index) {
		if err := os.WriteFile(index, []byte(indexThemeContent), filePerm); err != nil {
			return &WriteError{Path: index, Err: err}
		}
	}
	return nil
}

// IsValid reports the icons dir and index.theme both present.
func (r *Registry) IsValid() bool {
	_, e1 := os.Stat(r.IconsDir())
	_, e2 := os.Stat(filepath.Join(r.base, "index.theme"))
	return e1 == nil && e2 == nil
}

// ThemeSearchPaths is refresh_gtk_theme's icon search path: the user
// root first, then each system root, then the rest of existing in its
// order (without those roots, so a refresh never duplicates them).
func (r *Registry) ThemeSearchPaths(existing []string) []string {
	paths := []string{r.base}
	for _, p := range SystemIconPaths() {
		if !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	for _, p := range existing {
		if !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	return paths
}

// iconChanges are the icons directory events that change what resolves:
// icons written, removed, or renamed in or out (an atomic write lands
// as a rename).
const iconChanges = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO

// Watch runs onChange after each burst of icons appearing in or leaving
// the icons directory (start_watcher), on the watcher's goroutine,
// until stop. The directory must exist (EnsureSetup).
func (r *Registry) Watch(onChange func()) (stop func(), err error) {
	w, err := fswatch.New(iconChanges, false)
	if err != nil {
		return nil, err
	}
	if err := w.Add(r.IconsDir()); err != nil {
		_ = w.Close()
		return nil, err
	}
	go func() {
		for range w.Changed() {
			onChange()
		}
	}()
	return func() { _ = w.Close() }, nil
}

// SystemIconPaths are the existing <XDG_DATA_DIRS>/wayle/icons roots,
// then /usr/share/wayle/icons.
func SystemIconPaths() []string {
	var out []string
	for d := range strings.SplitSeq(os.Getenv("XDG_DATA_DIRS"), ":") {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "wayle/icons")
		if exists(p) {
			out = append(out, p)
		}
	}
	if exists(systemIconsPath) && !slices.Contains(out, systemIconsPath) {
		out = append(out, systemIconsPath)
	}
	return out
}

// exists is Path::exists: any stat failure reads as absent.
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
