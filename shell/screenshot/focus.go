package screenshot

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
)

// Focus resolves what the output and window modes capture: the focused
// output and the active window, from whichever compositor is running
// (screenshot/mod.rs's focused_output_name and active_window_target).
type Focus interface {
	// FocusedOutput is the focused output's connector name; false lets
	// the capture fall back to the first output.
	FocusedOutput() (string, bool)
	// ActiveWindow identifies the focused window; the zero target
	// matches nothing.
	ActiveWindow() windowTarget
}

// CompositorFocus asks Hyprland (when Hyprland is set), then mango,
// then niri - the Rust host's order. Sway reports nothing, as in Rust:
// the output mode falls back to the first output there.
type CompositorFocus struct {
	Hyprland *hyprland.Connection
}

// mangoWait bounds the wait for mango's first watch frames.
const mangoWait = 500 * time.Millisecond

// FocusedOutput implements Focus.
func (f CompositorFocus) FocusedOutput() (string, bool) {
	if f.Hyprland != nil {
		if mons, err := f.Hyprland.Monitors(); err == nil {
			for _, m := range mons {
				if m.Focused {
					return m.Name, true
				}
			}
		}
	}
	if mons, _, ok := mangoState(); ok {
		for _, m := range mons {
			if m.IsActive {
				return m.Name, true
			}
		}
	}
	// niri / unknown: the caller falls back to the first output.
	return "", false
}

// ActiveWindow implements Focus.
func (f CompositorFocus) ActiveWindow() windowTarget {
	if f.Hyprland != nil {
		if raw, err := f.Hyprland.Command("j/activewindow"); err == nil {
			if t, ok := parseHyprlandActiveWindow(raw); ok {
				return t
			}
		}
	}
	if niri.IsRunning() {
		if conn, err := niri.Connect(); err == nil {
			wins, err := conn.Windows()
			_ = conn.Close()
			if err == nil {
				for _, w := range wins {
					if w.IsFocused {
						return targetOf(w.AppID, w.Title)
					}
				}
			}
		}
	}
	if _, clients, ok := mangoState(); ok {
		for _, c := range clients {
			if c.IsFocused {
				t := windowTarget{appID: c.AppID, hasAppID: c.HasAppID, title: c.Title, hasTitle: c.HasTitle}
				return t
			}
		}
	}
	return windowTarget{}
}

// parseHyprlandActiveWindow reads j/activewindow: the address (hex,
// 0x-prefixed) becomes the toplevel-export handle, class and title the
// match keys. An empty object means no active window.
func parseHyprlandActiveWindow(raw string) (windowTarget, bool) {
	var w struct {
		Address *string `json:"address"`
		Class   string  `json:"class"`
		Title   string  `json:"title"`
	}
	if err := json.Unmarshal([]byte(raw), &w); err != nil || w.Address == nil {
		return windowTarget{}, false
	}
	t := windowTarget{appID: w.Class, hasAppID: true, title: w.Title, hasTitle: true}
	if h, err := strconv.ParseUint(strings.TrimPrefix(*w.Address, "0x"), 16, 64); err == nil {
		t.hyprlandHandle, t.hasHandle = h, true
	}
	return t, true
}

// targetOf builds a target from optional app id and title.
func targetOf(appID, title *string) windowTarget {
	var t windowTarget
	if appID != nil {
		t.appID, t.hasAppID = *appID, true
	}
	if title != nil {
		t.title, t.hasTitle = *title, true
	}
	return t
}

// mangoState snapshots mango's monitors and clients through a
// short-lived watch; false when mango is not running or never answers.
func mangoState() ([]mango.Monitor, []mango.Client, bool) {
	if !mango.IsRunning() {
		return nil, nil, false
	}
	w, err := mango.Watch()
	if err != nil {
		return nil, nil, false
	}
	defer w.Close()
	deadline := time.After(mangoWait)
	for !w.Ready() {
		select {
		case _, open := <-w.Ticks():
			if !open {
				return nil, nil, false
			}
		case <-deadline:
			return nil, nil, false
		}
	}
	mons, clients := w.State()
	return mons, clients, true
}
