package launcher

import "maps"

// Binding is one action and its comma-separated key (or mouse) list.
type Binding struct {
	Action string
	Keys   string
}

// DefaultKeybindings is rofi's default table (keybinds.rs DEFAULTS):
// action names are rofi's kb- names without the prefix, keys are
// comma-separated GTK accelerator names.
var DefaultKeybindings = []Binding{
	{"accept-entry", "Return,KP_Enter"},
	{"accept-alt", "Shift+Return"},
	{"accept-custom", "Control+Return"},
	{"accept-custom-alt", "Control+Shift+Return"},
	{"cancel", "Escape,Control+g,Control+bracketleft"},
	{"delete-entry", "Shift+Delete"},
	{"mode-next", "Shift+Right,Control+Tab"},
	{"mode-previous", "Shift+Left,Control+ISO_Left_Tab"},
	{"mode-complete", "Control+l"},
	{"row-up", "Up,Control+p"},
	{"row-down", "Down,Control+n"},
	{"row-left", "Control+Page_Up"},
	{"row-right", "Control+Page_Down"},
	{"row-first", "Home,KP_Home"},
	{"row-last", "End,KP_End"},
	{"row-select", "Control+space"},
	{"page-prev", "Page_Up"},
	{"page-next", "Page_Down"},
	{"element-next", "Tab"},
	{"element-prev", "ISO_Left_Tab"},
	{"toggle-case-sensitivity", "grave,dead_grave"},
	{"toggle-sort", "Alt+grave"},
	{"clear-line", "Control+w"},
	{"move-front", "Control+a"},
	{"move-end", "Control+e"},
	{"move-word-back", "Alt+b,Control+Left"},
	{"move-word-forward", "Alt+f,Control+Right"},
	{"move-char-back", "Left,Control+b"},
	{"move-char-forward", "Right,Control+f"},
	{"remove-word-back", "Control+Alt+h,Control+BackSpace"},
	{"remove-word-forward", "Control+Alt+d"},
	{"remove-char-back", "BackSpace,Shift+BackSpace,Control+h"},
	{"remove-char-forward", "Delete,Control+d"},
	{"remove-to-eol", "Control+k"},
	{"remove-to-sol", "Control+u"},
	{"paste-primary", "Shift+Insert"},
	{"paste-secondary", "Control+v,Insert"},
	{"select-element", "space"},
	{"custom-1", "Alt+1"},
	{"custom-2", "Alt+2"},
	{"custom-3", "Alt+3"},
	{"custom-4", "Alt+4"},
	{"custom-5", "Alt+5"},
	{"custom-6", "Alt+6"},
	{"custom-7", "Alt+7"},
	{"custom-8", "Alt+8"},
	{"custom-9", "Alt+9"},
	{"custom-10", "Alt+0"},
	{"custom-11", "Alt+exclam"},
	{"custom-12", "Alt+at"},
	{"custom-13", "Alt+numbersign"},
	{"custom-14", "Alt+dollar"},
	{"custom-15", "Alt+percent"},
	{"custom-16", "Alt+dead_circumflex"},
	{"custom-17", "Alt+ampersand"},
	{"custom-18", "Alt+asterisk"},
	{"custom-19", "Alt+parenleft"},
}

// DefaultMouseBindings is rofi-keys(5)'s mouse table, with one
// deliberate deviation: rofi accepts a row on MouseDPrimary (a double
// click) and only selects on a single one. wayle has always accepted on
// a single click, so me-accept-entry keeps both; rofi's exact behavior
// is one `-me-accept-entry MouseDPrimary` away.
var DefaultMouseBindings = []Binding{
	{"me-select-entry", "MousePrimary"},
	{"me-accept-entry", "MousePrimary,MouseDPrimary"},
	{"me-accept-custom", "Control+MouseDPrimary"},
	{"ml-row-up", "ScrollUp"},
	{"ml-row-down", "ScrollDown"},
	{"ml-row-left", "ScrollLeft"},
	{"ml-row-right", "ScrollRight"},
}

// EffectiveKeybindings is DefaultKeybindings with per-action overrides
// applied ([launcher.keybindings], then per-session -kb-*).
func EffectiveKeybindings(overrides map[string]string) []Binding {
	return withOverrides(DefaultKeybindings, overrides)
}

// EffectiveMouseBindings is DefaultMouseBindings with per-action
// overrides applied ([launcher.mouse-bindings], then -me-*/-ml-*).
func EffectiveMouseBindings(overrides map[string]string) []Binding {
	return withOverrides(DefaultMouseBindings, overrides)
}

// withOverrides keeps the default table's actions and order; an
// override for an action the table does not name is ignored, as the
// Rust table walk ignores it.
func withOverrides(defaults []Binding, overrides map[string]string) []Binding {
	out := make([]Binding, len(defaults))
	for i, b := range defaults {
		if keys, ok := overrides[b.Action]; ok {
			b.Keys = keys
		}
		out[i] = b
	}
	return out
}

// MergeOverrides layers later maps over earlier ones: config first,
// then the session's flags.
func MergeOverrides(layers ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, layer := range layers {
		maps.Copy(out, layer)
	}
	return out
}
