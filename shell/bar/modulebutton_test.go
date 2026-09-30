package bar

import (
	"os"
	"strings"
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

// recordedActions captures what the button dispatches.
type recordedActions struct {
	actions []config.ClickAction
}

func newWrappedModule(t *testing.T, binding config.ClickConfig) (*barButton, *recordedActions) {
	t.Helper()
	rec := &recordedActions{}
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	label := widget.NewLabel(testFont(t), 12, "x", 0xFF000000)
	button := asBarButton(ctx, label)
	button.configure(cfg.Battery.Button, binding, func(action config.ClickAction) {
		rec.actions = append(rec.actions, action)
	})
	return button, rec
}

func TestBarButtonRoutesAllFiveBindings(t *testing.T) {
	binding := config.ClickConfig{
		LeftClick:   config.MustClickAction("left"),
		MiddleClick: config.MustClickAction("middle"),
		RightClick:  config.MustClickAction("right"),
		ScrollUp:    config.MustClickAction("up"),
		ScrollDown:  config.MustClickAction("down"),
	}
	button, rec := newWrappedModule(t, binding)

	button.toggle.OnClick()
	button.toggle.PointerButton(widget.BTNMiddle)
	button.toggle.PointerButton(widget.BTNRight)
	button.toggle.ScrollInput(-120)
	button.toggle.ScrollInput(120)
	if len(rec.actions) != 5 {
		t.Fatalf("dispatched %+v, want five", rec.actions)
	}
	for i, want := range []string{"left", "middle", "right", "up", "down"} {
		if got := rec.actions[i].Command; got != want {
			t.Errorf("action %d = %q, want %q", i, got, want)
		}
	}
	// A declined scroll step (dy == 0) dispatches nothing.
	button.toggle.ScrollInput(0)
	if len(rec.actions) != 5 {
		t.Errorf("dy=0 dispatched %+v", rec.actions)
	}
}

func TestUnboundBarButtonPassesInputThrough(t *testing.T) {
	button, rec := newWrappedModule(t, config.ClickConfig{})
	if button.toggle.ScrollInput(1) {
		t.Error("an unbound button consumed scroll")
	}
	button.toggle.PointerButton(widget.BTNRight)
	button.toggle.OnClick()
	if len(rec.actions) != 0 || button.bound() {
		t.Errorf("unbound button dispatched %+v", rec.actions)
	}
}

func TestBarButtonTreeAndClasses(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	label := widget.NewLabel(testFont(t), 12, "42%", 0xFF112233)
	icon := widget.NewThemeIcon("x-symbolic", 16)
	b := newBarButton(ctx, icon, label)
	b.configure(cfg.Battery.Button, config.ClickConfig{}, nil)
	if b.Element() != "menubutton" || b.toggle.Element() != "button" || !b.toggle.HasClass("toggle") {
		t.Fatal("the menubutton > button.toggle spine")
	}
	if !b.content.HasClass("bar-button-content") || !b.iconBox.HasClass("icon-container") ||
		!b.labelBox.HasClass("label-container") || !label.HasClass("bar-button-label") || icon.Element() != "image" {
		t.Fatal("content, containers, image, and label classes")
	}
	if label.Color() != 0 {
		t.Error("the label kept its programmatic ink over --bar-btn-label-color")
	}
	for _, c := range []string{"bar-button", cfg.Bar.ButtonVariant.CSSClass()} {
		if !b.HasClass(c) {
			t.Errorf("missing %q in %v", c, b.Classes())
		}
	}
	for _, c := range []string{"icon-only", "label-only", "vertical", "icon-end", "border-all"} {
		if b.HasClass(c) {
			t.Errorf("default button carries %q", c)
		}
	}
	if !strings.Contains(b.InlineStyle(), "--bar-btn-label-color: var(--yellow)") {
		t.Errorf("inline vars = %q, want the battery's label color", b.InlineStyle())
	}

	// The modifiers follow the config: variant, hidden label/icon, the
	// border class, the icon position, and a vertical bar.
	cfg.Bar.ButtonVariant = config.ButtonVariantBlockPrefix
	cfg.Bar.ButtonIconPosition = config.IconEnd
	cfg.Bar.Location = config.LocationLeft
	bc := cfg.Battery.Button
	bc.LabelShow, bc.IconShow, bc.BorderShow = false, false, true
	v := newBarButton(newTestContext(t, cfg), widget.NewThemeIcon("x", 16), widget.NewLabel(testFont(t), 12, "x", 0))
	v.configure(bc, config.ClickConfig{}, nil)
	for _, c := range []string{"block-prefix", "icon-only", "label-only", "vertical", "icon-end", "border-all"} {
		if !v.HasClass(c) {
			t.Errorf("modified button misses %q in %v", c, v.Classes())
		}
	}
	if v.content.Children()[0] != v.labelBox {
		t.Error("icon-end did not put the label container first")
	}
	if !strings.Contains(v.InlineStyle(), "--bar-btn-icon-color: var(--fg-on-accent)") {
		t.Errorf("block-prefix auto icon color = %q, want fg-on-accent", v.InlineStyle())
	}
}

func TestBarButtonThresholdsOverrideTheColors(t *testing.T) {
	cfg := config.Defaults()
	b := newBarButton(newTestContext(t, cfg), nil, widget.NewLabel(testFont(t), 12, "x", 0))
	b.configure(cfg.Battery.Button, config.ClickConfig{}, nil)
	before := b.InlineStyle()
	red := mustToken(config.TokenRed)
	b.SetThresholds(config.ThresholdColors{LabelColor: &red})
	if !strings.Contains(b.InlineStyle(), "--bar-btn-label-color: var(--red)") {
		t.Errorf("threshold label color = %q", b.InlineStyle())
	}
	b.SetThresholds(config.ThresholdColors{})
	if b.InlineStyle() != before {
		t.Error("clearing the thresholds did not restore the config colors")
	}
}

func TestBarButtonHidesAnEmptyLabelContainer(t *testing.T) {
	cfg := config.Defaults()
	label := widget.NewLabel(testFont(t), 12, "", 0)
	b := newBarButton(newTestContext(t, cfg), nil, label)
	b.configure(cfg.Battery.Button, config.ClickConfig{}, nil)
	b.Measure(widgetConstraintsMax(200, 50))
	if b.labelBox.Visible() {
		t.Error("an empty label kept its container (and its gap margin)")
	}
	label.SetText("50%")
	b.Measure(widgetConstraintsMax(200, 50))
	if !b.labelBox.Visible() {
		t.Error("a label with text stayed hidden")
	}
	if b.iconBox.Visible() {
		t.Error("a button without an icon shows the icon container")
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
// must shade the toggle.
func TestBarButtonRoutesThroughTheRouter(t *testing.T) {
	binding := config.ClickConfig{LeftClick: config.MustClickAction("left")}
	button, rec := newWrappedModule(t, binding)
	button.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 50}})
	button.Arrange(render.Rect{X: 0, Y: 0, W: 100, H: 30})
	router := &widget.Router{Root: button}
	p := widget.Point{X: 10, Y: 10}
	router.Move(p)
	if !button.toggle.Hovered {
		t.Fatal("routed hover did not reach the toggle")
	}
	router.Press(widget.BTNLeft, p)
	if !button.toggle.Pressed {
		t.Fatal("routed press did not reach the toggle")
	}
	router.Release(widget.BTNLeft, p)
	if len(rec.actions) != 1 || rec.actions[0].Command != "left" {
		t.Fatalf("routed left click dispatched %+v, want the left binding", rec.actions)
	}
}
