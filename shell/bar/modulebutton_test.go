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
	if action := config.ParseClickAction(""); action.Kind != config.ClickNone {
		t.Errorf("empty = %+v, want none", action)
	}
	if action := config.ParseClickAction("dropdown:battery"); action.Kind != config.ClickDropdown || action.Dropdown != "battery" {
		t.Errorf("dropdown = %+v", action)
	}
	if action := config.ParseClickAction("brightness:5"); action.Kind != config.ClickBrightness || action.Brightness != 5 {
		t.Errorf("brightness delta = %+v", action)
	}
	if action := config.ParseClickAction("brightness:-5"); action.Kind != config.ClickBrightness || action.Brightness != -5 {
		t.Errorf("negative delta = %+v", action)
	}
	if action := config.ParseClickAction("brightness:toggle"); action.Kind != config.ClickBrightnessToggle {
		t.Errorf("toggle = %+v", action)
	}
	if action := config.ParseClickAction("wayle audio output-mute"); action.Kind != config.ClickShell || action.Command != "wayle audio output-mute" {
		t.Errorf("shell = %+v", action)
	}
	// Round trip.
	for _, raw := range []string{"", "dropdown:battery", "brightness:-5", "brightness:toggle", "true"} {
		if got := config.ParseClickAction(raw).String(); got != raw {
			t.Errorf("round trip %q -> %q", raw, got)
		}
	}
	if got := config.ParseClickAction("brightness:louder"); got.Kind != config.ClickNone {
		t.Errorf("bad delta = %+v, want no action (click_action.rs from_str)", got)
	}
}

func TestModuleClickDefaultsMatchSchema(t *testing.T) {
	cfg := config.Defaults()
	if got := cfg.Battery.Clicks().LeftClick.String(); got != "dropdown:battery" {
		t.Errorf("battery left-click = %q", got)
	}
	if got := cfg.Brightness.Clicks().ScrollUp.String(); got != "brightness:5" {
		t.Errorf("brightness scroll-up = %q", got)
	}
	if got := cfg.Brightness.Clicks().ScrollDown.String(); got != "brightness:-5" {
		t.Errorf("brightness scroll-down = %q", got)
	}
	if got := cfg.Volume.Clicks().MiddleClick.String(); got != "wayle audio output-mute" {
		t.Errorf("volume middle-click = %q", got)
	}
	if got := cfg.Microphone.Clicks().MiddleClick.String(); got != "wayle audio input-mute" {
		t.Errorf("microphone middle-click = %q", got)
	}
	if got := cfg.Clock.Clicks().RightClick.String(); got != "dropdown:weather" {
		t.Errorf("clock right-click = %q", got)
	}
	if cfg.KeyboardInput.Clicks().LeftClick.Kind != config.ClickNone {
		t.Errorf("keyboard-input left-click = %+v, want none", cfg.KeyboardInput.Clicks().LeftClick)
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
	button.configure(batteryButton(cfg), binding, func(action config.ClickAction, _ bool) {
		rec.actions = append(rec.actions, action)
	})
	return button, rec
}

func TestBarButtonRoutesAllFiveBindings(t *testing.T) {
	binding := config.ClickConfig{
		LeftClick:   config.ParseClickAction("left"),
		MiddleClick: config.ParseClickAction("middle"),
		RightClick:  config.ParseClickAction("right"),
		ScrollUp:    config.ParseClickAction("up"),
		ScrollDown:  config.ParseClickAction("down"),
	}
	button, rec := newWrappedModule(t, binding)

	// The primary fires on the press, the Rust GestureClick's
	// connect_pressed; the release fires nothing more.
	button.toggle.SetPressed(true)
	button.toggle.SetPressed(false)
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

func TestUnboundBarButtonConsumesScroll(t *testing.T) {
	button, rec := newWrappedModule(t, config.ClickConfig{})
	// Scroll steps are always consumed (the Rust helper's
	// Propagation::Stop); the unbound action no-ops in run.
	if !button.toggle.ScrollInput(1) {
		t.Error("scroll passed through the bar button; content behind it scrolls")
	}
	button.toggle.PointerButton(widget.BTNRight)
	button.toggle.SetPressed(true)
	button.toggle.SetPressed(false)
	if len(rec.actions) != 0 || button.bound() {
		t.Errorf("unbound button dispatched %+v", rec.actions)
	}
}

// The module wrapper: `module` plus the module's own type class sit on
// a box ABOVE the menubutton, so ancestor selectors like
// `.media-disc menubutton ... image` match, the Rust factory's shape.
func TestModuleWrapsTheButtonInModuleClasses(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	row := CreateAll([]config.BarItem{{Module: "clock"}}, ctx)
	item := row.Children()[0].(*widget.Box).Children()[0].(*widget.Box)
	if !item.HasClass("module") || !item.HasClass("clock") {
		t.Fatalf("wrapper classes %v, want module + clock above the button", item.Classes())
	}
	type hasClass interface{ HasClass(string) bool }
	if b, ok := item.Children()[0].(hasClass); !ok || !b.HasClass("bar-button") {
		t.Errorf("the wrapper holds %T, want the bar button", item.Children()[0])
	}
}

func TestBarButtonTreeAndClasses(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	label := widget.NewLabel(testFont(t), 12, "42%", 0xFF112233)
	icon := widget.NewThemeIcon("x-symbolic", 16)
	b := newBarButton(ctx, icon, label)
	b.configure(batteryButton(cfg), config.ClickConfig{}, nil)
	b.Measure(widget.Constraints{Max: widget.Size{W: 400, H: 40}})
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
	cfg.Bar.ButtonVariant = config.ButtonBlockPrefix
	cfg.Bar.ButtonIconPosition = config.IconEnd
	cfg.Bar.Location = config.LocationLeft
	bc := batteryButton(cfg)
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
	b.configure(batteryButton(cfg), config.ClickConfig{}, nil)
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
	b.configure(batteryButton(cfg), config.ClickConfig{}, nil)
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
	if c.Battery.Clicks().LeftClick.Kind != config.ClickBrightnessToggle {
		t.Errorf("left-click = %+v", c.Battery.Clicks().LeftClick)
	}
	if c.Battery.Clicks().ScrollDown.Dropdown != "battery" {
		t.Errorf("scroll-down = %+v", c.Battery.Clicks().ScrollDown)
	}

	if err := os.WriteFile(path, []byte("[modules.battery]\nscroll-up = \"brightness:sideways\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Every string parses: a bad delta is no action (click_action.rs).
	if c, err := config.LoadFile(path); err != nil || c.Battery.Clicks().ScrollUp.Kind != config.ClickNone {
		t.Errorf("bad delta: want no action and no error, got %v", err)
	}
}

// The real input path: gelm's Router hit-tests the tree and drives the
// hit leaf, so a routed left press must reach the binding and a hover
// must shade the toggle.
func TestBarButtonRoutesThroughTheRouter(t *testing.T) {
	binding := config.ClickConfig{LeftClick: config.ParseClickAction("left")}
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

// batteryButton is the battery module's bar-button view.
func batteryButton(cfg *config.Config) config.ButtonConfig {
	b, _ := cfg.ModuleButton("battery")
	return b
}

// configure and later refreshes replace only the classes the button
// itself derives; a class the module put on its button stays.
func TestBarButtonKeepsTheModulesClasses(t *testing.T) {
	cfg := config.Defaults()
	b := newBarButton(newTestContext(t, cfg), widget.NewThemeIcon("x", 16), widget.NewLabel(testFont(t), 12, "x", 0))
	b.AddClass("media-disc")
	b.configure(config.ButtonConfig{IconShow: true, LabelShow: false}, config.ClickConfig{}, nil)
	if !b.HasClass("media-disc") || !b.HasClass("bar-button") || !b.HasClass("icon-only") {
		t.Fatalf("after configure: %v", b.Classes())
	}
	b.configure(config.ButtonConfig{IconShow: true, LabelShow: true}, config.ClickConfig{}, nil)
	if b.HasClass("icon-only") {
		t.Error("a stale derived class survived the refresh")
	}
	if !b.HasClass("media-disc") {
		t.Error("the refresh dropped the module's class")
	}
}

// The bar toggle is named by its label for assistive technology, as
// GTK names the Rust shell's menu button; a button with no label has
// no name to give.
func TestBarToggleAccessibleNameIsItsLabel(t *testing.T) {
	cfg := config.Defaults()
	b := newBarButton(newTestContext(t, cfg), widget.NewThemeIcon("x", 16), widget.NewLabel(testFont(t), 12, "12:30", 0))
	st := widget.Describe(b.toggle)
	if st.Role != widget.RoleButton || st.Name != "12:30" {
		t.Errorf("toggle = %s %q, want a button named by its label", st.Role, st.Name)
	}
	bare := newBarButtonAround(newTestContext(t, cfg), widget.NewBox(widget.Row, 0, 0))
	if got := widget.Describe(bare.toggle).Name; got != "" {
		t.Errorf("label-less toggle name = %q, want none", got)
	}
}

// While its dropdown is open the button's label holds still
// (FreezeSize); the module's updates land on thaw (pending_label).
func TestBarButtonLabelFreezesUntilThaw(t *testing.T) {
	cfg := config.Defaults()
	label := widget.NewLabel(testFont(t), 12, "12:30", 0)
	b := newBarButton(newTestContext(t, cfg), nil, label)
	b.freezeLabel()
	label.SetText("12:31")
	shown := b.labelBox.Children()
	if len(shown) != 1 || shown[0] == widget.Widget(label) || shown[0].(*widget.Label).Text() != "12:30" {
		t.Fatalf("frozen: shows %v, want the held 12:30", shown)
	}
	if !shown[0].(*widget.Label).HasClass("bar-button-label") {
		t.Error("the held label lost its class")
	}
	b.thawLabel()
	if shown = b.labelBox.Children(); len(shown) != 1 || shown[0] != widget.Widget(label) || label.Text() != "12:31" {
		t.Errorf("thawed: shows %v, want the live label at 12:31", shown)
	}
	// Thaw without a freeze changes nothing.
	b.thawLabel()
	if shown = b.labelBox.Children(); len(shown) != 1 || shown[0] != widget.Widget(label) {
		t.Error("a stray thaw moved the label")
	}
}
