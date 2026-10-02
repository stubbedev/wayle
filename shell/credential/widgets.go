package credential

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// The small painters the lock screen and the greeter compose their
// surfaces from: gelm's stylesheet is process-wide (the bar owns it),
// so the _lock.scss boxes are drawn directly.

func clamp(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}

// Panel is a padded, rounded, filled card around one child.
type Panel struct {
	widget.Base
	child   widget.Widget
	padding int
	radius  int
	color   render.Color
}

// NewPanel wraps child in a card.
func NewPanel(child widget.Widget, padding, radius int, color render.Color) *Panel {
	return &Panel{child: child, padding: padding, radius: radius, color: color}
}

// Measure grows the child's natural size by the padding.
func (p *Panel) Measure(con widget.Constraints) widget.Size {
	pad := 2 * p.padding
	inner := widget.Constraints{Max: widget.Size{W: max(con.Max.W-pad, 0), H: max(con.Max.H-pad, 0)}}
	sz := p.child.Measure(inner)
	return clamp(widget.Size{W: sz.W + pad, H: sz.H + pad}, con)
}

// Arrange lays the child out inside the padding.
func (p *Panel) Arrange(r render.Rect) {
	p.ArrangeSelf(r)
	p.child.Arrange(render.Rect{
		X: r.X + p.padding, Y: r.Y + p.padding,
		W: max(r.W-2*p.padding, 0), H: max(r.H-2*p.padding, 0),
	})
	widget.SetParents(p, p.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (p *Panel) ArrangeRoot(r render.Rect) { p.Arrange(r) }

// Paint fills the card, then the child.
func (p *Panel) Paint(cv *render.Canvas) {
	cv.RoundedRect(p.Bounds(), p.radius, p.color)
	p.child.Paint(cv)
}

// HitTest prefers the child; the padding belongs to the card.
func (p *Panel) HitTest(pt widget.Point) widget.Widget {
	if hit := p.child.HitTest(pt); hit != nil {
		return hit
	}
	return p.HitLeaf(p, pt)
}

// Children exposes the child for focus traversal.
func (p *Panel) Children() []widget.Widget { return []widget.Widget{p.child} }

// Spacer is a fixed empty box: a CSS margin between siblings.
type Spacer struct {
	widget.Base
	w, h int
}

// NewSpacer returns a w x h gap.
func NewSpacer(w, h int) *Spacer { return &Spacer{w: w, h: h} }

// Measure reports the fixed size.
func (s *Spacer) Measure(con widget.Constraints) widget.Size {
	return clamp(widget.Size{W: s.w, H: s.h}, con)
}

// Paint draws nothing.
func (s *Spacer) Paint(*render.Canvas) {}

// HitTest never claims input.
func (s *Spacer) HitTest(widget.Point) widget.Widget { return nil }

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
func (f *Fixed) Paint(cv *render.Canvas) { f.child.Paint(cv) }

// HitTest forwards to the child.
func (f *Fixed) HitTest(pt widget.Point) widget.Widget { return f.child.HitTest(pt) }

// Children exposes the child for focus traversal.
func (f *Fixed) Children() []widget.Widget { return []widget.Widget{f.child} }

// Fill paints its whole rect one color: the solid background, the
// image scrim, and the blank-screen blackout. A Fill that is off paints
// nothing and lets input through.
type Fill struct {
	widget.Base
	color render.Color
	on    bool
}

// NewFill returns a fill, shown.
func NewFill(color render.Color) *Fill { return &Fill{color: color, on: true} }

// SetOn shows or hides the fill.
func (f *Fill) SetOn(on bool) {
	if f.on == on {
		return
	}
	f.on = on
	f.Invalidate()
}

// On reports whether the fill paints.
func (f *Fill) On() bool { return f.on }

// Measure takes whatever it is given.
func (f *Fill) Measure(con widget.Constraints) widget.Size { return con.Min }

// Paint fills the bounds while on.
func (f *Fill) Paint(cv *render.Canvas) {
	if f.on {
		cv.FillRect(f.Bounds(), f.color)
	}
}

// HitTest claims input only while on: a blacked-out screen swallows
// the pointer, a transparent scrim never does.
func (f *Fill) HitTest(pt widget.Point) widget.Widget {
	if !f.on || f.color.A() == 0 {
		return nil
	}
	return f.HitLeaf(f, pt)
}
