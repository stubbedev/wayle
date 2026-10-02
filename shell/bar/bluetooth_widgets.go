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

// btDeviceRow is one device row (device_item): the whole row is the
// click target (connect/disconnect), hovering a paired row swaps its
// status label for the action buttons, and presses over a visible
// action button go to that button.
type btDeviceRow struct {
	widget.Base
	box *widget.Box
	bg  render.Color
	// hoverBg paints under an available row while hovered
	// (.bluetooth-device.available:hover).
	hoverBg render.Color
	// status and actions are the hover stack's two pages.
	slot   *widget.Stack
	status *widget.Label
	// statusShown is status_visible: connected, paired, or pending.
	statusShown bool
	actions     *widget.Box
	// hoverSwaps is set for my devices: only they carry the hover
	// controller.
	hoverSwaps bool
	pending    bool

	rowHovered    bool
	actionHovered int
	onClick       func()
}

// SetHovered implements widget.HoverSetter.
func (r *btDeviceRow) SetHovered(on bool) {
	r.rowHovered = on
	r.syncSlot()
	r.Invalidate()
}

// hovered is the Rust hover state: over the row or one of its buttons.
func (r *btDeviceRow) hovered() bool { return r.rowHovered || r.actionHovered > 0 }

// syncSlot shows the actions while hovered and idle, else the status
// (set_visible_child_name).
func (r *btDeviceRow) syncSlot() {
	if r.actions == nil {
		return
	}
	r.status.SetVisible(r.statusShown)
	if r.hoverSwaps && r.hovered() && !r.pending {
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

// ClickAt implements widget.Clicker.
func (r *btDeviceRow) ClickAt(widget.Point) {
	if r.onClick != nil {
		r.onClick()
	}
}

// CursorName implements widget.CursorNamer (set_cursor_from_name).
func (r *btDeviceRow) CursorName() string { return "pointer" }

func (r *btDeviceRow) Measure(con widget.Constraints) widget.Size { return r.box.Measure(con) }

func (r *btDeviceRow) Arrange(rect render.Rect) {
	r.Base.Arrange(rect)
	r.box.Arrange(rect)
	widget.SetParents(r, r.box)
}

func (r *btDeviceRow) ArrangeRoot(rect render.Rect) { r.Arrange(rect) }

func (r *btDeviceRow) Paint(cv *render.Canvas) {
	if r.bg != 0 {
		cv.RoundedRect(r.Bounds(), 0, r.bg)
	}
	if r.hoverBg != 0 && r.rowHovered {
		cv.RoundedRect(r.Bounds(), 0, r.hoverBg)
	}
	widget.PaintChild(cv, r.box)
}

// HitTest routes to a visible action button, else claims the press for
// the row, so the labels inside never swallow the click.
func (r *btDeviceRow) HitTest(p widget.Point) widget.Widget {
	if !r.Bounds().Contains(p.X, p.Y) {
		return nil
	}
	if r.actions != nil && r.slot.Visible() == btSlotActions {
		if hit := r.actions.HitTest(p); hit != nil {
			return hit
		}
	}
	return r
}

func (r *btDeviceRow) Children() []widget.Widget { return []widget.Widget{r.box} }

// btActionButton is a row action that keeps its row hovered while the
// pointer is on it, so moving onto the button does not swap it away.
type btActionButton struct {
	*widget.Button
	row *btDeviceRow
}

// HitTest returns the wrapper, so the router's hover lands here.
func (b *btActionButton) HitTest(p widget.Point) widget.Widget {
	if b.Button.HitTest(p) != nil {
		return b
	}
	return nil
}

// SetHovered implements widget.HoverSetter.
func (b *btActionButton) SetHovered(on bool) {
	b.Button.SetHovered(on)
	if on {
		b.row.actionHovered++
	} else if b.row.actionHovered > 0 {
		b.row.actionHovered--
	}
	b.row.syncSlot()
}
