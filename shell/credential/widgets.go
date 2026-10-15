package credential

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// Fixed and Inset pin a child's size and margins for the portal
// surfaces (the print dialog, the portal dialogs, the file chooser).

func clamp(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}

// Fixed pins a child's size: an entry's width-chars width, an avatar's
// 64x64. A zero axis keeps the child's natural extent.
type Fixed struct {
	widget.Base
	child widget.Widget
	w, h  int
}

// NewFixed wraps child at w x h (0 = natural).
func NewFixed(child widget.Widget, w, h int) *Fixed {
	return &Fixed{child: child, w: w, h: h}
}

// SetSize re-pins the size (a column dragged wider).
func (f *Fixed) SetSize(w, h int) {
	if f.w == w && f.h == h {
		return
	}
	f.w, f.h = w, h
	f.InvalidateLayout()
}

// Size is the pinned size.
func (f *Fixed) Size() (w, h int) { return f.w, f.h }

// Measure reports the pinned axes and the child's natural others.
func (f *Fixed) Measure(con widget.Constraints) widget.Size {
	inner := widget.Constraints{Min: widget.Size{W: f.w, H: f.h}, Max: con.Max}
	if f.w > 0 {
		inner.Max.W = f.w
	}
	if f.h > 0 {
		inner.Max.H = f.h
	}
	sz := f.child.Measure(inner)
	if f.w > 0 {
		sz.W = f.w
	}
	if f.h > 0 {
		sz.H = f.h
	}
	return clamp(sz, con)
}

// Arrange gives the child the whole rect.
func (f *Fixed) Arrange(r render.Rect) {
	f.ArrangeSelf(r)
	f.child.Arrange(r)
	widget.SetParents(f, f.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (f *Fixed) ArrangeRoot(r render.Rect) { f.Arrange(r) }

// Paint paints the child.
func (f *Fixed) Paint(cv *render.Canvas) { widget.PaintChild(cv, f.child) }

// HitTest forwards to the child.
func (f *Fixed) HitTest(pt widget.Point) widget.Widget { return f.child.HitTest(pt) }

// Children exposes the child for focus traversal.
func (f *Fixed) Children() []widget.Widget { return []widget.Widget{f.child} }

// Inset is a child inside fixed per-side margins: GTK's margin-*
// properties, which sit outside the stylesheet (a blanket
// `* { padding: 0 }` does not zero them).
type Inset struct {
	widget.Base
	child widget.Widget
	in    render.Insets
}

// NewInset wraps child in the margins.
func NewInset(child widget.Widget, in render.Insets) *Inset { return &Inset{child: child, in: in} }

// Measure adds the margins around the child's natural size.
func (s *Inset) Measure(con widget.Constraints) widget.Size {
	h, v := s.in.Left+s.in.Right, s.in.Top+s.in.Bottom
	sz := s.child.Measure(widget.Constraints{Max: widget.Size{W: max(con.Max.W-h, 0), H: max(con.Max.H-v, 0)}})
	return clamp(widget.Size{W: sz.W + h, H: sz.H + v}, con)
}

// Arrange lays the child out inside the margins.
func (s *Inset) Arrange(r render.Rect) {
	s.ArrangeSelf(r)
	s.child.Arrange(s.in.Shrink(r))
	widget.SetParents(s, s.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (s *Inset) ArrangeRoot(r render.Rect) { s.Arrange(r) }

// Paint paints the child.
func (s *Inset) Paint(cv *render.Canvas) { widget.PaintChild(cv, s.child) }

// HitTest forwards to the child; the margins take no input.
func (s *Inset) HitTest(pt widget.Point) widget.Widget { return s.child.HitTest(pt) }

// Children exposes the child for focus traversal.
func (s *Inset) Children() []widget.Widget { return []widget.Widget{s.child} }
