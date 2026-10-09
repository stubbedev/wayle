package greeter

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// The greeter runs pre-login, so there is no session to ask what
// cursor the user sees (cursor.rs). The last user's own dotfiles are
// read best effort: the record a wayle session leaves, then the
// compositor matching the remembered session first (Hyprland env and
// hyprctl setcursor, niri's cursor block, sway's seat xcursor_theme),
// then GTK settings.ini and ~/.icons/default/index.theme.
//
// Like GTK 4.22, gelm asks the compositor to draw the cursor
// (cursor-shape-v1) when it can; on a host without the protocol (cage)
// it draws xcursor themes client-side from XCURSOR_THEME and
// XCURSOR_SIZE, so the resolved cursor is applied by exporting those
// before connecting. The bundled-PNG fallback the Rust greeter works
// around does not exist here.

// recordedRel is where a running wayle session records its live cursor
// (RecordCursor), relative to the user's home.
const recordedRel = ".local/state/wayle/greeter-cursor"

// recordFile is the record's name inside the wayle state dir.
const recordFile = "greeter-cursor"

// Cursor is what one source found; either side may be missing.
type Cursor struct {
	Theme string // "" when unknown
	Size  uint32 // 0 when unknown
}

// or fills the missing sides from other.
func (c Cursor) or(other Cursor) Cursor {
	if c.Theme == "" {
		c.Theme = other.Theme
	}
	if c.Size == 0 {
		c.Size = other.Size
	}
	return c
}

func (c Cursor) complete() bool { return c.Theme != "" && c.Size != 0 }

// DetectCursor reads the cursor the user's session uses from dotfiles
// under home; session is the remembered session id, whose compositor
// is consulted first.
func DetectCursor(home, session string) Cursor {
	read := func(rel string) string {
		data, _ := os.ReadFile(filepath.Join(home, rel)) //nolint:gosec // best-effort dotfile read
		return string(data)
	}
	found := parseRecorded(read(recordedRel))
	if found.complete() {
		return found
	}
	session = strings.ToLower(session)
	order := []string{"hypr", "niri", "sway"}
	for i, c := range order {
		if strings.Contains(session, c) {
			order = append([]string{c}, append(order[:i:i], order[i+1:]...)...)
			break
		}
	}
	for _, c := range order {
		if found.complete() {
			break
		}
		switch c {
		case "hypr":
			found = found.or(hyprlandFile(filepath.Join(home, ".config/hypr/hyprland.conf"), home, 0))
		case "niri":
			found = found.or(parseNiri(read(".config/niri/config.kdl")))
		default:
			found = found.or(parseSway(read(".config/sway/config")))
		}
	}
	for _, text := range []func() Cursor{
		func() Cursor { return parseGTKSettings(read(".config/gtk-4.0/settings.ini")) },
		func() Cursor { return parseGTKSettings(read(".config/gtk-3.0/settings.ini")) },
		func() Cursor { return parseIndexTheme(read(".icons/default/index.theme")) },
	} {
		if found.complete() {
			break
		}
		found = found.or(text())
	}
	return found
}

// ResolveCursor applies install_cursor's order: an explicit [greeter]
// key (config.toml or runtime.toml), the detected dotfiles, the
// XCURSOR_* environment, then the size default. The theme may stay "".
func ResolveCursor(g config.GreeterConfig, detected Cursor) Cursor {
	var configured Cursor
	if g.CursorThemeExplicit {
		configured.Theme = g.CursorTheme
	}
	if g.CursorSizeExplicit {
		configured.Size = g.CursorSize
	}
	env := Cursor{Theme: os.Getenv("XCURSOR_THEME")}
	if n, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("XCURSOR_SIZE")), 10, 32); err == nil {
		env.Size = uint32(n)
	}
	resolved := configured.or(detected).or(env)
	if resolved.Size == 0 {
		resolved.Size = max(g.CursorSize, 1)
	}
	return resolved
}

// ApplyCursor exports the resolved cursor for gelm's theme loader.
func ApplyCursor(c Cursor) {
	if c.Theme != "" {
		_ = os.Setenv("XCURSOR_THEME", c.Theme)
	}
	_ = os.Setenv("XCURSOR_SIZE", strconv.FormatUint(uint64(c.Size), 10))
}

// RecordCursor is the shell side (cursor_record.rs): from inside the
// live session, XCURSOR_THEME and XCURSOR_SIZE (falling back to
// HYPRCURSOR_SIZE) are the resolved truth, so they are written to
// $XDG_STATE_HOME/wayle/greeter-cursor for the next login screen.
// Nothing is written when neither is exported, so a good record is
// never clobbered by blanks.
func RecordCursor() error {
	theme := os.Getenv("XCURSOR_THEME")
	sizeText := os.Getenv("XCURSOR_SIZE")
	if sizeText == "" {
		sizeText = os.Getenv("HYPRCURSOR_SIZE")
	}
	size, sizeErr := strconv.ParseUint(strings.TrimSpace(sizeText), 10, 32)
	if theme == "" && sizeErr != nil {
		return nil
	}
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return nil
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "wayle")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // wayle's state dir
		return err
	}
	var body strings.Builder
	if theme != "" {
		body.WriteString("theme=" + theme + "\n")
	}
	if sizeErr == nil {
		body.WriteString("size=" + strconv.FormatUint(size, 10) + "\n")
	}
	return os.WriteFile(filepath.Join(dir, recordFile), []byte(body.String()), 0o644) //nolint:gosec // readable by the greeter on purpose
}

// parseRecorded reads the theme=/size= record.
func parseRecorded(text string) Cursor {
	var c Cursor
	for line := range strings.SplitSeq(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "theme":
			if value != "" {
				c.Theme = value
			}
		case "size":
			c.Size = parseSize(value)
		}
	}
	return c
}

func parseSize(s string) uint32 {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

// hyprlandFile reads a Hyprland config, following `source =` includes
// up to depth 3 (globs are not expanded).
func hyprlandFile(path, home string, depth int) Cursor {
	if depth > 3 {
		return Cursor{}
	}
	data, err := os.ReadFile(path) //nolint:gosec // best-effort dotfile read
	if err != nil {
		return Cursor{}
	}
	found, sources := parseHyprland(string(data))
	for _, src := range sources {
		if found.complete() {
			break
		}
		var resolved string
		switch {
		case strings.HasPrefix(src, "~/"):
			resolved = filepath.Join(home, src[2:])
		case strings.HasPrefix(src, "$HOME/"):
			resolved = filepath.Join(home, src[6:])
		case strings.HasPrefix(src, "/"):
			resolved = src
		default:
			resolved = filepath.Join(filepath.Dir(path), src)
		}
		found = found.or(hyprlandFile(resolved, home, depth+1))
	}
	return found
}

// parseHyprland extracts env = XCURSOR_* lines, exec[-once] = hyprctl
// setcursor <theme> <size>, and the source includes.
func parseHyprland(text string) (Cursor, []string) {
	var c Cursor
	var sources []string
	for line := range strings.SplitSeq(text, "\n") {
		line, _, _ = strings.Cut(line, "#")
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "env", "envd":
			name, val, ok := strings.Cut(value, ",")
			if !ok {
				continue
			}
			switch strings.TrimSpace(name) {
			case "XCURSOR_THEME":
				if c.Theme == "" {
					c.Theme = strings.TrimSpace(val)
				}
			case "XCURSOR_SIZE":
				if c.Size == 0 {
					c.Size = parseSize(val)
				}
			}
		case "exec", "exec-once":
			tok := strings.Fields(value)
			if len(tok) >= 2 && strings.HasSuffix(tok[0], "hyprctl") && tok[1] == "setcursor" {
				if len(tok) > 2 && c.Theme == "" {
					c.Theme = strings.Trim(tok[2], `"`)
				}
				if len(tok) > 3 && c.Size == 0 {
					c.Size = parseSize(tok[3])
				}
			}
		case "source":
			sources = append(sources, value)
		}
	}
	return c, sources
}

// parseNiri scans niri's KDL for xcursor-theme and xcursor-size.
func parseNiri(text string) Cursor {
	var c Cursor
	for line := range strings.SplitSeq(text, "\n") {
		line, _, _ = strings.Cut(line, "//")
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "xcursor-theme"); ok {
			if v := strings.Trim(strings.TrimSpace(rest), `"`); c.Theme == "" && v != "" {
				c.Theme = v
			}
		} else if rest, ok := strings.CutPrefix(line, "xcursor-size"); ok && c.Size == 0 {
			c.Size = parseSize(rest)
		}
	}
	return c
}

// parseSway reads `seat <name> xcursor_theme <theme> [size]`.
func parseSway(text string) Cursor {
	var c Cursor
	for line := range strings.SplitSeq(text, "\n") {
		line, _, _ = strings.Cut(line, "#")
		tok := strings.Fields(line)
		if len(tok) < 4 || tok[0] != "seat" || tok[2] != "xcursor_theme" {
			continue
		}
		if c.Theme == "" {
			c.Theme = strings.Trim(tok[3], `"`)
		}
		if c.Size == 0 && len(tok) > 4 {
			c.Size = parseSize(tok[4])
		}
	}
	return c
}

// parseGTKSettings reads gtk-cursor-theme-name/-size.
func parseGTKSettings(text string) Cursor {
	var c Cursor
	for line := range strings.SplitSeq(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "gtk-cursor-theme-name":
			if c.Theme == "" && value != "" {
				c.Theme = value
			}
		case "gtk-cursor-theme-size":
			if c.Size == 0 {
				c.Size = parseSize(value)
			}
		}
	}
	return c
}

// parseIndexTheme reads the Inherits= of ~/.icons/default/index.theme
// (theme only).
func parseIndexTheme(text string) Cursor {
	for line := range strings.SplitSeq(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "Inherits" {
			continue
		}
		first, _, _ := strings.Cut(value, ",")
		return Cursor{Theme: strings.TrimSpace(first)}
	}
	return Cursor{}
}
