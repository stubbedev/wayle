package bar

import (
	"strings"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// barButton is the port of the Rust BarButton
// (crates/wayle-widgets/src/components/bar_buttons/component.rs): the
// same widget tree and classes, so the compiled bar-button SCSS styles
// it as it styles the GTK one —
//
//	menubutton.bar-button.<variant>[.icon-only][.label-only][.vertical][.border-*][.icon-end]
//	└ button.toggle
//	  └ box.bar-button-content
//	    ├ box.icon-container > image
//	    └ box.label-container > label.bar-button-label
//
// with the module's colors injected as the per-button custom
// properties (styling.ButtonCSS) on the menubutton. The five click and
// scroll bindings route through the toggle, the router's hit leaf.
type barButton struct {
	*widget.Box // menubutton.bar-button

	toggle   *barToggle
	content  *widget.Box
	iconBox  *widget.Box
	labelBox *widget.Box
	icon     *widget.Icon
	label    *widget.Label

	cfg        *config.Config
	button     config.ButtonConfig
	thresholds config.ThresholdColors
	vertical   bool

	binding config.ClickConfig
	onRun   func(config.ClickAction)
}

// barToggle is the button.toggle node: a gelm Button that stays the hit
// leaf and routes the non-primary bindings to its bar button.
type barToggle struct {
	*widget.Button
	owner *barButton
}

// newBarButton builds the bar-button tree around a module's icon and
// label (either may be nil). The label hands its color to the
// stylesheet: the programmatic ink a module constructed it with would
// otherwise outrank the per-button --bar-btn-label-color.
func newBarButton(ctx ModuleContext, icon *widget.Icon, label *widget.Label) *barButton {
	b := newBarButtonShell(ctx)
	b.iconBox = widget.NewBox(widget.Row, 0, 0)
	b.iconBox.AddClass("icon-container")
	if icon != nil {
		icon.SetElement("image")
		b.icon = icon
		b.iconBox.Append(icon, true)
	}
	b.labelBox = widget.NewBox(widget.Row, 0, 0)
	b.labelBox.AddClass("label-container")
	if label != nil {
		label.AddClass("bar-button-label")
		label.SetColor(0)
		label.SetAlignment(render.AlignCenter)
		b.label = label
		b.labelBox.Append(label, true)
	}
	b.placeContainers()
	return b
}

// newBarButtonAround wraps arbitrary module content (a canvas, a row of
// its own) in the same button chrome, without the icon and label
// containers.
func newBarButtonAround(ctx ModuleContext, content widget.Widget) *barButton {
	b := newBarButtonShell(ctx)
	b.content.Append(content, true)
	return b
}

// newBarButtonShell builds the menubutton > button.toggle >
// box.bar-button-content spine.
func newBarButtonShell(ctx ModuleContext) *barButton {
	b := &barButton{cfg: ctx.Config}
	if ctx.Config != nil {
		b.vertical = ctx.Config.Bar.Location.IsVertical()
		b.button = config.DefaultsButton(config.ButtonColors{}, config.TokenFgDefault, true, 0)
	}
	axis := widget.Row
	if b.vertical {
		axis = widget.Column
	}
	b.content = widget.NewBox(axis, 0, 0)
	b.content.AddClass("bar-button-content")
	b.toggle = &barToggle{Button: widget.NewButton(b.content, 0, 0), owner: b}
	b.toggle.SetElement("button")
	b.toggle.AddClass("toggle")
	b.toggle.OnClick = func() { b.run(b.binding.LeftClick) }
	b.Box = widget.NewBox(widget.Row, 0, 0)
	b.SetElement("menubutton")
	b.Append(b.toggle, true)
	b.refresh()
	return b
}

// placeContainers orders the icon and label containers by the bar's
// icon position.
func (b *barButton) placeContainers() {
	b.content.Clear()
	if b.cfg != nil && b.cfg.Bar.ButtonIconPosition == config.IconEnd {
		b.content.Append(b.labelBox, b.vertical)
		b.content.Append(b.iconBox, false)
	} else {
		b.content.Append(b.iconBox, false)
		b.content.Append(b.labelBox, b.vertical)
	}
	b.syncVisibility()
}

// configure applies a module's button config and bindings; appendModule
// calls it once it knows which module the button belongs to.
func (b *barButton) configure(button config.ButtonConfig, binding config.ClickConfig, onRun func(config.ClickAction)) {
	b.button, b.binding, b.onRun = button, binding, onRun
	if b.label != nil && button.LabelMaxLength > 0 {
		b.label.SetEllipsize(widget.EllipsizeEnd)
	}
	b.refresh()
}

// SetThresholds applies threshold color overrides over the config
// colors (BarButtonInput::SetThresholdColors): the per-button variables
// rebuild when they changed.
func (b *barButton) SetThresholds(t config.ThresholdColors) {
	if thresholdsEqual(t, b.thresholds) {
		return
	}
	b.thresholds = t
	b.refresh()
}

// refresh recomputes the classes and the per-button variables.
func (b *barButton) refresh() {
	if b.cfg == nil {
		return
	}
	b.SetClasses(b.classes()...)
	b.SetInlineStyle(inlineDecls(styling.ButtonCSS(b.button, b.cfg.Bar, b.cfg.ColorExtractor.ThemeProvider, b.thresholds)))
	b.syncVisibility()
}

// classes is BarButton::css_classes: base, variant, and the modifiers.
func (b *barButton) classes() []string {
	bar := b.cfg.Bar
	out := []string{"bar-button", bar.ButtonVariant.CSSClass()}
	if !b.button.LabelShow {
		out = append(out, "icon-only")
	}
	if !b.button.IconShow {
		out = append(out, "label-only")
	}
	if b.vertical {
		out = append(out, "vertical")
	}
	if b.button.BorderShow {
		if c, ok := bar.ButtonBorderLocation.CSSClass(); ok {
			out = append(out, c)
		}
	}
	if c, ok := bar.ButtonIconPosition.CSSClass(); ok {
		out = append(out, c)
	}
	return out
}

// syncVisibility hides the icon container when the icon is off and the
// label container when there is no label to show — the Rust
// has_label rule, which also drops the icon-label gap margin.
func (b *barButton) syncVisibility() {
	if b.iconBox != nil {
		b.iconBox.SetVisible(b.button.IconShow && b.icon != nil)
	}
	if b.labelBox != nil {
		b.labelBox.SetVisible(b.button.LabelShow && b.label != nil && strings.TrimSpace(b.label.Text()) != "")
	}
}

// Measure re-derives the container visibility first: modules set label
// text directly, and an emptied label must drop its container.
func (b *barButton) Measure(con widget.Constraints) widget.Size {
	b.syncVisibility()
	return b.Box.Measure(con)
}

// run dispatches one binding; unbound (ClickNone) actions do nothing.
func (b *barButton) run(action config.ClickAction) {
	if action.Kind == config.ClickNone || b.onRun == nil {
		return
	}
	b.onRun(action)
}

// bound reports whether any of the five bindings is set.
func (b *barButton) bound() bool {
	c := b.binding
	return c.LeftClick.Kind != config.ClickNone || c.MiddleClick.Kind != config.ClickNone ||
		c.RightClick.Kind != config.ClickNone || c.ScrollUp.Kind != config.ClickNone ||
		c.ScrollDown.Kind != config.ClickNone
}

// HitTest keeps the toggle the hit leaf, so the router drives its hover,
// press, and click, and the non-primary hooks below.
func (t *barToggle) HitTest(p widget.Point) widget.Widget {
	if t.Button.HitTest(p) != nil {
		return t
	}
	return nil
}

// PointerButton routes middle and right presses; with no bindings the
// hooks pass through unconsumed.
func (t *barToggle) PointerButton(button uint32) {
	switch button {
	case widget.BTNMiddle:
		t.owner.run(t.owner.binding.MiddleClick)
	case widget.BTNRight:
		t.owner.run(t.owner.binding.RightClick)
	}
}

// ScrollInput maps vertical steps to the up/down bindings; positive dy
// scrolls down, matching the wire convention Axis feeds ScrollBy. With
// neither scroll binding set the step passes through, so scroll
// containers keep scrolling through inert modules.
func (t *barToggle) ScrollInput(dy int) bool {
	c := t.owner.binding
	if c.ScrollUp.Kind == config.ClickNone && c.ScrollDown.Kind == config.ClickNone {
		return false
	}
	switch {
	case dy > 0:
		t.owner.run(c.ScrollDown)
	case dy < 0:
		t.owner.run(c.ScrollUp)
	default:
		return false
	}
	return true
}

// buttonRef lets a module reach the bar button appendModule builds
// around its root, for the threshold overrides a BarButton takes as
// input (BarButtonInput::SetThresholdColors). Embed it; overrides set
// before the button exists apply when it is attached.
type buttonRef struct {
	btn     *barButton
	pending config.ThresholdColors
}

// setButton attaches the module's button and applies the pending
// overrides.
func (r *buttonRef) setButton(b *barButton) {
	r.btn = b
	b.SetThresholds(r.pending)
}

// thresholds evaluates value against the module's thresholds
// (evaluate_thresholds) and hands the matching colors to the button.
func (r *buttonRef) thresholds(value float64, entries []config.ThresholdEntry) {
	r.pending = config.EvaluateThresholds(value, entries)
	if r.btn != nil {
		r.btn.SetThresholds(r.pending)
	}
}

// inlineDecls strips a `selector { ... }` rule down to its declaration
// list: the Rust per-widget providers are `* { ... }` rules, which in
// gelm are the widget's inline declarations.
func inlineDecls(rule string) string {
	open, end := strings.IndexByte(rule, '{'), strings.LastIndexByte(rule, '}')
	if open < 0 || end < open {
		return rule
	}
	return strings.TrimSpace(rule[open+1 : end])
}

// thresholdsEqual compares two override sets by value.
func thresholdsEqual(a, b config.ThresholdColors) bool {
	eq := func(x, y *config.ColorValue) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return eq(a.IconColor, b.IconColor) && eq(a.LabelColor, b.LabelColor) &&
		eq(a.IconBgColor, b.IconBgColor) && eq(a.ButtonBgColor, b.ButtonBgColor) &&
		eq(a.BorderColor, b.BorderColor)
}
