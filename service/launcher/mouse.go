package launcher

import (
	"log"
	"strconv"
	"strings"
)

// MouseModifiers are the modifiers held with a mouse binding.
type MouseModifiers uint8

// Mouse binding modifiers.
const (
	MouseControl MouseModifiers = 1 << iota
	MouseShift
	MouseAlt
	MouseSuper
)

// MouseButton is a physical button by its pointer button number (1
// primary, 2 middle, 3 secondary, 8 back, 9 forward, ExtraN beyond).
type MouseButton uint32

// Named buttons (mouse.rs MouseButton::number).
const (
	MousePrimary   MouseButton = 1
	MouseMiddle    MouseButton = 2
	MouseSecondary MouseButton = 3
	MouseBack      MouseButton = 8
	MouseForward   MouseButton = 9
)

// ScrollDirection is which way the wheel turned.
type ScrollDirection uint8

// Scroll directions.
const (
	ScrollUp ScrollDirection = iota + 1
	ScrollDown
	ScrollLeft
	ScrollRight
)

// MouseBinding is one parsed rofi mouse identifier: modifiers plus
// either a click (Button, Double) or a scroll (Scroll non-zero).
type MouseBinding struct {
	Modifiers MouseModifiers
	// Button is the clicked button; zero for a scroll binding.
	Button MouseButton
	// Double requires two presses (MouseD...).
	Double bool
	// Scroll is the wheel direction; zero for a click binding.
	Scroll ScrollDirection
}

// ParseMouse parses one rofi mouse identifier with its modifier
// prefixes (rofi-keys(5): Mouse<D><Primary|Secondary|Middle|Forward|
// Back|ExtraN> and Scroll<Up|Down|Left|Right>). A key name is not a
// mouse binding: the two tables share a config shape, so a mistyped
// Return is refused rather than bound to some button.
func ParseMouse(spec string) (MouseBinding, bool) {
	var b MouseBinding
	input := false
	for part := range strings.SplitSeq(spec, "+") {
		switch part = strings.TrimSpace(part); part {
		case "Control", "Ctrl":
			b.Modifiers |= MouseControl
		case "Shift":
			b.Modifiers |= MouseShift
		case "Alt", "Mod1":
			b.Modifiers |= MouseAlt
		case "Super", "Mod4":
			b.Modifiers |= MouseSuper
		default:
			if !parseMouseInput(part, &b) {
				return MouseBinding{}, false
			}
			input = true
		}
	}
	return b, input
}

func parseMouseInput(name string, b *MouseBinding) bool {
	if dir, ok := strings.CutPrefix(name, "Scroll"); ok {
		switch dir {
		case "Up":
			b.Scroll = ScrollUp
		case "Down":
			b.Scroll = ScrollDown
		case "Left":
			b.Scroll = ScrollLeft
		case "Right":
			b.Scroll = ScrollRight
		default:
			return false
		}
		b.Button, b.Double = 0, false
		return true
	}
	rest, ok := strings.CutPrefix(name, "Mouse")
	if !ok {
		return false
	}
	// The D is only a double marker when a button name follows it.
	double := false
	if after, ok := strings.CutPrefix(rest, "D"); ok && after != "" {
		double, rest = true, after
	}
	button, ok := parseButton(rest)
	if !ok {
		return false
	}
	b.Button, b.Double, b.Scroll = button, double, 0
	return true
}

func parseButton(name string) (MouseButton, bool) {
	switch name {
	case "Primary":
		return MousePrimary, true
	case "Secondary":
		return MouseSecondary, true
	case "Middle":
		return MouseMiddle, true
	case "Forward":
		return MouseForward, true
	case "Back":
		return MouseBack, true
	}
	n, ok := strings.CutPrefix(name, "Extra")
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(n, 10, 8)
	if err != nil {
		return 0, false
	}
	return MouseButton(v), true
}

// ParseMouseList parses a comma-separated binding list, dropping (and
// logging) the parts that are not mouse identifiers.
func ParseMouseList(specs string) []MouseBinding {
	var out []MouseBinding
	for spec := range strings.SplitSeq(specs, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		b, ok := ParseMouse(spec)
		if !ok {
			log.Printf("launcher: unparseable mouse binding %q", spec)
			continue
		}
		out = append(out, b)
	}
	return out
}
