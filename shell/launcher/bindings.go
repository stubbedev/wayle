package launcher

import (
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/app"

	engine "github.com/stubbedev/wayle/service/launcher"
)

// keyAction is a surface action a key can trigger (views.rs KeyAction).
type keyAction uint8

const (
	keyAccept keyAction = iota + 1
	keyAcceptAlt
	keyAcceptCustom
	keyCancel
	keyDeleteEntry
	keyModeNext
	keyModePrevious
	keyModeComplete
	keyRowUp
	keyRowDown
	keyRowFirst
	keyRowLast
	keyPagePrev
	keyPageNext
	// keyCustom is kb-custom-N; the binding carries N.
	keyCustom
)

// keyBinding is a resolved binding: accelerator -> action (and N for
// kb-custom-N).
type keyBinding struct {
	accel  app.Accel
	action keyAction
	custom uint8
}

// actionFromName maps rofi kb- names onto the actions the surface
// implements. Entry editing (move-word, remove-char, paste) stays with
// the entry's own editing keys, as in the Rust surface.
func actionFromName(name string) (keyAction, uint8, bool) {
	switch name {
	case "accept-entry":
		return keyAccept, 0, true
	case "accept-alt":
		return keyAcceptAlt, 0, true
	case "accept-custom":
		return keyAcceptCustom, 0, true
	case "cancel":
		return keyCancel, 0, true
	case "delete-entry":
		return keyDeleteEntry, 0, true
	case "mode-next":
		return keyModeNext, 0, true
	case "mode-previous":
		return keyModePrevious, 0, true
	case "mode-complete":
		return keyModeComplete, 0, true
	case "row-up", "element-prev":
		return keyRowUp, 0, true
	case "row-down", "element-next":
		return keyRowDown, 0, true
	case "row-first":
		return keyRowFirst, 0, true
	case "row-last":
		return keyRowLast, 0, true
	case "page-prev":
		return keyPagePrev, 0, true
	case "page-next":
		return keyPageNext, 0, true
	}
	if n, ok := strings.CutPrefix(name, "custom-"); ok {
		if v, err := strconv.ParseUint(n, 10, 8); err == nil && v >= 1 && v <= 19 {
			return keyCustom, uint8(v), true
		}
	}
	return 0, 0, false
}

// compileKeys builds the lookup table; unimplemented actions are
// skipped and unparseable key specs logged. Key specs are GTK
// accelerator names (Control+Shift+Return, Super+Return).
func compileKeys(bindings []engine.Binding) []keyBinding {
	var table []keyBinding
	for _, b := range bindings {
		action, n, ok := actionFromName(b.Action)
		if !ok {
			continue
		}
		for spec := range strings.SplitSeq(b.Keys, ",") {
			spec = strings.TrimSpace(spec)
			accel, err := app.ParseAccel(spec)
			if err != nil {
				log.Printf("launcher: unparseable keybinding %q", spec)
				continue
			}
			table = append(table, keyBinding{accel: accel, action: action, custom: n})
		}
	}
	return table
}

// lookupKey finds the binding for a press; the accelerator form
// already folds letters, which is the Rust lookup's lowercase match.
func lookupKey(table []keyBinding, press app.Accel) (keyBinding, bool) {
	for _, b := range table {
		if b.accel == press {
			return b, true
		}
	}
	return keyBinding{}, false
}

// mouseAction is a surface action a pointer binding can trigger. rofi's
// row-left/row-right move between columns and the list has one, so
// they are recognized and bind nothing.
type mouseAction uint8

const (
	mouseSelect mouseAction = iota + 1
	mouseAccept
	mouseAcceptCustom
	mouseRowUp
	mouseRowDown
)

type mouseEntry struct {
	binding engine.MouseBinding
	action  mouseAction
}

func mouseActionFromName(name string) (mouseAction, bool) {
	switch name {
	case "me-select-entry":
		return mouseSelect, true
	case "me-accept-entry":
		return mouseAccept, true
	case "me-accept-custom":
		return mouseAcceptCustom, true
	case "ml-row-up":
		return mouseRowUp, true
	case "ml-row-down":
		return mouseRowDown, true
	}
	return 0, false
}

func compileMouse(bindings []engine.Binding) []mouseEntry {
	var table []mouseEntry
	for _, b := range bindings {
		action, ok := mouseActionFromName(b.Action)
		if !ok {
			continue
		}
		for _, mb := range engine.ParseMouseList(b.Keys) {
			table = append(table, mouseEntry{binding: mb, action: action})
		}
	}
	return table
}

// lookupButton returns every action bound to a press, select first so
// the row is current when an accept reads it. presses is the click
// count: a double-press binding loses to a single click while a
// single-press one still fires on a double's first press.
func lookupButton(table []mouseEntry, button engine.MouseButton, presses int, mods engine.MouseModifiers) []mouseAction {
	var out []mouseAction
	for _, e := range table {
		b := e.binding
		if b.Modifiers != mods || b.Scroll != 0 || b.Button != button || (b.Double && presses < 2) {
			continue
		}
		out = append(out, e.action)
	}
	slices.SortStableFunc(out, func(a, b mouseAction) int {
		ai, bi := 1, 1
		if a == mouseSelect {
			ai = 0
		}
		if b == mouseSelect {
			bi = 0
		}
		return ai - bi
	})
	return slices.Compact(out)
}

// lookupScroll returns every action bound to a scroll direction.
func lookupScroll(table []mouseEntry, dir engine.ScrollDirection, mods engine.MouseModifiers) []mouseAction {
	var out []mouseAction
	for _, e := range table {
		if e.binding.Modifiers == mods && e.binding.Scroll == dir {
			out = append(out, e.action)
		}
	}
	return slices.Compact(out)
}

// gdkButton maps a Linux input button code onto GDK's numbering, the
// one rofi's bindings name: left 1, middle 2, right 3, and every other
// button after the old 4-7 scroll slots (side 8, extra 9, ...).
func gdkButton(code uint32) engine.MouseButton {
	const btnLeft, btnRight, btnMiddle = 0x110, 0x111, 0x112
	switch code {
	case btnLeft:
		return engine.MousePrimary
	case btnMiddle:
		return engine.MouseMiddle
	case btnRight:
		return engine.MouseSecondary
	}
	return engine.MouseButton(code - (btnLeft - 1) + 4)
}
