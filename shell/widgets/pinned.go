package widgets

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// Pinned holds a child at an exact size on each non-zero axis, the
// extent GTK reaches with a size request where the content measures
// smaller and a clip where it measures larger: the file chooser's
// resizable columns and HiDPI thumbnails, a portal header image. A zero
// axis keeps the child's natural extent. (Minimum sizes are gelm's
// SetSizeRequest; this is for boxes that must not grow.)
type Pinned struct {
	widget.Base
	child widget.Widget
	w, h  int
}

// NewPinned wraps child at w x h (0 = natural).
func NewPinned(child widget.Widget, w, h int) *Pinned {
	return &Pinned{child: child, w: w, h: h}
}

// SetSize re-pins the size (a column dragged wider).
func (f *Pinned) SetSize(w, h int) {
	if f.w == w && f.h == h {
		return
	}
	f.w, f.h = w, h
	f.InvalidateLayout()
}

// Size is the pinned size.
func (f *Pinned) Size() (w, h int) { return f.w, f.h }

// Measure reports the pinned axes and the child's natural others.
func (f *Pinned) Measure(con widget.Constraints) widget.Size {
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
func (f *Pinned) Arrange(r render.Rect) {
	f.ArrangeSelf(r)
	f.child.Arrange(r)
	widget.SetParents(f, f.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (f *Pinned) ArrangeRoot(r render.Rect) { f.Arrange(r) }

// Paint paints the child.
func (f *Pinned) Paint(cv *render.Canvas) { widget.PaintChild(cv, f.child) }

// HitTest forwards to the child.
func (f *Pinned) HitTest(pt widget.Point) widget.Widget { return f.child.HitTest(pt) }

// Children exposes the child for focus traversal.
func (f *Pinned) Children() []widget.Widget { return []widget.Widget{f.child} }

func clamp(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}
