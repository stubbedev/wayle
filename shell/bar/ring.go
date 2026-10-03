package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// progressRing is ProgressRing: a DrawingArea-classed canvas whose
// cascade paints it — the stroke is the computed border width, the ink
// the computed color (a success/warning/error class recolors it) —
// over a centered ring-text label.
type progressRing struct {
	widget.Base
	size     int
	fraction float64
	variant  string
	label    *widget.Label
}

// ringVariants is every variant class the ring's cascade recolors by.
var ringVariants = []string{"success", "warning", "error"}

// newProgressRing builds the canvas at the size class inside its
// progress-ring overlay.
func newProgressRing(size int, sizeClass string, font render.Font, px float64) (*progressRing, *widget.Box) {
	r := &progressRing{size: size}
	r.AddClass("progress-ring-canvas", sizeClass)
	r.label = widget.NewLabel(font, px, "", 0)
	r.label.AddClass("ring-text")
	overlay := widget.NewBox(widget.Column, 0, 0)
	overlay.AddClass("progress-ring", sizeClass)
	overlay.Append(r, false)
	overlay.Append(r.label, false)
	return r, overlay
}

// set updates the gauge: the fraction (clamped to 0..1), its label, and
// the variant class the cascade recolors by.
func (r *progressRing) set(fraction float64, label string, variant string) {
	r.fraction = math.Max(0, math.Min(1, fraction))
	if variant != r.variant {
		r.RemoveClass(ringVariants...)
		if variant != "" {
			r.AddClass(variant)
		}
		r.variant = variant
	}
	r.label.SetText(label)
	r.Invalidate()
}

// stroke is the ring's line width: the cascade's border width, what
// the Rust draw reads off the style context.
func (r *progressRing) stroke() int { return widget.CascadeBorder(r).Top }

func (r *progressRing) Measure(widget.Constraints) widget.Size {
	return widget.Size{W: r.size, H: r.size}
}

func (r *progressRing) Arrange(rect render.Rect) {
	r.ArrangeSelf(rect)
	widget.SetParents(r, r.label)
	sz := r.label.Measure(widget.Constraints{Max: widget.Size{W: rect.W, H: rect.H}})
	r.label.Arrange(render.Rect{X: rect.X + (rect.W-sz.W)/2, Y: rect.Y + (rect.H-sz.H)/2, W: sz.W, H: sz.H})
}

// Paint is draw_ring: a quarter-alpha track and the fraction clockwise
// from twelve o'clock, round-capped, both in the computed color.
func (r *progressRing) Paint(cv *render.Canvas) {
	b := r.Bounds()
	side := float64(min(b.W, b.H))
	stroke := float64(r.stroke())
	radius := side/2 - stroke/2
	if radius > 0 {
		ink := widget.CascadeColor(r)
		cx, cy := float64(b.X)+float64(b.W)/2, float64(b.Y)+float64(b.H)/2
		cv.Arc(cx, cy, radius, stroke, 0, 2*math.Pi, withAlpha(ink, 0.25))
		if r.fraction > 0 {
			cv.Arc(cx, cy, radius, stroke, -math.Pi/2, 2*math.Pi*r.fraction, ink)
		}
	}
	widget.PaintChild(cv, r.label)
}

// Children exposes the label to the tree walks.
func (r *progressRing) Children() []widget.Widget { return []widget.Widget{r.label} }

func (r *progressRing) HitTest(widget.Point) widget.Widget { return nil }

// withAlpha scales a color's alpha (premultiplied, like the canvas).
func withAlpha(c render.Color, a float64) render.Color {
	scale := func(v uint8) uint32 { return uint32(math.Round(float64(v) * a)) }
	return render.Color(scale(c.A())<<24 | scale(c.R())<<16 | scale(c.G())<<8 | scale(c.B()))
}
