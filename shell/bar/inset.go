package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// inset wraps one child with per-side padding. gelm's Box padding is
// uniform, but the bar's sections carry the wayle margin model: padding
// on the cross axis and padding-ends on the outer end only, so the
// sections need real per-side insets.
type inset struct {
	widget.Base
	child                    widget.Widget
	left, top, right, bottom int
}

func newInset(child widget.Widget, left, top, right, bottom int) *inset {
	return &inset{child: child, left: left, top: top, right: right, bottom: bottom}
}

// Measure reports the child's natural size grown by the insets,
// clamped to the constraints.
func (in *inset) Measure(con widget.Constraints) widget.Size {
	inner := widget.Constraints{
		Min: con.Min,
		Max: widget.Size{
			W: max(con.Max.W-in.left-in.right, 0),
			H: max(con.Max.H-in.top-in.bottom, 0),
		},
	}
	sz := in.child.Measure(inner)
	return clampSize(widget.Size{W: sz.W + in.left + in.right, H: sz.H + in.top + in.bottom}, con)
}

// MinSize grows the child's squeeze floor by the insets.
func (in *inset) MinSize() widget.Size {
	min, ok := in.child.(widget.MinSizer)
	if !ok {
		return widget.Size{}
	}
	sz := min.MinSize()
	return widget.Size{W: sz.W + in.left + in.right, H: sz.H + in.top + in.bottom}
}

// Arrange lays the child out inside the inset rect.
func (in *inset) Arrange(r render.Rect) {
	in.Base.Arrange(r)
	in.child.Arrange(in.inner(r))
	widget.SetParents(in, in.child)
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (in *inset) ArrangeRoot(r render.Rect) { in.Arrange(r) }

// Paint is transparent: the insets show whatever sits behind (the bar
// background on the root box).
func (in *inset) Paint(cv *render.Canvas) { in.child.Paint(cv) }

// HitTest prefers the child; presses over the padding land on the inset
// itself, like a Box's padding.
func (in *inset) HitTest(p widget.Point) widget.Widget {
	if hit := in.child.HitTest(p); hit != nil {
		return hit
	}
	return in.HitLeaf(in, p)
}

// Children exposes the child for focus traversal.
func (in *inset) Children() []widget.Widget { return []widget.Widget{in.child} }

func (in *inset) inner(r render.Rect) render.Rect {
	return render.Rect{
		X: r.X + in.left,
		Y: r.Y + in.top,
		W: max(r.W-in.left-in.right, 0),
		H: max(r.H-in.top-in.bottom, 0),
	}
}

// clampSize confines s to the constraint box.
func clampSize(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}
