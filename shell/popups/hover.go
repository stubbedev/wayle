package popups

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// hoverCard wraps one popup card so the pointer entering and leaving
// it reaches the service: the card is the hit leaf, and the router's
// hover protocol (SetHovered) toggles the countdown pause — the Rust
// card's EventControllerMotion enter/leave pair
// (card/methods.rs setup_hover_controller).
type hoverCard struct {
	widget.Base
	inner   widget.Widget
	onHover func(on bool)
	hovered bool
}

func newHoverCard(inner widget.Widget, onHover func(on bool)) *hoverCard {
	return &hoverCard{inner: inner, onHover: onHover}
}

func (h *hoverCard) Measure(con widget.Constraints) widget.Size { return h.inner.Measure(con) }

func (h *hoverCard) Arrange(r render.Rect) {
	h.ArrangeSelf(r)
	widget.SetParents(h, h.inner)
	h.inner.Arrange(r)
}

func (h *hoverCard) Paint(cv *render.Canvas) { h.inner.Paint(cv) }

func (h *hoverCard) HitTest(p widget.Point) widget.Widget { return h.HitLeaf(h, p) }

// SetHovered implements widget.HoverSetter. Repeats of the same state
// are dropped, so the service sees strictly paired enter/leave calls.
func (h *hoverCard) SetHovered(on bool) {
	if on == h.hovered {
		return
	}
	h.hovered = on
	h.onHover(on)
}
