package credential

import (
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

func TestFixedSetSizeRepinsAndInvalidates(t *testing.T) {
	f := NewFixed(NewSpacer(10, 10), 90, 0)
	con := widget.Constraints{Max: widget.Size{W: 500, H: 500}}
	if got := f.Measure(con); got.W != 90 || got.H != 10 {
		t.Fatalf("pinned width = %+v, want 90 x natural 10", got)
	}
	f.SetSize(120, 30)
	if w, h := f.Size(); w != 120 || h != 30 {
		t.Errorf("Size = %d x %d after SetSize", w, h)
	}
	if got := f.Measure(con); got.W != 120 || got.H != 30 {
		t.Errorf("re-pinned measure = %+v, want 120 x 30 (the cache must not answer)", got)
	}
	// Re-pinning to the same size is not a change.
	f.SetSize(120, 30)
	if got := f.Measure(con); got.W != 120 {
		t.Errorf("same-size SetSize measure = %+v", got)
	}
}

func TestInsetHoldsItsMargins(t *testing.T) {
	child := widget.NewSpacer(30, 10)
	s := NewInset(child, render.Insets{Top: 6, Right: 10, Bottom: 6, Left: 10})
	if got := s.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 500}}); got != (widget.Size{W: 50, H: 22}) {
		t.Errorf("measure = %+v, want the child plus its margins", got)
	}
	s.Arrange(render.Rect{X: 5, Y: 5, W: 100, H: 40})
	if got := child.Bounds(); got != (render.Rect{X: 15, Y: 11, W: 80, H: 28}) {
		t.Errorf("child = %+v, want inside the margins", got)
	}
	// A stylesheet zeroing padding everywhere leaves the margins.
	sheet := widget.NewStylesheet("* { padding: 0; margin: 0; }", widget.StylePriorityUser)
	root := widget.NewBox(widget.Column, 0, 0)
	root.AttachStylesheet(sheet)
	root.Append(s, false)
	if got := root.Measure(widget.Constraints{Max: widget.Size{W: 500, H: 500}}); got != (widget.Size{W: 50, H: 22}) {
		t.Errorf("under * { padding: 0 } = %+v", got)
	}
}

// The containers paint their child through widget.PaintChild, so the
// frame's focus ring draws right after a focused child, in tree order.
func TestContainersReportTheirChildToTheFocusRing(t *testing.T) {
	for name, wrap := range map[string]func(widget.Widget) widget.Widget{
		"Fixed": func(c widget.Widget) widget.Widget { return NewFixed(c, 10, 10) },
		"Inset": func(c widget.Widget) widget.Widget { return NewInset(c, render.Insets{Top: 1}) },
		"Panel": func(c widget.Widget) widget.Widget { return NewPanel(c, 1, 0, 0) },
	} {
		child := widget.NewSpacer(10, 10)
		w := wrap(child)
		w.Measure(widget.Constraints{Max: widget.Size{W: 20, H: 20}})
		w.Arrange(render.Rect{W: 20, H: 20})
		cv := render.New(make([]byte, render.Stride(20)*20), render.Stride(20), 20, 20)
		drawn := false
		cv.MarkFocus(child, func(*render.Canvas) { drawn = true })
		w.Paint(cv)
		if !drawn {
			t.Errorf("%s did not report its child", name)
		}
		cv.FinishFocus()
	}
}
