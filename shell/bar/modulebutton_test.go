package bar

import (
	"os"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestParseClickActionForms(t *testing.T) {
	// click_action.rs's from_str cases.
	if action := config.MustClickAction(""); action.Kind != config.ClickNone {
		t.Errorf("empty = %+v, want none", action)
	}
	if action := config.MustClickAction("dropdown:battery"); action.Kind != config.ClickDropdown || action.Dropdown != "battery" {
		t.Errorf("dropdown = %+v", action)
	}
	if action := config.MustClickAction("brightness:5"); action.Kind != config.ClickBrightness || action.Brightness != 5 {
		t.Errorf("brightness delta = %+v", action)
	}
	if action := config.MustClickAction("brightness:-5"); action.Kind != config.ClickBrightness || action.Brightness != -5 {
		t.Errorf("negative delta = %+v", action)
	}
	if action := config.MustClickAction("brightness:toggle"); action.Kind != config.ClickBrightnessToggle {
		t.Errorf("toggle = %+v", action)
	}
	if action := config.MustClickAction("wayle audio output-mute"); action.Kind != config.ClickShell || action.Command != "wayle audio output-mute" {
		t.Errorf("shell = %+v", action)
	}
	// Round trip.
	for _, raw := range []string{"", "dropdown:battery", "brightness:-5", "brightness:toggle", "true"} {
		if got := config.MustClickAction(raw).String(); got != raw {
			t.Errorf("round trip %q -> %q", raw, got)
		}
	}
	if _, err := config.ParseClickAction("brightness:louder"); err == nil {
		t.Error("bad delta: want an error")
	}
}

func TestModuleClickDefaultsMatchSchema(t *testing.T) {
	cfg := config.Defaults()
	if got := cfg.Battery.Click.LeftClick.String(); got != "dropdown:battery" {
		t.Errorf("battery left-click = %q", got)
	}
	if got := cfg.Brightness.Click.ScrollUp.String(); got != "brightness:5" {
		t.Errorf("brightness scroll-up = %q", got)
	}
	if got := cfg.Brightness.Click.ScrollDown.String(); got != "brightness:-5" {
		t.Errorf("brightness scroll-down = %q", got)
	}
	if got := cfg.Volume.Click.MiddleClick.String(); got != "wayle audio output-mute" {
		t.Errorf("volume middle-click = %q", got)
	}
	if got := cfg.Microphone.Click.MiddleClick.String(); got != "wayle audio input-mute" {
		t.Errorf("microphone middle-click = %q", got)
	}
	if got := cfg.Clock.Click.RightClick.String(); got != "dropdown:weather" {
		t.Errorf("clock right-click = %q", got)
	}
	if cfg.KeyboardInput.Click.LeftClick.Kind != config.ClickNone {
		t.Errorf("keyboard-input left-click = %+v, want none", cfg.KeyboardInput.Click.LeftClick)
	}
}

// recordedActions captures what the wrapper dispatches.
type recordedActions struct {
	actions []config.ClickAction
}

func newWrappedModule(t *testing.T, binding config.ClickConfig) (*actionButton, *recordedActions) {
	t.Helper()
	rec := &recordedActions{}
	label := widget.NewLabel(testFont(t), 12, "x", 0xFF000000)
	wrapped := wrapActions(label, binding, nil, func(action config.ClickAction) {
		rec.actions = append(rec.actions, action)
	})
	button, ok := wrapped.(*actionButton)
	if !ok {
		t.Fatalf("wrapActions = %T, want an actionButton", wrapped)
	}
	return button, rec
}

func TestWrapActionsRoutesAllFiveBindings(t *testing.T) {
	binding := config.ClickConfig{
		LeftClick:   config.MustClickAction("left"),
		MiddleClick: config.MustClickAction("middle"),
		RightClick:  config.MustClickAction("right"),
		ScrollUp:    config.MustClickAction("up"),
		ScrollDown:  config.MustClickAction("down"),
	}
	button, rec := newWrappedModule(t, binding)

	button.inner.OnClick()
	button.PointerButton(widget.BTNMiddle)
	button.PointerButton(widget.BTNRight)
	button.ScrollInput(-120)
	button.ScrollInput(120)
	if len(rec.actions) != 5 {
		t.Fatalf("dispatched %+v, want five", rec.actions)
	}
	for i, want := range []string{"left", "middle", "right", "up", "down"} {
		if got := rec.actions[i].Command; got != want {
			t.Errorf("action %d = %q, want %q", i, got, want)
		}
	}
	// A declined scroll step (dy == 0) dispatches nothing.
	button.ScrollInput(0)
	if len(rec.actions) != 5 {
		t.Errorf("dy=0 dispatched %+v", rec.actions)
	}
}

func TestWrapActionsAlwaysWraps(t *testing.T) {
	label := widget.NewLabel(testFont(t), 12, "x", 0xFF000000)
	// Unbound modules still wrap: the button chrome applies either way.
	wrapped := wrapActions(label, config.ClickConfig{}, nil, func(config.ClickAction) {})
	btn, ok := wrapped.(*actionButton)
	if !ok {
		t.Fatalf("unbound module wrapped as %T, want the actionButton", wrapped)
	}
	// The unbound hooks pass through unconsumed.
	if btn.ScrollInput(1) {
		t.Error("an unbound button consumed scroll")
	}
	btn.PointerButton(widget.BTNRight)
	// And a styled wrap carries the chrome.
	bg := render.Color(0x112233FF)
	style := &barStyle{buttonBg: bg, buttonRadius: 6, buttonLabelPad: 3}
	styled := wrapActions(label, config.ClickConfig{}, style, func(config.ClickAction) {}).(*actionButton)
	if styled.inner.Bg != bg {
		t.Errorf("styled button bg = %#08x", uint32(styled.inner.Bg))
	}
	if got := styled.inner.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 50}}); got.W <= label.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 50}}).W {
		t.Errorf("padding not applied: %v", got)
	}
}

func TestLoadFileAppliesAndRejectsClickBindings(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	content := "[modules.battery]\nleft-click = \"brightness:toggle\"\nscroll-down = \"dropdown:battery\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Battery.Click.LeftClick.Kind != config.ClickBrightnessToggle {
		t.Errorf("left-click = %+v", c.Battery.Click.LeftClick)
	}
	if c.Battery.Click.ScrollDown.Dropdown != "battery" {
		t.Errorf("scroll-down = %+v", c.Battery.Click.ScrollDown)
	}

	if err := os.WriteFile(path, []byte("[modules.battery]\nscroll-up = \"brightness:sideways\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Error("bad delta: want a load error")
	}
}

// The real input path: gelm's Router hit-tests the tree and drives the
// hit leaf, so a routed left press must reach the binding and a hover
// must shade the button.
func TestWrapActionsRoutesThroughTheRouter(t *testing.T) {
	binding := config.ClickConfig{LeftClick: config.MustClickAction("left")}
	button, rec := newWrappedModule(t, binding)
	button.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 50}})
	button.Arrange(render.Rect{X: 0, Y: 0, W: 100, H: 30})
	router := &widget.Router{Root: button}
	p := widget.Point{X: 10, Y: 10}
	router.Move(p)
	if !button.inner.Hovered {
		t.Fatal("routed hover did not shade the button")
	}
	router.Press(widget.BTNLeft, p)
	if !button.inner.Pressed {
		t.Fatal("routed press did not shade the button")
	}
	router.Release(widget.BTNLeft, p)
	if len(rec.actions) != 1 || rec.actions[0].Command != "left" {
		t.Fatalf("routed left click dispatched %+v, want the left binding", rec.actions)
	}
}
