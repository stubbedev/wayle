package config

import (
	"fmt"
	"strconv"
)

// ClickAction is one module input binding, parsed from the schema's
// ClickAction strings (crates/wayle-config/src/click_action.rs):
// "dropdown:<name>", "brightness:<delta>" or "brightness:toggle", an
// empty string for no action, and anything else runs as a shell
// command.
type ClickAction struct {
	Kind       ClickKind
	Dropdown   string
	Command    string
	Brightness int32
}

// ClickKind discriminates the action.
type ClickKind int

// Kinds.
const (
	ClickNone ClickKind = iota
	ClickDropdown
	ClickShell
	ClickBrightness
	ClickBrightnessToggle
)

// ParseClickAction parses one schema string.
func ParseClickAction(s string) (ClickAction, error) {
	if s == "" {
		return ClickAction{Kind: ClickNone}, nil
	}
	if rest, ok := cutPrefix(s, "brightness:"); ok {
		if rest == "toggle" {
			return ClickAction{Kind: ClickBrightnessToggle}, nil
		}
		var delta int32
		if _, err := fmt.Sscan(rest, &delta); err != nil {
			return ClickAction{}, fmt.Errorf("click action %q: bad brightness delta", s)
		}
		return ClickAction{Kind: ClickBrightness, Brightness: delta}, nil
	}
	if rest, ok := cutPrefix(s, "dropdown:"); ok {
		return ClickAction{Kind: ClickDropdown, Dropdown: rest}, nil
	}
	return ClickAction{Kind: ClickShell, Command: s}, nil
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// String serializes back to the schema form.
func (a ClickAction) String() string {
	switch a.Kind {
	case ClickDropdown:
		return "dropdown:" + a.Dropdown
	case ClickBrightness:
		return "brightness:" + strconv.FormatInt(int64(a.Brightness), 10)
	case ClickBrightnessToggle:
		return "brightness:toggle"
	case ClickShell:
		return a.Command
	}
	return ""
}

// MustClickAction parses a compile-time default; defaults are static
// strings, a parse failure is a programmer error.
func MustClickAction(s string) ClickAction {
	action, err := ParseClickAction(s)
	if err != nil {
		panic(err)
	}
	return action
}
