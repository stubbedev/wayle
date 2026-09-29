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

// wrapActions wraps the module root when any binding is set; with all
// five empty the module root goes in bare, like the Rust component
// with an action-free module.
func wrapActions(root widget.Widget, binding config.ClickConfig, onRun func(config.ClickAction)) widget.Widget {
	if binding.LeftClick.Kind == config.ClickNone &&
		binding.MiddleClick.Kind == config.ClickNone &&
		binding.RightClick.Kind == config.ClickNone &&
		binding.ScrollUp.Kind == config.ClickNone &&
		binding.ScrollDown.Kind == config.ClickNone {
		return root
	}
	button := &actionButton{binding: binding, onRun: onRun}
	button.inner = widget.NewButton(root, 0, 0)
	button.inner.OnClick = func() { onRun(binding.LeftClick) }
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

// PointerButton routes middle and right presses.
func (a *actionButton) PointerButton(button uint32) {
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
