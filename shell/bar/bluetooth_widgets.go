package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// btSurface paints a rounded fill behind one child, optionally at a
// fixed size: the dropdown's popover frame, cards, and icon wells (the
// Card/background rules of the Rust SCSS, which gelm's CSS subset
// cannot express).
type btSurface struct {
	widget.Base
	child  widget.Widget
	bg     render.Color
	radius int
	// w, h fix the size when non-zero (set_width/height_request).
	w, h int
}

func newBtSurface(child widget.Widget, bg render.Color, radius int) *btSurface {
	return &btSurface{child: child, bg: bg, radius: radius}
}

// Measure is the fixed size where set, else the child's.
func (s *btSurface) Measure(con widget.Constraints) widget.Size {
	inner := con
	if s.w > 0 {
		inner.Max.W = min(s.w, con.Max.W)
		inner.Min.W = inner.Max.W
	}
	if s.h > 0 {
		inner.Max.H = min(s.h, con.Max.H)
		inner.Min.H = inner.Max.H
	}
	sz := s.child.Measure(inner)
	if s.w > 0 {
		sz.W = inner.Max.W
	}
	if s.h > 0 {
		sz.H = inner.Max.H
	}
	return clampSize(sz, con)
}

// Arrange gives the child the whole rect.
func (s *btSurface) Arrange(r render.Rect) {
	s.Base.Arrange(r)
	s.child.Arrange(r)
	widget.SetParents(s, s.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (s *btSurface) ArrangeRoot(r render.Rect) { s.Arrange(r) }

// Paint fills, then paints the child.
func (s *btSurface) Paint(cv *render.Canvas) {
	if !widget.IsVisible(s) {
		return
	}
	if s.bg != 0 {
		cv.RoundedRect(s.Bounds(), s.radius, s.bg)
	}
	widget.PaintChild(cv, s.child)
}

// HitTest prefers the child, else the surface itself.
func (s *btSurface) HitTest(p widget.Point) widget.Widget {
	if !widget.IsVisible(s) {
		return nil
	}
	if hit := s.child.HitTest(p); hit != nil {
		return hit
	}
	return s.HitLeaf(s, p)
}

// Children exposes the child for traversal.
func (s *btSurface) Children() []widget.Widget { return []widget.Widget{s.child} }

// btDeviceRow is one device row (device_item): the row box carries the
// classes and the stylesheet paints it — the available :hover, the
// padding, the first-child radii. The whole row is the click target
// (SetOnClickWithin; the action buttons are clicking descendants and
// keep their own), and hovering swaps the status label for the actions
// (SetOnHoverWithin; the buttons are descendants, so moving onto one
// holds the swap).
type btDeviceRow struct {
	*widget.Box
	// status and actions are the hover stack's two pages.
	slot   *widget.Stack
	status *widget.Label
	// statusShown is status_visible: connected, paired, or pending.
	statusShown bool
	actions     *widget.Box
	// hoverSwaps is set for my devices: only they carry the swap.
	hoverSwaps bool
	pending    bool

	onClick func()
}

// syncSlot shows the actions while hovered and idle, else the status
// (set_visible_child_name).
func (r *btDeviceRow) syncSlot() {
	if r.actions == nil {
		return
	}
	r.status.SetVisible(r.statusShown)
	if r.hoverSwaps && r.HoverWithin() && !r.pending {
		r.slot.Show(btSlotActions)
	} else {
		r.slot.Show(btSlotStatus)
	}
}

// The hover stack's pages.
const (
	btSlotStatus  = "status"
	btSlotActions = "actions"
)
