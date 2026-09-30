// Package greeter is wayle-greeter: a greetd greeter sharing the lock
// screen's credential prompt. It runs as the single client of a kiosk
// compositor (cage) that greetd spawns, drives the login over the
// greetd IPC socket, and exits when greetd starts the chosen session.
package greeter

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// The discovery defaults sddm and gdm read, highest precedence first.
var (
	defaultSessionDirs  = []string{"/usr/local/share/wayland-sessions", "/usr/share/wayland-sessions"}
	defaultXSessionDirs = []string{"/usr/local/share/xsessions", "/usr/share/xsessions"}
)

// defaultStatePath is the last resort for the remembered session when
// neither --state, XDG_STATE_HOME, nor HOME yields a location.
const defaultStatePath = "/var/lib/wayle-greeter/last-session"

// Options are the parsed command line (config.rs Options).
type Options struct {
	// ConfigPath is the wayle config the greeter themes from.
	ConfigPath string
	// SessionDirs and XSessionDirs are scanned for session files.
	SessionDirs, XSessionDirs []string
	// StatePath remembers the last session id; the last username sits
	// in a last-user file beside it.
	StatePath string
	// Command is the explicit fallback session from `-- <argv>`.
	Command []string
	// Env holds extra KEY=value entries for the session.
	Env []string
}

// UsageError is a malformed command line; the greeter exits 2 with it.
type UsageError struct{ detail string }

func (e UsageError) Error() string {
	return e.detail + "\nusage: wayle-greeter [--config PATH] [--sessions DIR]... [--xsessions DIR]... " +
		"[--state PATH] [--env KEY=VAL]... [-- <session argv...>]"
}

// ParseOptions parses the arguments after the program name. Everything
// after "--" is the fallback session argv; the repeatable --sessions
// and --xsessions replace their defaults once given.
func ParseOptions(args []string) (Options, error) {
	o := Options{ConfigPath: config.GreeterConfigPath}
	value := func(i *int, flag, need string) (string, error) {
		if *i+1 >= len(args) {
			return "", UsageError{flag + " requires " + need}
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		var err error
		var v string
		switch args[i] {
		case "--":
			o.Command = append(o.Command, args[i+1:]...)
			i = len(args)
		case "--config":
			v, err = value(&i, "--config", "a path")
			o.ConfigPath = v
		case "--sessions":
			v, err = value(&i, "--sessions", "a DIR")
			o.SessionDirs = append(o.SessionDirs, v)
		case "--xsessions":
			v, err = value(&i, "--xsessions", "a DIR")
			o.XSessionDirs = append(o.XSessionDirs, v)
		case "--state":
			v, err = value(&i, "--state", "a path")
			o.StatePath = v
		case "--env":
			v, err = value(&i, "--env", "KEY=VAL")
			if err == nil && !strings.Contains(v, "=") {
				err = UsageError{"--env value must be KEY=VAL"}
			}
			o.Env = append(o.Env, v)
		default:
			err = UsageError{"unexpected argument: " + args[i]}
		}
		if err != nil {
			return Options{}, err
		}
	}
	if len(o.SessionDirs) == 0 {
		o.SessionDirs = append([]string(nil), defaultSessionDirs...)
	}
	if len(o.XSessionDirs) == 0 {
		o.XSessionDirs = append([]string(nil), defaultXSessionDirs...)
	}
	if o.StatePath == "" {
		o.StatePath = defaultStateFile()
	}
	return o, nil
}

// defaultStateFile: $XDG_STATE_HOME/wayle-greeter/last-session, else
// $HOME/.local/state/..., else /var/lib/wayle-greeter/last-session.
// greetd's greeter user has an unusual HOME, so an explicit --state is
// the robust choice.
func defaultStateFile() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "wayle-greeter", "last-session")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "wayle-greeter", "last-session")
	}
	return defaultStatePath
}

// LastUserPath is the last-user file beside the session state file.
func LastUserPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "last-user")
}

// LoadLast reads a remembered value; "" when unset or unreadable.
func LoadLast(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // the greeter's own state file
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SaveLast persists a remembered value, creating its directory.
func SaveLast(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the greeter's state dir is not secret
		return err
	}
	return os.WriteFile(path, []byte(value), 0o644) //nolint:gosec // a session id and a username, not secret
}


