package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// borderPainter paints per-side border strips over a surface. gelm's
// stylesheet border strokes a widget's whole outline uniformly, but the
// bar's border-location picks single edges (bar/_container.scss's
// border-top-width and friends), so the bar paints its own.
type borderPainter struct {
	widget.Base
	widths borderWidths
	color  render.Color
}

func newBorder(widths borderWidths, color render.Color) *borderPainter {
	return &borderPainter{widths: widths, color: color}
}

// Measure contributes nothing: the strips overlay content instead of
// reserving space, exactly like a CSS border on the container.
func (b *borderPainter) Measure(con widget.Constraints) widget.Size {
	return con.Min
}

// Arrange records the covered rect.
func (b *borderPainter) Arrange(r render.Rect)     { b.Base.Arrange(r) }
func (b *borderPainter) ArrangeRoot(r render.Rect) { b.Base.Arrange(r) }

// Paint draws one strip per edged side, inside the bounds.
func (b *borderPainter) Paint(cv *render.Canvas) {
	r := b.Bounds()
	if b.widths.left > 0 {
		cv.RoundedRect(render.Rect{X: r.X, Y: r.Y, W: b.widths.left, H: r.H}, 0, b.color)
	}
	if b.widths.top > 0 {
		cv.RoundedRect(render.Rect{X: r.X, Y: r.Y, W: r.W, H: b.widths.top}, 0, b.color)
	}
	if b.widths.right > 0 {
		cv.RoundedRect(render.Rect{X: r.X + r.W - b.widths.right, Y: r.Y, W: b.widths.right, H: r.H}, 0, b.color)
	}
	if b.widths.bottom > 0 {
		cv.RoundedRect(render.Rect{X: r.X, Y: r.Y + r.H - b.widths.bottom, W: r.W, H: b.widths.bottom}, 0, b.color)
	}
}

// HitTest always misses: the strips are chrome over the content, and a
// hit here would shadow the widgets underneath in the overlay.
func (b *borderPainter) HitTest(widget.Point) widget.Widget { return nil }
