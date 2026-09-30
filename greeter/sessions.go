package greeter

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SessionKind is which discovery directory advertised a session.
type SessionKind uint8

// Session kinds.
const (
	// Wayland sessions (wayland-sessions/*.desktop) start directly.
	Wayland SessionKind = iota
	// X11 sessions (xsessions/*.desktop) start through
	// `startx /usr/bin/env`: greetd runs no X server, startx provides
	// one (the tuigreet approach).
	X11
)

// Session is one selectable session (session.rs Session).
type Session struct {
	// ID is the .desktop stem, suffixed -x11 for X11 sessions; the
	// remembered choice.
	ID string
	// Name is Name= (else the id), suffixed " (X11)" for X11.
	Name string
	// Exec is the argv from Exec=, field codes dropped (and the startx
	// wrapper prepended for X11).
	Exec []string
}

// Discover reads kind sessions from every *.desktop under dirs:
// Hidden/NoDisplay entries and files without a usable Exec are
// skipped, and an id seen in an earlier dir wins (XDG precedence).
func Discover(dirs []string, kind SessionKind) []Session {
	var out []Session
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing sessions dir is normal
		}
		for _, e := range entries {
			name := e.Name()
			if filepath.Ext(name) != ".desktop" {
				continue
			}
			id := strings.TrimSuffix(name, ".desktop")
			if kind == X11 {
				id += "-x11"
			}
			if seen[id] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // system session files
			if err != nil {
				continue
			}
			if s, ok := parseDesktop(string(data), id); ok {
				seen[id] = true
				out = append(out, applyKind(s, kind))
			}
		}
	}
	return out
}

// SortSessions orders the merged list by lowercased name, as main.rs
// does after merging the kinds.
func SortSessions(sessions []Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		return strings.ToLower(sessions[i].Name) < strings.ToLower(sessions[j].Name)
	})
}

func applyKind(s Session, kind SessionKind) Session {
	if kind == X11 {
		s.Name += " (X11)"
		s.Exec = append([]string{"startx", "/usr/bin/env"}, s.Exec...)
	}
	return s
}

// parseDesktop reads the [Desktop Entry] group; false when hidden or
// without a usable Exec.
func parseDesktop(text, id string) (Session, bool) {
	var name, exec string
	haveName, haveExec, hidden, inEntry := false, false, false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Name":
			if !haveName {
				name, haveName = value, true
			}
		case "Exec":
			exec, haveExec = value, true
		case "Hidden", "NoDisplay":
			if value == "true" {
				hidden = true
			}
		}
	}
	if hidden || !haveExec {
		return Session{}, false
	}
	argv := parseExec(exec)
	if len(argv) == 0 {
		return Session{}, false
	}
	if name == "" {
		name = id
	}
	return Session{ID: id, Name: name, Exec: argv}, true
}

// parseExec splits an Exec= line on whitespace, dropping field codes
// (%f, %U, ...). Session Exec lines are near-universally simple, so
// full desktop-entry quoting is not handled (as in session.rs).
func parseExec(exec string) []string {
	var out []string
	for _, tok := range strings.Fields(exec) {
		if len(tok) == 2 && tok[0] == '%' {
			continue
		}
		out = append(out, tok)
	}
	return out
}
