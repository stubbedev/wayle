package wallpaper

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// TransitionKind is how one wallpaper replaces the previous one: the
// gtk::Stack transitions the Rust surface flips between its two
// pictures (stack_transition).
type TransitionKind uint8

// Transitions. The [animations] types a Stack cannot do (swing,
// bounce, genie, zoom, rotate, flip) fall back to Crossfade, as in
// Rust.
const (
	TransitionNone TransitionKind = iota
	TransitionCrossfade
	TransitionSlideUp
	TransitionSlideDown
	TransitionSlideLeft
	TransitionSlideRight
)

// Transition is the kind and its duration.
type Transition struct {
	Kind     TransitionKind
	Duration time.Duration
}

// DefaultTransition is [animations]' default for the wallpaper surface:
// a 200ms fade.
func DefaultTransition() Transition {
	return Transition{Kind: TransitionCrossfade, Duration: 200 * time.Millisecond}
}

// TransitionFor maps an [animations] AnimationType name to the stack
// transition the wallpaper can play.
func TransitionFor(animationType string, d time.Duration) (Transition, error) {
	kind := TransitionCrossfade
	switch animationType {
	case "none":
		kind = TransitionNone
	case "slide-up":
		kind = TransitionSlideUp
	case "slide-down":
		kind = TransitionSlideDown
	case "slide-left":
		kind = TransitionSlideLeft
	case "slide-right":
		kind = TransitionSlideRight
	case "fade", "swing-up", "swing-down", "swing-left", "swing-right", "bounce", "genie", "zoom", "rotate", "flip":
	default:
		return Transition{}, fmt.Errorf("unknown animation type %q", animationType)
	}
	return Transition{Kind: kind, Duration: d}, nil
}

// easeOutCubic is GtkStack's progress curve.
func easeOutCubic(t float64) float64 {
	p := t - 1
	return p*p*p + 1
}

// view shows the current wallpaper and, mid-transition, the previous
// one: the Go counterpart of the surface's two-picture gtk::Stack.
type view struct {
	widget.Base
	cur, prev *widget.Image
	// curSize is cur's source size, for the crossfade's letterbox.
	curSize image.Point
	kind    TransitionKind
	// progress runs 0 -> 1 over a transition; at 1 prev is dropped.
	progress float64
}

func newView() *view { return &view{progress: 1} }

// show swaps img in with the given transition kind; the caller then
// drives progress with step. TransitionNone (or no previous image)
// lands at once.
func (v *view) show(img *widget.Image, size image.Point, kind TransitionKind) {
	v.prev, v.cur, v.curSize = v.cur, img, size
	v.kind, v.progress = kind, 0
	if kind == TransitionNone || v.prev == nil {
		v.finish()
	}
	v.arrangeChildren()
	v.InvalidateLayout()
	v.Invalidate()
}

// step advances a running transition to linear time fraction t (0-1)
// and reports whether it is still running.
func (v *view) step(t float64) bool {
	if t >= 1 {
		v.finish()
	} else {
		v.progress = easeOutCubic(max(t, 0))
	}
	v.arrangeChildren()
	v.Invalidate()
	return v.progress < 1
}

func (v *view) finish() {
	v.progress, v.prev = 1, nil
}

// Measure claims the whole box: the surface is anchored to every edge.
func (v *view) Measure(con widget.Constraints) widget.Size { return con.Max }

// Arrange records the rect and places the images for the current
// transition offset.
func (v *view) Arrange(r render.Rect) {
	v.ArrangeSelf(r)
	v.arrangeChildren()
}

// ArrangeRoot mirrors Arrange for the tree-root path.
func (v *view) ArrangeRoot(r render.Rect) { v.Arrange(r) }

// offsets are the logical x/y shifts of the incoming and outgoing
// images: the incoming slides from the far edge to rest, the outgoing
// from rest out the near edge.
func (v *view) offsets(r render.Rect) (inX, inY, outX, outY int) {
	p := v.progress
	w, h := float64(r.W), float64(r.H)
	round := func(f float64) int { return int(math.Round(f)) }
	switch v.kind {
	case TransitionSlideLeft:
		return round(w * (1 - p)), 0, round(-w * p), 0
	case TransitionSlideRight:
		return round(-w * (1 - p)), 0, round(w * p), 0
	case TransitionSlideUp:
		return 0, round(h * (1 - p)), 0, round(-h * p)
	case TransitionSlideDown:
		return 0, round(-h * (1 - p)), 0, round(h * p)
	}
	return 0, 0, 0, 0
}

func (v *view) arrangeChildren() {
	r := v.Bounds()
	inX, inY, outX, outY := v.offsets(r)
	if v.cur != nil {
		v.cur.Arrange(render.Rect{X: r.X + inX, Y: r.Y + inY, W: r.W, H: r.H})
	}
	if v.prev != nil {
		v.prev.Arrange(render.Rect{X: r.X + outX, Y: r.Y + outY, W: r.W, H: r.H})
	}
	widget.SetParents(v, v.Children()...)
}

// Paint draws the outgoing image under the incoming one, clipped to
// the surface. A crossfade keeps the outgoing image opaque and blends
// the incoming one over it at the progress: over opaque pixels that is
// exactly prev*(1-p) + cur*p, the GSK cross-fade.
func (v *view) Paint(cv *render.Canvas) {
	prevClip := cv.PushClip(cv.MapRect(v.Bounds()))
	defer cv.PopClip(prevClip)
	if v.prev != nil {
		widget.PaintChild(cv, v.prev)
	}
	if v.cur == nil {
		return
	}
	if v.prev == nil || v.kind != TransitionCrossfade {
		widget.PaintChild(cv, v.cur)
		return
	}
	// The incoming letterbox is the black backdrop fading in too.
	prevAlpha := cv.PushAlpha(v.progress)
	box := cv.MapRect(v.cur.Bounds())
	_, dw, dh := render.ScaleRect(v.curSize.X, v.curSize.Y, box.W, box.H, v.cur.Scale())
	inner := render.Rect{X: box.X + (box.W-dw)/2, Y: box.Y + (box.H-dh)/2, W: dw, H: dh}
	fillOutside(cv, box, inner, backdrop)
	widget.PaintChild(cv, v.cur)
	cv.PopAlpha(prevAlpha)
}

// backdrop is the surface's letterbox color (.wallpaper-window's
// background: black).
var backdrop = render.RGB(0, 0, 0)

// fillOutside fills the device-pixel ring of outer around inner.
func fillOutside(cv *render.Canvas, outer, inner render.Rect, c render.Color) {
	if inner.W <= 0 || inner.H <= 0 {
		cv.FillRectDevice(outer, c)
		return
	}
	for _, r := range []render.Rect{
		{X: outer.X, Y: outer.Y, W: outer.W, H: inner.Y - outer.Y},
		{X: outer.X, Y: inner.Y + inner.H, W: outer.W, H: outer.Y + outer.H - inner.Y - inner.H},
		{X: outer.X, Y: inner.Y, W: inner.X - outer.X, H: inner.H},
		{X: inner.X + inner.W, Y: inner.Y, W: outer.X + outer.W - inner.X - inner.W, H: inner.H},
	} {
		if r.W > 0 && r.H > 0 {
			cv.FillRectDevice(r, c)
		}
	}
}

// HitTest: the wallpaper takes no input; presses land on the view.
func (v *view) HitTest(p widget.Point) widget.Widget { return v.HitLeaf(v, p) }

// Children lists the images being shown.
func (v *view) Children() []widget.Widget {
	var out []widget.Widget
	if v.prev != nil {
		out = append(out, v.prev)
	}
	if v.cur != nil {
		out = append(out, v.cur)
	}
	return out
}
