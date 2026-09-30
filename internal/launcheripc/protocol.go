// Package launcheripc is the launcher socket (wayle-ipc
// launcher_socket.rs): the protocol between `wayle launcher` (the rofi
// shim) and the launcher surface in the shell, with both halves - the
// CLI's client and the shell's server.
//
// One persistent connection per session carries newline-delimited JSON
// frames, and the connection doubles as the session's lifetime: when
// the CLI dies the server sees EOF and closes the surface; when the
// server replies with a terminal frame, the CLI prints it and exits
// with rofi's code. The wire is the Rust shell's byte for byte, so
// either CLI talks to either shell.
package launcheripc

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RowChunk is the number of rows per rows frame when streaming stdin.
const RowChunk = 2000

// SocketPath is $XDG_RUNTIME_DIR/wayle/launcher.sock, or
// /tmp/wayle-launcher.sock when the runtime dir is unset.
func SocketPath() string {
	if dir, ok := os.LookupEnv("XDG_RUNTIME_DIR"); ok {
		return filepath.Join(dir, "wayle", "launcher.sock")
	}
	return "/tmp/wayle-launcher.sock"
}

// WidthUnit is the unit of a rofi -width value.
type WidthUnit uint8

// Width units, distinguished by syntax in rofi rather than a suffix.
const (
	// WidthPercent is `-width 60`: percent of the monitor's width.
	WidthPercent WidthUnit = iota + 1
	// WidthChars is `-width -30`: a character count, measured against
	// the font's advance.
	WidthChars
	// WidthPixels is `-width 600px`.
	WidthPixels
)

// Width is a parsed -width. The zero Width is no width.
type Width struct {
	Unit WidthUnit
	// Value is the percent, the char count, or the pixels.
	Value float64
}

// ParseWidth reads a rofi -width: NNpx is pixels, a negative number a
// character count, anything else a percentage. Not a number is an
// error naming the value.
func ParseWidth(raw string) (Width, error) {
	raw = strings.TrimSpace(raw)
	if px, ok := strings.CutSuffix(raw, "px"); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(px), 10, 32)
		if err != nil {
			return Width{}, fmt.Errorf("-width: %s is not a pixel count", raw)
		}
		return Width{Unit: WidthPixels, Value: float64(n)}, nil
	}
	v, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return Width{}, fmt.Errorf("-width: %s is not a number", raw)
	}
	if v < 0 {
		// `as u32`: truncated, saturating at the type's range.
		return Width{Unit: WidthChars, Value: math.Min(math.Trunc(-v), math.MaxUint32)}, nil
	}
	return Width{Unit: WidthPercent, Value: v}, nil
}

// MarshalJSON writes serde's externally tagged form: {"percent":60.0},
// {"chars":30}, {"pixels":600}.
func (w Width) MarshalJSON() ([]byte, error) {
	switch w.Unit {
	case WidthPercent:
		return json.Marshal(map[string]float64{"percent": w.Value})
	case WidthChars:
		return json.Marshal(map[string]uint32{"chars": uint32(w.Value)})
	case WidthPixels:
		return json.Marshal(map[string]int32{"pixels": int32(w.Value)})
	}
	return nil, errors.New("launcheripc: marshal an unset width")
}

// UnmarshalJSON reads the tagged form; an unknown tag is an error.
func (w *Width) UnmarshalJSON(data []byte) error {
	var m map[string]float64
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if len(m) != 1 {
		return fmt.Errorf("launcheripc: width %s", data)
	}
	for k, v := range m {
		switch k {
		case "percent":
			*w = Width{Unit: WidthPercent, Value: v}
		case "chars":
			*w = Width{Unit: WidthChars, Value: v}
		case "pixels":
			*w = Width{Unit: WidthPixels, Value: v}
		default:
			return fmt.Errorf("launcheripc: unknown width unit %q", k)
		}
	}
	return nil
}

// SessionOptions is the open frame's per-invocation options: rofi's
// flag surface. Every field is optional; the shell merges the set ones
// over [launcher] (launcher_socket.rs SessionOptions, kebab-case).
type SessionOptions struct {
	Mode         *string  `json:"mode,omitempty"`
	Modes        []string `json:"modes,omitempty"`
	Dmenu        bool     `json:"dmenu,omitempty"`
	ErrorMessage *string  `json:"error-message,omitempty"`

	Prompt      *string `json:"prompt,omitempty"`
	Lines       *uint32 `json:"lines,omitempty"`
	Mesg        *string `json:"mesg,omitempty"`
	Filter      *string `json:"filter,omitempty"`
	Select      *string `json:"select,omitempty"`
	SelectedRow *uint32 `json:"selected-row,omitempty"`
	WindowTitle *string `json:"window-title,omitempty"`

	MultiSelect            bool     `json:"multi-select,omitempty"`
	OnlyMatch              bool     `json:"only-match,omitempty"`
	NoCustom               bool     `json:"no-custom,omitempty"`
	Password               bool     `json:"password,omitempty"`
	MarkupRows             bool     `json:"markup-rows,omitempty"`
	Sync                   bool     `json:"sync,omitempty"`
	Dump                   bool     `json:"dump,omitempty"`
	Urgent                 []uint32 `json:"urgent,omitempty"`
	Active                 []uint32 `json:"active,omitempty"`
	BallotSelected         *string  `json:"ballot-selected,omitempty"`
	BallotUnselected       *string  `json:"ballot-unselected,omitempty"`
	DisplayColumns         []uint32 `json:"display-columns,omitempty"`
	DisplayColumnSeparator *string  `json:"display-column-separator,omitempty"`
	EllipsizeMode          *string  `json:"ellipsize-mode,omitempty"`
	KeepRight              bool     `json:"keep-right,omitempty"`

	Matching        *string `json:"matching,omitempty"`
	Tokenize        *bool   `json:"tokenize,omitempty"`
	NegateChar      *string `json:"negate-char,omitempty"`
	NormalizeMatch  *bool   `json:"normalize-match,omitempty"`
	Sort            *bool   `json:"sort,omitempty"`
	SortingMethod   *string `json:"sorting-method,omitempty"`
	CaseSensitive   *bool   `json:"case-sensitive,omitempty"`
	CaseSmart       *bool   `json:"case-smart,omitempty"`
	CaseInsensitive *bool   `json:"case-insensitive,omitempty"`

	Location        *uint8  `json:"location,omitempty"`
	Width           *Width  `json:"width,omitempty"`
	XOffset         *int32  `json:"xoffset,omitempty"`
	YOffset         *int32  `json:"yoffset,omitempty"`
	Monitor         *string `json:"monitor,omitempty"`
	NoFixedNumLines bool    `json:"no-fixed-num-lines,omitempty"`
	SidebarMode     *bool   `json:"sidebar-mode,omitempty"`
	Cycle           *bool   `json:"cycle,omitempty"`
	AutoSelect      *bool   `json:"auto-select,omitempty"`
	HoverSelect     *bool   `json:"hover-select,omitempty"`
	ShowIcons       *bool   `json:"show-icons,omitempty"`
	IconTheme       *string `json:"icon-theme,omitempty"`

	Terminal                *string  `json:"terminal,omitempty"`
	RunCommand              *string  `json:"run-command,omitempty"`
	RunShellCommand         *string  `json:"run-shell-command,omitempty"`
	RunListCommand          *string  `json:"run-list-command,omitempty"`
	SSHClient               *string  `json:"ssh-client,omitempty"`
	SSHCommand              *string  `json:"ssh-command,omitempty"`
	ParseHosts              *bool    `json:"parse-hosts,omitempty"`
	ParseKnownHosts         *bool    `json:"parse-known-hosts,omitempty"`
	WindowFormat            *string  `json:"window-format,omitempty"`
	WindowCommand           *string  `json:"window-command,omitempty"`
	WindowMatchFields       []string `json:"window-match-fields,omitempty"`
	HideActiveWindow        *bool    `json:"hide-active-window,omitempty"`
	DrunCategories          []string `json:"drun-categories,omitempty"`
	DrunExcludeCategories   []string `json:"drun-exclude-categories,omitempty"`
	DrunMatchFields         []string `json:"drun-match-fields,omitempty"`
	DrunDisplayFormat       *string  `json:"drun-display-format,omitempty"`
	DrunShowActions         *bool    `json:"drun-show-actions,omitempty"`
	DrunURLLauncher         *string  `json:"drun-url-launcher,omitempty"`
	ApplicationFallbackIcon *string  `json:"application-fallback-icon,omitempty"`
	IgnoredPrefixes         []string `json:"ignored-prefixes,omitempty"`
	CombiModes              []string `json:"combi-modes,omitempty"`
	CombiDisplayFormat      *string  `json:"combi-display-format,omitempty"`

	// KbOverrides maps an action (no kb- prefix) to its key list.
	KbOverrides map[string]string `json:"kb-overrides,omitempty"`
	// MouseOverrides maps an action (me-/ml- prefix kept, the two
	// namespaces have their own names) to its binding list.
	MouseOverrides map[string]string `json:"mouse-overrides,omitempty"`
	// DisplayNames maps a mode to its display name (-display-<mode>).
	DisplayNames map[string]string `json:"display-names,omitempty"`

	OnSelectionChanged *string `json:"on-selection-changed,omitempty"`
	OnEntryAccepted    *string `json:"on-entry-accepted,omitempty"`
	OnModeChanged      *string `json:"on-mode-changed,omitempty"`
	OnMenuCanceled     *string `json:"on-menu-canceled,omitempty"`
	OnMenuError        *string `json:"on-menu-error,omitempty"`

	Font          *string `json:"font,omitempty"`
	Style         *string `json:"style,omitempty"`
	PreviewCmd    *string `json:"preview-cmd,omitempty"`
	CompleterMode *string `json:"completer-mode,omitempty"`
}

// Selected is one accepted row.
type Selected struct {
	// Index is the row's input index; -1 for accepted custom input.
	Index int64  `json:"index"`
	Text  string `json:"text"`
}

// Frame type tags.
const (
	FrameOpen      = "open"
	FrameRows      = "rows"
	FrameRowsDone  = "rows-done"
	FrameOpened    = "opened"
	FrameBusy      = "busy"
	FrameResult    = "result"
	FrameCancelled = "cancelled"
	FrameDump      = "dump"
)

// ClientFrame is a CLI -> shell frame: open (Options, Replace), rows
// (Items), or rows-done.
type ClientFrame struct {
	Type string `json:"type"`
	// Options and Replace are the open frame's.
	Options *SessionOptions `json:"options,omitempty"`
	Replace bool            `json:"replace,omitempty"`
	// Items is the rows frame's chunk.
	Items []string `json:"items,omitempty"`
}

// MarshalJSON writes the variant's fields, all of them: serde requires
// an open frame's replace and a rows frame's items.
func (f ClientFrame) MarshalJSON() ([]byte, error) {
	switch f.Type {
	case FrameOpen:
		opts := f.Options
		if opts == nil {
			opts = &SessionOptions{}
		}
		return json.Marshal(struct {
			Type    string          `json:"type"`
			Options *SessionOptions `json:"options"`
			Replace bool            `json:"replace"`
		}{f.Type, opts, f.Replace})
	case FrameRows:
		items := f.Items
		if items == nil {
			items = []string{}
		}
		return json.Marshal(struct {
			Type  string   `json:"type"`
			Items []string `json:"items"`
		}{f.Type, items})
	case FrameRowsDone:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{f.Type})
	}
	return nil, fmt.Errorf("launcheripc: unknown client frame %q", f.Type)
}

// ServerFrame is a shell -> CLI frame: opened, busy, result (Code,
// Selected, Filter), cancelled (Code), or dump (Items).
type ServerFrame struct {
	Type string `json:"type"`
	// Code is the rofi exit code: 0 accept, 1 cancel, 10..28
	// kb-custom-N.
	Code     int        `json:"code"`
	Selected []Selected `json:"selected,omitempty"`
	// Filter is the query at accept time (rofi -format f/F).
	Filter string   `json:"filter"`
	Items  []string `json:"items,omitempty"`
}

// MarshalJSON writes only the fields the frame's variant carries, as
// serde's tagged enum does.
func (f ServerFrame) MarshalJSON() ([]byte, error) {
	switch f.Type {
	case FrameOpened, FrameBusy:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{f.Type})
	case FrameResult:
		selected := f.Selected
		if selected == nil {
			selected = []Selected{}
		}
		return json.Marshal(struct {
			Type     string     `json:"type"`
			Code     int        `json:"code"`
			Selected []Selected `json:"selected"`
			Filter   string     `json:"filter"`
		}{f.Type, f.Code, selected, f.Filter})
	case FrameCancelled:
		return json.Marshal(struct {
			Type string `json:"type"`
			Code int    `json:"code"`
		}{f.Type, f.Code})
	case FrameDump:
		items := f.Items
		if items == nil {
			items = []string{}
		}
		return json.Marshal(struct {
			Type  string   `json:"type"`
			Items []string `json:"items"`
		}{f.Type, items})
	}
	return nil, fmt.Errorf("launcheripc: unknown server frame %q", f.Type)
}

// Result is a result frame.
func Result(code int, selected []Selected, filter string) ServerFrame {
	return ServerFrame{Type: FrameResult, Code: code, Selected: selected, Filter: filter}
}

// Cancelled is a cancelled frame with rofi's code 1.
func Cancelled() ServerFrame { return ServerFrame{Type: FrameCancelled, Code: 1} }

// Busy is the reply when another session is live and replace was not
// asked for.
func Busy() ServerFrame { return ServerFrame{Type: FrameBusy} }

// Dump is the -dump reply: the filtered rows in display order.
func Dump(items []string) ServerFrame { return ServerFrame{Type: FrameDump, Items: items} }
