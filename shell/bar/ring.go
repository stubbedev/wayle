package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// progressRing is ProgressRing: a circular gauge (a quarter-alpha track
// and the fraction clockwise from twelve o'clock, round-capped) with a
// centered label.
type progressRing struct {
	widget.Base
	size, stroke int
	fraction     float64
	color        render.Color
	label        *widget.Label
}

// newProgressRing builds a ring size logical pixels across with the
// given stroke width.
func newProgressRing(size, stroke int, font render.Font, px float64, ink render.Color) *progressRing {
	r := &progressRing{size: size, stroke: stroke}
	r.label = widget.NewLabel(font, px, "", ink)
	return r
}

// set updates the gauge: the fraction (clamped to 0..1), its label, and
// the fill color.
func (r *progressRing) set(fraction float64, label string, color render.Color) {
	r.fraction = math.Max(0, math.Min(1, fraction))
	r.color = color
	r.label.SetText(label)
	r.Invalidate()
}

func (r *progressRing) Measure(widget.Constraints) widget.Size {
	return widget.Size{W: r.size, H: r.size}
}

func (r *progressRing) Arrange(rect render.Rect) {
	r.ArrangeSelf(rect)
	widget.SetParents(r, r.label)
	sz := r.label.Measure(widget.Constraints{Max: widget.Size{W: rect.W, H: rect.H}})
	r.label.Arrange(render.Rect{X: rect.X + (rect.W-sz.W)/2, Y: rect.Y + (rect.H-sz.H)/2, W: sz.W, H: sz.H})
}

// Paint is draw_ring.
func (r *progressRing) Paint(cv *render.Canvas) {
	b := r.Bounds()
	side := float64(min(b.W, b.H))
	radius := side/2 - float64(r.stroke)/2
	if radius > 0 {
		cx, cy := float64(b.X)+float64(b.W)/2, float64(b.Y)+float64(b.H)/2
		cv.Arc(cx, cy, radius, float64(r.stroke), 0, 2*math.Pi, withAlpha(r.color, 0.25))
		if r.fraction > 0 {
			cv.Arc(cx, cy, radius, float64(r.stroke), -math.Pi/2, 2*math.Pi*r.fraction, r.color)
		}
	}
	r.label.Paint(cv)
}

// Children exposes the label to the tree walks.
func (r *progressRing) Children() []widget.Widget { return []widget.Widget{r.label} }

func (r *progressRing) HitTest(widget.Point) widget.Widget { return nil }

// withAlpha scales a color's alpha (premultiplied, like the canvas).
func withAlpha(c render.Color, a float64) render.Color {
	scale := func(v uint8) uint32 { return uint32(math.Round(float64(v) * a)) }
	return render.Color(scale(c.A())<<24 | scale(c.R())<<16 | scale(c.G())<<8 | scale(c.B()))
}
