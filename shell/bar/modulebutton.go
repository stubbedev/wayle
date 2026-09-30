package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// actionButton wraps a module's root so the five config bindings
// (left/middle/right click, scroll up/down) route through the gelm
// input hooks. The primary click keeps Button's OnClick protocol; the
// other four arrive through the router's PointerButtonHandler and
// ScrollInputHandler. With no bindings at all the wrapper hands the
// hooks back unconsumed, so scroll containers keep scrolling through
// inert modules.
type actionButton struct {
	widget.Base
	inner   *widget.Button
	binding config.ClickConfig
	onRun   func(config.ClickAction)
}

// wrapActions wraps the module root in the styled button. The chrome
// (bg states, rounding, paddings) applies with or without bindings -
// the Rust BarButton is styled either way - but with all five bindings
// empty the wrapper hands the input hooks back unconsumed so scroll
// containers keep scrolling through inert modules.
func wrapActions(root widget.Widget, binding config.ClickConfig, style *barStyle, onRun func(config.ClickAction)) widget.Widget {
	bound := binding.LeftClick.Kind != config.ClickNone ||
		binding.MiddleClick.Kind != config.ClickNone ||
		binding.RightClick.Kind != config.ClickNone ||
		binding.ScrollUp.Kind != config.ClickNone ||
		binding.ScrollDown.Kind != config.ClickNone
	var inner *widget.Button
	if style != nil {
		inner = widget.NewButton(root, style.buttonLabelPad, style.buttonRadius)
		inner.Bg = style.buttonBg
		inner.BgHover = style.buttonBgHover
		inner.BgPressed = style.buttonBgActive
	} else {
		inner = widget.NewButton(root, 0, 0)
	}
	button := &actionButton{binding: binding, onRun: onRun, inner: inner}
	button.inner.OnClick = func() { onRun(binding.LeftClick) }
	if !bound {
		button.binding = config.ClickConfig{}
	}
	return button
}

func (a *actionButton) Measure(con widget.Constraints) widget.Size { return a.inner.Measure(con) }

func (a *actionButton) Arrange(r render.Rect) {
	a.ArrangeSelf(r)
	widget.SetParents(a, a.inner)
	a.inner.Arrange(r)
}

func (a *actionButton) Paint(cv *render.Canvas) { a.inner.Paint(cv) }

func (a *actionButton) HitTest(p widget.Point) widget.Widget { return a.HitLeaf(a, p) }

// The wrapper is the router's hit leaf, so the primary-button protocol
// (hover and pressed shades, the click) forwards to the inner button.

// SetHovered implements widget.HoverSetter.
func (a *actionButton) SetHovered(on bool) { a.inner.SetHovered(on) }

// SetPressed implements widget.PressSetter.
func (a *actionButton) SetPressed(on bool) { a.inner.SetPressed(on) }

// ClickAt implements widget.Clicker: the left binding.
func (a *actionButton) ClickAt(p widget.Point) { a.inner.ClickAt(p) }

// PointerButton routes middle and right presses; with no bindings the
// hooks pass through unconsumed.
func (a *actionButton) PointerButton(button uint32) {
	if a.binding.MiddleClick.Kind == config.ClickNone && a.binding.RightClick.Kind == config.ClickNone {
		return
	}
	switch button {
	case widget.BTNMiddle:
		a.onRun(a.binding.MiddleClick)
	case widget.BTNRight:
		a.onRun(a.binding.RightClick)
	}
}

// ScrollInput maps vertical steps to the up/down bindings; positive dy
// scrolls down, matching the wire convention Axis feeds ScrollBy.
func (a *actionButton) ScrollInput(dy int) bool {
	if a.binding.ScrollUp.Kind == config.ClickNone && a.binding.ScrollDown.Kind == config.ClickNone {
		return false
	}
	switch {
	case dy > 0:
		a.onRun(a.binding.ScrollDown)
	case dy < 0:
		a.onRun(a.binding.ScrollUp)
	default:
		return false
	}
	return true
}
