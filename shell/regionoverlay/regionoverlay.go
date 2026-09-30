// Package regionoverlay is the slurp-like region selection overlay
// (crates/wayle-shell/src/shell/region_overlay): a layer surface on
// every monitor, a dim wash with a transparent hole where the dragged
// rectangle overlaps that monitor, an accent border, and a "W × H"
// size label. The drag is tracked in global (compositor-layout)
// logical coordinates, so it can start on one monitor and end on
// another. The screenshot host passes frozen per-output frames, which
// the surfaces paint behind the wash (freeze-frame); the share picker
// passes none, so the live screen shows through.
package regionoverlay

import (
	"fmt"
	"image"
	"log"
	"math"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// Selection is a region the user dragged, in logical pixels relative
// to Output: slurp's `<output>@x,y,w,h` and the screencopy region
// semantics.
type Selection struct {
	// Output is the connector name of the output the selection is
	// relative to.
	Output              string
	X, Y, Width, Height int
}

// Monitor is one output's connector name and global logical geometry.
type Monitor struct {
	Connector           string
	X, Y, Width, Height int
}

// MonitorOf reads an output's logical geometry: the xdg-output
// position and size, falling back to the mode divided by the scale
// when the compositor reported no logical size.
func MonitorOf(o *app.Output) Monitor {
	m := Monitor{Connector: o.Name, X: int(o.LogicalX), Y: int(o.LogicalY), Width: int(o.LogicalW), Height: int(o.LogicalH)}
	if m.Width <= 0 || m.Height <= 0 {
		scale := max(o.Scale, 1)
		m.Width, m.Height = o.ModeW/scale, o.ModeH/scale
	}
	return m
}

// dragRect is a drag in global logical coordinates.
type dragRect struct {
	startX, startY float64
	endX, endY     float64
}

// normalized is (x, y, w, h) with a positive extent whatever the drag
// direction.
func (r dragRect) normalized() (x, y, w, h float64) {
	return math.Min(r.startX, r.endX), math.Min(r.startY, r.endY),
		math.Abs(r.endX - r.startX), math.Abs(r.endY - r.startY)
}

// finalize resolves a global drag to an output-relative selection: the
// output is the one containing the rectangle's top-left corner, else
// the first monitor. A drag under one logical pixel on either axis is
// no selection (a plain click cancels).
func finalize(r dragRect, mons []Monitor) (Selection, bool) {
	gx, gy, gw, gh := r.normalized()
	if gw < 1 || gh < 1 || len(mons) == 0 {
		return Selection{}, false
	}
	x, y := int(math.Round(gx)), int(math.Round(gy))
	w, h := int(math.Round(gw)), int(math.Round(gh))
	mon := mons[0]
	for _, m := range mons {
		if x >= m.X && x < m.X+m.Width && y >= m.Y && y < m.Y+m.Height {
			mon = m
			break
		}
	}
	return Selection{Output: mon.Connector, X: x - mon.X, Y: y - mon.Y, Width: w, Height: h}, true
}

// Style carries the overlay's themed pieces: the accent (the
// .region-overlay-area color, --palette-primary) and the monospace bold
// face of the size label.
type Style struct {
	Accent render.Color
	Font   render.Font
}

// Paint constants of region_overlay/mod.rs's draw func.
const (
	// dimAlpha is the wash: rgba(0, 0, 0, 0.35).
	dimAlpha    = 89
	borderWidth = 2
	labelPx     = 14.0
	labelPad    = 6
	labelGap    = 4
)

// labelBacking is the size label's dark box, rgba(0, 0, 0, 0.75).
var labelBacking = render.Color(191 << 24)

// escapeKeycode is the evdev code of Escape.
const escapeKeycode = 1

// result is one request's answer.
type result struct {
	sel Selection
	ok  bool
}

// Overlay is the overlay component. Request is its only entry point;
// everything else runs on the gelm loop goroutine.
type Overlay struct {
	app     *app.Application
	outputs func() []*app.Output
	style   Style

	reply    chan result
	surfaces []*app.LayerWindow
	areas    []*area
	monitors []Monitor
	// drag is the shared rectangle every surface paints; nil before
	// the first press.
	drag *dragRect
	// pointer is the last global pointer position any surface saw,
	// where a press starts the drag.
	pointerX, pointerY float64
}

// New builds the overlay on the application; outputs lists the
// monitors to cover at each request.
func New(a *app.Application, outputs func() []*app.Output, style Style) *Overlay {
	return &Overlay{app: a, outputs: outputs, style: style}
}

// Request opens the overlay and blocks until the user drags a region
// (ok) or cancels with Escape or a click (not ok). frames maps
// connector names to frozen frames painted behind the wash; nil or a
// missing connector leaves that surface transparent over the live
// screen. A request arriving while one is open cancels the older one.
// Call it from any goroutine but the loop's.
func (o *Overlay) Request(frames map[string]*image.RGBA) (Selection, bool) {
	ch := make(chan result, 1)
	o.app.Invoke(func() { o.show(ch, frames) })
	r := <-ch
	return r.sel, r.ok
}

// show replaces any open request with a fresh one and maps the
// surfaces.
func (o *Overlay) show(ch chan result, frames map[string]*image.RGBA) {
	o.finish(Selection{}, false)
	o.reply = ch
	o.open(frames)
	if len(o.surfaces) == 0 {
		log.Print("region overlay: no surface could be mapped")
		o.finish(Selection{}, false)
	}
}

// open maps one surface per monitor.
func (o *Overlay) open(frames map[string]*image.RGBA) {
	outs := o.outputs()
	o.monitors = o.monitors[:0]
	for _, out := range outs {
		o.monitors = append(o.monitors, MonitorOf(out))
	}
	for i, out := range outs {
		mon := o.monitors[i]
		a := &area{o: o, mon: mon, frame: frames[mon.Connector]}
		a.AddClass("region-overlay-area")
		layer, err := o.app.NewLayer(app.LayerConfig{
			Output:        out,
			Layer:         app.LayerOverlay,
			Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
			ExclusiveZone: -1,
			Keyboard:      app.KeyboardExclusive,
			Namespace:     "wayle-region-overlay",
			Root:          a,
			OnKey: func(_ *widget.Router, keycode uint32, _ app.Mods) {
				if keycode == escapeKeycode {
					o.finish(Selection{}, false)
				}
			},
		})
		if err != nil {
			log.Printf("region overlay: surface on %s: %v", mon.Connector, err)
			continue
		}
		o.surfaces = append(o.surfaces, layer)
		o.areas = append(o.areas, a)
	}
}

// finish answers the open request (if any) and tears the surfaces
// down.
func (o *Overlay) finish(sel Selection, ok bool) {
	if o.reply != nil {
		o.reply <- result{sel: sel, ok: ok}
		o.reply = nil
	}
	for _, s := range o.surfaces {
		s.Close()
	}
	o.surfaces, o.areas, o.drag = nil, nil, nil
}

// press starts the drag at the last pointer position.
func (o *Overlay) press() {
	o.drag = &dragRect{startX: o.pointerX, startY: o.pointerY, endX: o.pointerX, endY: o.pointerY}
	o.redraw()
}

// move extends the drag to a global position.
func (o *Overlay) move(x, y float64) {
	if o.drag == nil {
		return
	}
	o.drag.endX, o.drag.endY = x, y
	o.redraw()
}

// release concludes the gesture: a real rectangle answers, anything
// else cancels.
func (o *Overlay) release() {
	if o.drag == nil {
		o.finish(Selection{}, false)
		return
	}
	sel, ok := finalize(*o.drag, o.monitors)
	o.finish(sel, ok)
}

func (o *Overlay) redraw() {
	for _, a := range o.areas {
		a.Invalidate()
	}
}

// area is one monitor's drawing area: the frozen frame (if any), the
// wash with its hole, the border, and the label.
type area struct {
	widget.Base
	o     *Overlay
	mon   Monitor
	frame *image.RGBA

	// bright and dim are the frame resampled to the device rect and
	// its washed copy, built on first paint at that size.
	bright, dim *image.RGBA
}

func (a *area) Measure(con widget.Constraints) widget.Size { return con.Max }

func (a *area) HitTest(p widget.Point) widget.Widget { return a.HitLeaf(a, p) }

// CursorName implements widget.CursorNamer.
func (a *area) CursorName() string { return "crosshair" }

// global maps a surface-local point to the layout.
func (a *area) global(p widget.Point) (float64, float64) {
	return float64(a.mon.X + p.X), float64(a.mon.Y + p.Y)
}

// HoverMove implements widget.HoverMover: track where a press starts.
func (a *area) HoverMove(p widget.Point) {
	a.o.pointerX, a.o.pointerY = a.global(p)
}

// SetPressed implements widget.PressSetter: a press starts the drag.
func (a *area) SetPressed(on bool) {
	if on {
		a.o.press()
	}
}

// DragMove implements widget.DragMover. Under the implicit grab the
// point can lie beyond this surface; mapping it through the monitor
// offset carries the drag across outputs.
func (a *area) DragMove(p widget.Point) {
	a.o.move(a.global(p))
}

// PressEnd implements widget.PressEnder: the release (or a lost
// pointer) ends the gesture.
func (a *area) PressEnd() { a.o.release() }

// localSelection is the drag in this surface's logical coordinates.
func (a *area) localSelection() (render.Rect, bool) {
	if a.o.drag == nil {
		return render.Rect{}, false
	}
	gx, gy, gw, gh := a.o.drag.normalized()
	return render.Rect{
		X: int(math.Round(gx)) - a.mon.X,
		Y: int(math.Round(gy)) - a.mon.Y,
		W: int(math.Round(gw)),
		H: int(math.Round(gh)),
	}, true
}

func (a *area) Paint(cv *render.Canvas) {
	b := a.Bounds()
	dev := cv.MapRect(b)
	sel, dragging := a.localSelection()
	if dragging {
		sel.X += b.X
		sel.Y += b.Y
	}
	selDev := cv.MapRect(sel).Intersect(dev)
	if a.frame != nil {
		a.paintFrozen(cv, dev, selDev, dragging)
	} else {
		cv.ClearDevice(dev, render.Color(dimAlpha<<24))
		if dragging && !selDev.Empty() {
			cv.ClearDevice(selDev, 0)
		}
	}
	if dragging {
		a.paintBorderAndLabel(cv, sel)
	}
}

// paintFrozen draws the washed frame and, inside the selection, the
// bright one: the wash-then-hole composite of the Rust overlay, whose
// transparent drawing area sits over the frame picture.
func (a *area) paintFrozen(cv *render.Canvas, dev, selDev render.Rect, dragging bool) {
	if a.bright == nil || a.bright.Bounds().Dx() != dev.W || a.bright.Bounds().Dy() != dev.H {
		a.bright = render.Resample(a.frame, a.frame.Bounds(), dev.W, dev.H)
		a.dim = wash(a.bright)
	}
	cv.DrawImageDevice(a.dim, dev.X, dev.Y)
	if dragging && !selDev.Empty() {
		sub := image.Rect(selDev.X-dev.X, selDev.Y-dev.Y, selDev.X-dev.X+selDev.W, selDev.Y-dev.Y+selDev.H)
		cv.DrawImageDevice(a.bright.SubImage(sub), selDev.X, selDev.Y)
	}
}

// wash composites the 35% black dim over a copy of img.
func wash(img *image.RGBA) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	const keep = 255 - dimAlpha
	for i, v := range img.Pix {
		out.Pix[i] = uint8((uint32(v)*keep + 127) / 255)
		if i%4 == 3 {
			// Alpha: the black wash over opaque stays opaque; over a
			// translucent pixel it adds its own coverage.
			out.Pix[i] = uint8(dimAlpha + (uint32(v)*keep+127)/255)
		}
	}
	return out
}

// paintBorderAndLabel strokes the accent border (2px, centered on the
// selection edge like cairo's stroke) and draws the "W × H" label on
// its dark backing box, above the selection when it fits, else just
// inside it.
func (a *area) paintBorderAndLabel(cv *render.Canvas, sel render.Rect) {
	accent := a.o.style.Accent
	cv.BorderRect(render.Rect{X: sel.X - borderWidth/2, Y: sel.Y - borderWidth/2, W: sel.W + borderWidth, H: sel.H + borderWidth}, borderWidth, accent)
	if a.o.style.Font == nil {
		return
	}
	text := labelText(sel.W, sel.H)
	shaped := a.o.style.Font.Shape(text, labelPx)
	boxW := int(math.Ceil(shaped.Advance())) + labelPad*2
	ascent := int(math.Ceil(shaped.Ascent()))
	boxH := ascent + labelPad*2
	bx, by := labelOrigin(sel, boxH)
	cv.FillRect(render.Rect{X: bx, Y: by, W: boxW, H: boxH}, labelBacking)
	a.o.style.Font.Draw(cv, shaped, bx+labelPad, by+labelPad+ascent, accent)
}

// labelText is the size label in logical pixels.
func labelText(w, h int) string { return fmt.Sprintf("%d × %d", w, h) }

// labelOrigin places the label box at the selection's left edge,
// above it when there is room on the surface, else just inside.
func labelOrigin(sel render.Rect, boxH int) (int, int) {
	if above := sel.Y - boxH - labelGap; above >= 0 {
		return sel.X, above
	}
	return sel.X, sel.Y + labelGap
}
