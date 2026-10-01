package config

import (
	"strconv"
	"strings"
)

// ClickAction is one module input binding, parsed from the schema's
// ClickAction strings (crates/wayle-config/src/click_action.rs):
// "dropdown:<name>", "brightness:<delta>" or "brightness:toggle", an
// empty string for no action, and anything else runs as a shell
// command. Like the Rust type every string parses: a brightness delta
// that is not an i32 is no action.
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

// ParseClickAction parses one schema string (ClickAction::from_str).
func ParseClickAction(s string) ClickAction {
	if s == "" {
		return ClickAction{Kind: ClickNone}
	}
	if rest, ok := strings.CutPrefix(s, "brightness:"); ok {
		if rest == "toggle" {
			return ClickAction{Kind: ClickBrightnessToggle}
		}
		delta, err := strconv.ParseInt(rest, 10, 32)
		if err != nil {
			return ClickAction{Kind: ClickNone}
		}
		return ClickAction{Kind: ClickBrightness, Brightness: int32(delta)}
	}
	if name, ok := strings.CutPrefix(s, "dropdown:"); ok {
		return ClickAction{Kind: ClickDropdown, Dropdown: name}
	}
	return ClickAction{Kind: ClickShell, Command: s}
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

// UnmarshalConfig implements Unmarshaler.
func (a *ClickAction) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	*a = ParseClickAction(s)
	return nil
}

// MarshalConfig implements Marshaler.
func (a ClickAction) MarshalConfig() any { return a.String() }

func (ClickAction) configSchema(*schemaGen) Schema { return Schema{"type": "string"} }

// ClickConfig is a module's five input bindings as one value, the view
// the bar's input routing takes; each module config declares the five
// keys itself (their docs differ per module) and exposes them through
// a Clicks method.
type ClickConfig struct {
	LeftClick   ClickAction
	RightClick  ClickAction
	MiddleClick ClickAction
	ScrollUp    ClickAction
	ScrollDown  ClickAction
}
