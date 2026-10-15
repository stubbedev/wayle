package widgets

import (
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// A centered child keeps its natural size in the middle of its slot;
// without Center it fills the slot.
func TestCenter(t *testing.T) {
	place := func(center bool) render.Rect {
		child := widget.NewDrawingArea(40, 20)
		if center {
			Center(child)
		}
		root := widget.NewOverlay().Append(child)
		root.Measure(widget.Constraints{Max: widget.Size{W: 200, H: 100}})
		root.Arrange(render.Rect{W: 200, H: 100})
		return child.Bounds()
	}
	if got := place(true); got != (render.Rect{X: 80, Y: 40, W: 40, H: 20}) {
		t.Errorf("centered child at %+v", got)
	}
	if got := place(false); got != (render.Rect{W: 200, H: 100}) {
		t.Errorf("plain child at %+v, want the whole slot", got)
	}
}
