package launcher

import "testing"

func keysOf(bindings []Binding, action string) (string, bool) {
	for _, b := range bindings {
		if b.Action == action {
			return b.Keys, true
		}
	}
	return "", false
}

func TestKeybindingOverridesReplaceDefaults(t *testing.T) {
	b := EffectiveKeybindings(map[string]string{"cancel": "Control+q", "no-such-action": "x"})
	if k, _ := keysOf(b, "cancel"); k != "Control+q" {
		t.Errorf("cancel = %q", k)
	}
	if k, _ := keysOf(b, "accept-entry"); k != "Return,KP_Enter" {
		t.Errorf("accept-entry kept its default, got %q", k)
	}
	if _, ok := keysOf(b, "no-such-action"); ok {
		t.Error("an override for an unknown action must not add one")
	}
	if len(b) != len(DefaultKeybindings) {
		t.Errorf("table size changed: %d", len(b))
	}
}

func TestMouseOverridesReplaceDefaults(t *testing.T) {
	b := EffectiveMouseBindings(map[string]string{"me-accept-entry": "MouseDPrimary"})
	if k, _ := keysOf(b, "me-accept-entry"); k != "MouseDPrimary" {
		t.Errorf("me-accept-entry = %q", k)
	}
	if k, _ := keysOf(b, "ml-row-down"); k != "ScrollDown" {
		t.Errorf("ml-row-down = %q", k)
	}
}

func TestMergeOverridesLaterWins(t *testing.T) {
	got := MergeOverrides(map[string]string{"a": "1", "b": "1"}, map[string]string{"b": "2"})
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("got %v", got)
	}
}

func TestABareButtonIsASinglePress(t *testing.T) {
	b, ok := ParseMouse("MousePrimary")
	if !ok || b.Button != MousePrimary || b.Double || b.Modifiers != 0 {
		t.Errorf("got %+v %v", b, ok)
	}
}

func TestTheDPrefixIsADoublePress(t *testing.T) {
	if b, ok := ParseMouse("MouseDPrimary"); !ok || b.Button != MousePrimary || !b.Double {
		t.Errorf("got %+v", b)
	}
	b, ok := ParseMouse("Control+MouseDPrimary")
	if !ok || !b.Double || b.Modifiers != MouseControl {
		t.Errorf("got %+v", b)
	}
}

func TestEveryDocumentedButtonParses(t *testing.T) {
	for name, want := range map[string]MouseButton{
		"MousePrimary": MousePrimary, "MouseSecondary": MouseSecondary, "MouseMiddle": MouseMiddle,
		"MouseForward": MouseForward, "MouseBack": MouseBack, "MouseExtra7": 7,
	} {
		if b, ok := ParseMouse(name); !ok || b.Button != want || b.Double {
			t.Errorf("%s = %+v %v", name, b, ok)
		}
	}
	for name, want := range map[string]ScrollDirection{
		"ScrollUp": ScrollUp, "ScrollDown": ScrollDown, "ScrollLeft": ScrollLeft, "ScrollRight": ScrollRight,
	} {
		if b, ok := ParseMouse(name); !ok || b.Scroll != want || b.Button != 0 {
			t.Errorf("%s = %+v %v", name, b, ok)
		}
	}
}

func TestAKeyNameIsNotAMouseBinding(t *testing.T) {
	for _, spec := range []string{"Return", "Control+space", "", "MouseThird", "ScrollSideways", "MouseExtra", "MouseD", "Control+Shift"} {
		if _, ok := ParseMouse(spec); ok {
			t.Errorf("%q must not parse", spec)
		}
	}
}

func TestAMouseListKeepsWhatParses(t *testing.T) {
	got := ParseMouseList("MousePrimary, nonsense ,ScrollDown")
	if len(got) != 2 || got[0].Button != MousePrimary || got[1].Scroll != ScrollDown {
		t.Errorf("got %+v", got)
	}
	if len(ParseMouseList(" , ")) != 0 {
		t.Error("an empty list binds nothing")
	}
}
