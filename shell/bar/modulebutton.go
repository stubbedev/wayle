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
	// font is the face the module's label renders in; frozen is the
	// stand-in shown while a dropdown holds the label still.
	font   render.Font
	frozen *widget.Label

	cfg        *config.Config
	button     config.ButtonConfig
	thresholds config.ThresholdColors
	vertical   bool

	binding config.ClickConfig
	onRun   func(action config.ClickAction, scroll bool)
	// ownClasses are the classes refresh last applied; classes a module
	// sets on its button (media-disc, the threshold states) are left
	// alone.
	ownClasses []string
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
	b.font = ctx.Font
	b.iconBox = widget.NewBox(widget.Row, 0, 0)
	b.iconBox.AddClass("icon-container")
	if icon != nil {
		b.icon = icon
		if b.vertical {
			b.iconBox.AppendAligned(icon, true, widget.AlignCenter)
		} else {
			b.iconBox.Append(icon, true)
		}
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

// freezeLabel is FreezeSize: while the button's dropdown is open the
// shown label stops following the module, so the button keeps its
// width and the popover its anchor. A copy of the label stands in;
// the module keeps updating its own, hidden one.
func (b *barButton) freezeLabel() {
	if b.label == nil || b.frozen != nil || b.font == nil {
		return
	}
	f := widget.NewLabel(b.font, b.label.SizePx(), b.label.Text(), b.label.Color())
	f.AddClass(b.label.Classes()...)
	f.SetAlignment(b.label.Alignment())
	f.SetEllipsize(b.label.Ellipsize())
	f.SetMaxWidthChars(b.label.MaxWidthChars())
	f.SetVisible(b.label.Visible())
	b.frozen = f
	b.labelBox.Clear()
	b.labelBox.Append(f, true)
}

// thawLabel is ThawSize: the live label returns, carrying whatever the
// module set meanwhile (pending_label).
func (b *barButton) thawLabel() {
	if b.frozen == nil {
		return
	}
	b.frozen = nil
	b.labelBox.Clear()
	b.labelBox.Append(b.label, true)
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
		b.button = config.ButtonConfig{IconShow: true, LabelShow: true, AutoIconColor: config.TokenFgDefault}
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
	b.Box = widget.NewBox(widget.Row, 0, 0)
	b.SetElement("menubutton")
	b.Append(b.toggle, true)
	b.refresh()
	return b
}

// placeContainers orders the icon and label containers by the bar's
// icon position. A vertical bar centers the icon across the button,
// the Rust component's hexpand + halign Center on the icon-only and
// vertical shapes.
func (b *barButton) placeContainers() {
	b.content.Clear()
	if b.cfg != nil && b.cfg.Bar.ButtonIconPosition == config.IconEnd {
		b.appendCross(b.labelBox)
		b.appendIcon()
	} else {
		b.appendIcon()
		b.appendCross(b.labelBox)
	}
	b.syncVisibility()
}

// appendIcon appends the icon container: in a vertical content it
// expands and centers (the glyph centered inside it, set at
// construction), a row just takes it at natural width.
func (b *barButton) appendIcon() {
	if b.vertical {
		b.content.AppendAligned(b.iconBox, true, widget.AlignCenter)
		return
	}
	b.content.Append(b.iconBox, false)
}

// appendCross appends a container in the content's cross position: in
// a vertical (column) content the free axis is horizontal, so the
// container expands and centers; a row just takes it.
func (b *barButton) appendCross(w widget.Widget) {
	if b.vertical {
		b.content.AppendAligned(w, true, widget.AlignCenter)
		return
	}
	b.content.Append(w, true)
}

// configure applies a module's button config and bindings; appendModule
// calls it once it knows which module the button belongs to.
func (b *barButton) configure(button config.ButtonConfig, binding config.ClickConfig, onRun func(action config.ClickAction, scroll bool)) {
	b.button, b.binding, b.onRun = button, binding, onRun
	// label-max-length is BarButtonBehavior's label_max_chars: the
	// label's width caps at that many characters and ellipsizes there.
	if b.label != nil {
		b.label.SetMaxWidthChars(button.LabelMaxLength)
		if button.LabelMaxLength > 0 {
			b.label.SetEllipsize(widget.EllipsizeEnd)
		} else {
			b.label.SetEllipsize(widget.EllipsizeNone)
		}
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
	b.RemoveClass(b.ownClasses...)
	b.ownClasses = b.classes()
	b.AddClass(b.ownClasses...)
	b.SetInlineStyle(inlineDecls(styling.ButtonCSS(b.button, b.cfg.Bar, b.cfg.Styling.ColorExtractor.ThemeProvider, b.thresholds)))
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
// run fires one binding; scroll marks the scroll-up/down ones (the custom
// module debounces its on-action for those).
func (b *barButton) run(action config.ClickAction, scroll bool) {
	if action.Kind == config.ClickNone || b.onRun == nil {
		return
	}
	b.onRun(action, scroll)
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

// SetPressed opens on the press, the Rust GestureClick's
// connect_pressed: the dropdown appears under the button-down, and a
// press that slides off before the release still opened it. OnClick
// stays unset, so the release cannot fire a second time.
func (t *barToggle) SetPressed(on bool) {
	was := t.Pressed
	t.Button.SetPressed(on)
	if on && !was {
		t.owner.run(t.owner.binding.LeftClick, false)
	}
}

// PointerButton routes middle and right presses; with no bindings the
// hooks pass through unconsumed.
func (t *barToggle) PointerButton(button uint32) {
	switch button {
	case widget.BTNMiddle:
		t.owner.run(t.owner.binding.MiddleClick, false)
	case widget.BTNRight:
		t.owner.run(t.owner.binding.RightClick, false)
	}
}

// ScrollInput maps vertical steps to the up/down bindings; positive dy
// scrolls down, matching the wire convention Axis feeds ScrollBy. The
// step is always consumed and the action always emitted, the Rust
// helper's Propagation::Stop — an unbound action no-ops in run, and
// content behind the bar never scrolls through a module.
func (t *barToggle) ScrollInput(dy int) bool {
	c := t.owner.binding
	switch {
	case dy > 0:
		t.owner.run(c.ScrollDown, true)
	case dy < 0:
		t.owner.run(c.ScrollUp, true)
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
