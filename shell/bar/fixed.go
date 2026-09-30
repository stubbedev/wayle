package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// fixedBox holds one child in a fixed logical box, whatever the
// child's natural size: an album cover that loads after the popover
// measured, or a 640px image shown as a thumbnail. The child is
// swappable, so the box keeps its place while the content changes.
type fixedBox struct {
	widget.Base
	w, h  int
	child widget.Widget
}

func newFixedBox(w, h int, child widget.Widget) *fixedBox {
	return &fixedBox{w: w, h: h, child: child}
}

// SetChild replaces the content and schedules a relayout.
func (f *fixedBox) SetChild(child widget.Widget) {
	f.child = child
	f.InvalidateLayout()
}

// Child returns the current content.
func (f *fixedBox) Child() widget.Widget { return f.child }

func (f *fixedBox) Measure(con widget.Constraints) widget.Size {
	return widget.Size{W: min(f.w, con.Max.W), H: min(f.h, con.Max.H)}
}

func (f *fixedBox) Arrange(r render.Rect) {
	f.ArrangeSelf(r)
	if f.child == nil {
		return
	}
	widget.SetParents(f, f.child)
	// Center the child at its natural size, capped by the box.
	sz := f.child.Measure(widget.Constraints{Max: widget.Size{W: r.W, H: r.H}})
	f.child.Arrange(render.Rect{X: r.X + (r.W-sz.W)/2, Y: r.Y + (r.H-sz.H)/2, W: sz.W, H: sz.H})
}

func (f *fixedBox) Paint(cv *render.Canvas) {
	if f.child != nil {
		f.child.Paint(cv)
	}
}

// Children exposes the content to the tree walks.
func (f *fixedBox) Children() []widget.Widget {
	if f.child == nil {
		return nil
	}
	return []widget.Widget{f.child}
}

func (f *fixedBox) HitTest(p widget.Point) widget.Widget {
	if f.child != nil {
		if hit := f.child.HitTest(p); hit != nil {
			return hit
		}
	}
	return nil
}
