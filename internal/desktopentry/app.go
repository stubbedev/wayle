package desktopentry

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/shlex"
)

// App is one application desktop entry, the GDesktopAppInfo a gio
// app list yields.
type App struct {
	// ID is the desktop id: the path under applications/ with '/' as
	// '-' (org.gnome.Nautilus.desktop, kde4-dolphin.desktop).
	ID string
	// Path is the file the entry was read from.
	Path string
	file *KeyFile
}

// Link is a Type=Link entry, which gio's app list skips and rofi shows.
type Link struct {
	ID   string
	Name string
	URL  string
	Icon string
}

// ApplicationDirs is the XDG applications search order:
// $XDG_DATA_HOME/applications (or ~/.local/share/applications), then
// each $XDG_DATA_DIRS entry (or /usr/local/share:/usr/share).
func ApplicationDirs() []string {
	var dirs []string
	if home := os.Getenv("XDG_DATA_HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "applications"))
	} else if home := os.Getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "applications"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for d := range strings.SplitSeq(data, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return dirs
}

// entryFile is one .desktop file under an applications directory.
type entryFile struct {
	id, path string
}

// walk lists the .desktop files of the application dirs, the earlier
// dir winning a desktop id.
func walk(dirs []string) []entryFile {
	seen := map[string]bool{}
	var out []entryFile
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".desktop" {
				return nil //nolint:nilerr // an unreadable subtree is skipped, as gio does
			}
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				return nil //nolint:nilerr // cannot happen under WalkDir
			}
			id := strings.ReplaceAll(rel, string(filepath.Separator), "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			out = append(out, entryFile{id: id, path: path})
			return nil
		})
	}
	return out
}

// AllApps is g_app_info_get_all: every loadable Type=Application
// entry, one per desktop id. An entry is not loadable when it is
// Hidden, when its TryExec or its Exec program is not installed, or
// when its Exec does not parse - gio's load_from_keyfile rules, so a
// remnant of an uninstalled package does not show.
func AllApps(dirs []string) []App {
	var apps []App
	for _, f := range walk(dirs) {
		if app, ok := LoadApp(f.id, f.path); ok {
			apps = append(apps, app)
		}
	}
	return apps
}

// FindApp looks one desktop id up in the application dirs.
func FindApp(dirs []string, id string) (App, bool) {
	for _, f := range walk(dirs) {
		if f.id == id {
			return LoadApp(f.id, f.path)
		}
	}
	return App{}, false
}

// LoadApp reads one entry, applying gio's load rules (see AllApps).
func LoadApp(id, path string) (App, bool) {
	kf, err := LoadKeyFile(path)
	if err != nil || !kf.HasGroup(DesktopGroup) {
		return App{}, false
	}
	if t, _ := kf.String(DesktopGroup, "Type"); t != "Application" {
		return App{}, false
	}
	if kf.Bool(DesktopGroup, "Hidden") {
		return App{}, false
	}
	workdir, _ := kf.String(DesktopGroup, "Path")
	if try, _ := kf.String(DesktopGroup, "TryExec"); try != "" && !findProgram(try, workdir) {
		return App{}, false
	}
	if execLine, _ := kf.String(DesktopGroup, "Exec"); execLine != "" {
		argv, ok := shlex.Split(execLine)
		if !ok || len(argv) == 0 || !findProgram(argv[0], workdir) {
			return App{}, false
		}
	}
	return App{ID: id, Path: path, file: kf}, true
}

// findProgram is g_find_program_for_path: an absolute or relative
// path is checked as is (relative to workdir), a bare name on $PATH.
func findProgram(program, workdir string) bool {
	if strings.ContainsRune(program, '/') {
		if !filepath.IsAbs(program) && workdir != "" {
			program = filepath.Join(workdir, program)
		}
		return executable(program)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if executable(filepath.Join(dir, program)) {
			return true
		}
	}
	return false
}

func executable(path string) bool {
	st, err := os.Stat(path) //nolint:gosec // a program lookup, as g_find_program_for_path
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// DisplayName is g_app_info_get_display_name: X-GNOME-FullName, else
// Name, localized.
func (a App) DisplayName() string {
	if v, ok := a.file.LocaleString(DesktopGroup, "X-GNOME-FullName"); ok && v != "" {
		return v
	}
	v, _ := a.file.LocaleString(DesktopGroup, "Name")
	return v
}

// GenericName is the localized GenericName.
func (a App) GenericName() string {
	v, _ := a.file.LocaleString(DesktopGroup, "GenericName")
	return v
}

// Comment is the localized Comment (gio's description).
func (a App) Comment() string {
	v, _ := a.file.LocaleString(DesktopGroup, "Comment")
	return v
}

// Exec is the Exec line as written (gio's commandline).
func (a App) Exec() string {
	v, _ := a.file.String(DesktopGroup, "Exec")
	return v
}

// Categories is the raw Categories string.
func (a App) Categories() string {
	v, _ := a.file.String(DesktopGroup, "Categories")
	return v
}

// Keywords is the localized Keywords list.
func (a App) Keywords() []string { return a.file.LocaleList(DesktopGroup, "Keywords") }

// Icon is the Icon key: a theme name or an absolute path; empty for
// none.
func (a App) Icon() string {
	v, _ := a.file.LocaleString(DesktopGroup, "Icon")
	return v
}

// Terminal reports Terminal=true.
func (a App) Terminal() bool { return a.file.Bool(DesktopGroup, "Terminal") }

// Actions is the Actions list: the ids of [Desktop Action <id>]
// groups.
func (a App) Actions() []string {
	var out []string
	for _, id := range a.file.List(DesktopGroup, "Actions") {
		if id != "" && a.file.HasGroup("Desktop Action "+id) {
			out = append(out, id)
		}
	}
	return out
}

// ActionName is an action's localized Name.
func (a App) ActionName(action string) string {
	v, _ := a.file.LocaleString("Desktop Action "+action, "Name")
	return v
}

// ShouldShow is g_app_info_should_show: not NoDisplay, and shown in
// the current desktop per OnlyShowIn/NotShowIn against
// $XDG_CURRENT_DESKTOP.
func (a App) ShouldShow() bool {
	if a.file.Bool(DesktopGroup, "NoDisplay") {
		return false
	}
	return a.ShownIn(CurrentDesktops())
}

// ShownIn is g_desktop_app_info_get_show_in over explicit desktop
// names: the first desktop listed in OnlyShowIn or NotShowIn decides;
// none listed means shown unless OnlyShowIn restricts it.
func (a App) ShownIn(desktops []string) bool {
	only := a.file.List(DesktopGroup, "OnlyShowIn")
	not := a.file.List(DesktopGroup, "NotShowIn")
	for _, d := range desktops {
		if slices.Contains(only, d) {
			return true
		}
		if slices.Contains(not, d) {
			return false
		}
	}
	return len(only) == 0
}

// CurrentDesktops splits $XDG_CURRENT_DESKTOP.
func CurrentDesktops() []string {
	var out []string
	for d := range strings.SplitSeq(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// dbusActivatable reports DBusActivatable=true.
func (a App) dbusActivatable() bool { return a.file.Bool(DesktopGroup, "DBusActivatable") }

// ExpandExec is gio's expand_macro over an Exec line launched with no
// files: %f %F %u %U vanish, %i is --icon <Icon>, %c the name, %k the
// entry's path, %% a percent; the deprecated codes vanish. A token that
// was only a vanishing code is dropped.
func (a App) ExpandExec(execLine string) ([]string, error) {
	words, ok := shlex.Split(execLine)
	if !ok || len(words) == 0 {
		return nil, fmt.Errorf("desktopentry: %s: unparseable Exec %q", a.ID, execLine)
	}
	var argv []string
	for _, w := range words {
		if w == "%i" {
			if icon := a.Icon(); icon != "" {
				argv = append(argv, "--icon", icon)
			}
			continue
		}
		var b strings.Builder
		codeOnly := true
		for i := 0; i < len(w); i++ {
			if w[i] != '%' || i+1 >= len(w) {
				b.WriteByte(w[i])
				codeOnly = false
				continue
			}
			i++
			switch w[i] {
			case '%':
				b.WriteByte('%')
				codeOnly = false
			case 'c':
				b.WriteString(a.DisplayName())
				codeOnly = false
			case 'k':
				b.WriteString(a.Path)
				codeOnly = false
			case 'f', 'F', 'u', 'U', 'd', 'D', 'n', 'N', 'v', 'm', 'i':
			default:
				// An unknown code is passed through.
				b.WriteByte('%')
				b.WriteByte(w[i])
				codeOnly = false
			}
		}
		if b.Len() == 0 && codeOnly {
			continue
		}
		argv = append(argv, b.String())
	}
	return argv, nil
}

// Launch starts the application with no files (g_app_info_launch). A
// DBusActivatable entry is activated over the session bus
// (org.freedesktop.Application.Activate); if that fails, or the entry
// is not activatable, its Exec runs detached in its Path.
func (a App) Launch() error {
	if a.dbusActivatable() {
		err := a.activate("Activate", nil)
		if err == nil {
			return nil
		}
		if a.Exec() == "" {
			return err
		}
	}
	return a.spawn(a.Exec())
}

// LaunchAction runs a desktop action (g_desktop_app_info_launch_action):
// ActivateAction over D-Bus for an activatable entry, else the action
// group's Exec.
func (a App) LaunchAction(action string) error {
	group := "Desktop Action " + action
	if !a.file.HasGroup(group) {
		return fmt.Errorf("desktopentry: %s has no action %q", a.ID, action)
	}
	if a.dbusActivatable() {
		if err := a.activate("ActivateAction", []any{action, []dbus.Variant{}}); err == nil {
			return nil
		}
	}
	execLine, _ := a.file.String(group, "Exec")
	return a.spawn(execLine)
}

// spawn runs an expanded Exec line detached, in the entry's Path, with
// gio's GIO_LAUNCHED_DESKTOP_FILE markers.
func (a App) spawn(execLine string) error {
	argv, err := a.ExpandExec(execLine)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		return fmt.Errorf("desktopentry: %s: empty Exec", a.ID)
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // the entry's own command
	if dir, _ := a.file.String(DesktopGroup, "Path"); dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIO_LAUNCHED_DESKTOP_FILE="+a.Path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// errNotActivatable is an id with no D-Bus name form.
var errNotActivatable = errors.New("desktopentry: desktop id is not a D-Bus name")

// activate calls org.freedesktop.Application on the entry's bus name,
// the desktop id without .desktop (the D-Bus activation spec).
func (a App) activate(method string, args []any) error {
	name, ok := strings.CutSuffix(a.ID, ".desktop")
	if !ok || !strings.Contains(name, ".") {
		return errNotActivatable
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	path := dbus.ObjectPath("/" + strings.ReplaceAll(strings.ReplaceAll(name, ".", "/"), "-", "_"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	call := append(args, map[string]dbus.Variant{})
	return conn.Object(name, path).CallWithContext(ctx, "org.freedesktop.Application."+method, 0, call...).Err
}

// String returns a raw Desktop Entry key, for callers that need a key
// App does not model.
func (a App) String(key string) (string, bool) { return a.file.String(DesktopGroup, key) }

// Links lists the Type=Link entries gio's app list skips, earlier dirs
// winning a desktop id; NoDisplay and Hidden ones are left out, and an
// entry without a name or URL is not a link.
func Links(dirs []string) []Link {
	var out []Link
	for _, f := range walkFlat(dirs) {
		kf, err := LoadKeyFile(f.path)
		if err != nil {
			continue
		}
		if t, _ := kf.String(DesktopGroup, "Type"); t != "Link" {
			continue
		}
		if kf.Bool(DesktopGroup, "NoDisplay") || kf.Bool(DesktopGroup, "Hidden") {
			continue
		}
		name, ok := kf.LocaleString(DesktopGroup, "Name")
		if !ok {
			continue
		}
		url, ok := kf.String(DesktopGroup, "URL")
		if !ok {
			continue
		}
		icon, _ := kf.String(DesktopGroup, "Icon")
		out = append(out, Link{ID: f.id, Name: name, URL: url, Icon: icon})
	}
	return out
}

// walkFlat lists the top-level .desktop files of each dir (drun.rs
// collect_links reads each dir without recursing), the earlier dir
// winning a file name.
func walkFlat(dirs []string) []entryFile {
	seen := map[string]bool{}
	var out []entryFile
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".desktop" || seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			out = append(out, entryFile{id: e.Name(), path: filepath.Join(dir, e.Name())})
		}
	}
	return out
}
