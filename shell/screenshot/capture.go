package screenshot

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"sync"

	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/service/sharepreview"
	"github.com/stubbedev/wayle/shell/regionoverlay"
)

// The compositor-agnostic capture half of the host
// (screenshot/capture.rs): whole outputs through wlr-screencopy with
// the transform read from wl_output, freeze-frame captures of every
// output for the region overlay and the color picker, and window
// capture through Hyprland's toplevel-export when a Hyprland handle is
// known, else ext-image-copy-capture matched by app id and title.
// Every function blocks on the capture connection.

// frozenOutput is one output captured up front: its connector and the
// full-resolution, transform-corrected frame.
type frozenOutput struct {
	connector string
	image     *image.RGBA
}

// windowTarget identifies the window to capture, resolved from the
// compositor's focus state.
type windowTarget struct {
	// hyprlandHandle is the toplevel-export handle (the window
	// address), when on Hyprland.
	hyprlandHandle uint64
	hasHandle      bool
	// appID and title match an ext toplevel on the generic path.
	appID, title       string
	hasAppID, hasTitle bool
}

// captureAllOutputs captures every named output for the freeze-frame
// flows. Run it before any overlay maps, so transient popups on screen
// are baked into the frames. The copies run sequentially on one
// connection; the conversions (format + transform) fan out one
// goroutine per output.
func captureAllOutputs() ([]frozenOutput, error) {
	c, err := capture.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to wayland: %w", err)
	}
	defer func() { _ = c.Close() }()
	type raw struct {
		connector string
		frame     *capture.Frame
		transform capture.Transform
	}
	var raws []raw
	for _, o := range c.Outputs() {
		if o.Name == "" {
			continue
		}
		f, err := c.CaptureOutput(o, capture.Options{})
		if err != nil {
			return nil, fmt.Errorf("output capture failed: %w", err)
		}
		raws = append(raws, raw{connector: o.Name, frame: f, transform: o.Transform})
	}
	out := make([]frozenOutput, len(raws))
	errs := make([]error, len(raws))
	var wg sync.WaitGroup
	for i, r := range raws {
		wg.Go(func() {
			img, err := sharepreview.FrameImage(r.frame, r.transform)
			if err != nil {
				errs[i] = fmt.Errorf("cannot decode capture: %w", err)
				return
			}
			out[i] = frozenOutput{connector: r.connector, image: img}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// captureOutput captures one output by connector name, or the first
// output when name is empty.
func captureOutput(name string) (*image.RGBA, error) {
	c, err := capture.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to wayland: %w", err)
	}
	defer func() { _ = c.Close() }()
	outs := c.Outputs()
	var target capture.Output
	switch {
	case name != "":
		o, ok := c.OutputByName(name)
		if !ok {
			return nil, fmt.Errorf("output %s not found", name)
		}
		target = o
	case len(outs) > 0:
		target = outs[0]
	default:
		return nil, errors.New("no outputs available")
	}
	f, err := c.CaptureOutput(target, capture.Options{})
	if err != nil {
		return nil, fmt.Errorf("output capture failed: %w", err)
	}
	img, err := sharepreview.FrameImage(f, target.Transform)
	if err != nil {
		return nil, fmt.Errorf("cannot decode capture: %w", err)
	}
	return img, nil
}

// captureWindow captures the target window: Hyprland's toplevel-export
// when a handle is known and the protocol is there, else the generic
// ext path.
func captureWindow(t windowTarget) (*image.RGBA, error) {
	c, err := capture.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to wayland: %w", err)
	}
	defer func() { _ = c.Close() }()
	var f *capture.Frame
	if t.hasHandle && c.HasHyprlandExport() {
		f, err = c.CaptureHyprlandWindow(t.hyprlandHandle, capture.Options{})
		if err != nil {
			return nil, fmt.Errorf("window capture failed: %w", err)
		}
	} else {
		if !c.HasToplevelCapture() {
			return nil, errors.New("window capture not supported on this compositor")
		}
		tls, err := c.Toplevels()
		if err != nil {
			return nil, fmt.Errorf("window capture failed: %w", err)
		}
		idx := -1
		for i, tl := range tls {
			if matchesTarget(tl.AppID, tl.Title, t) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, errors.New("could not find the target window to capture")
		}
		f, err = c.CaptureToplevel(tls[idx], capture.Options{})
		if err != nil {
			return nil, fmt.Errorf("window capture failed: %w", err)
		}
	}
	img, err := sharepreview.FrameImage(f, capture.TransformNormal)
	if err != nil {
		return nil, fmt.Errorf("cannot decode capture: %w", err)
	}
	return img, nil
}

// matchesTarget matches an ext toplevel against the target by app id
// and title. Each key the target carries must match; a target with no
// identity at all matches nothing, rather than everything.
func matchesTarget(appID, title string, t windowTarget) bool {
	if !t.hasAppID && !t.hasTitle {
		return false
	}
	if t.hasAppID && appID != t.appID {
		return false
	}
	if t.hasTitle && title != t.title {
		return false
	}
	return true
}

// logicalGeometry is an output's position and size in the compositor
// layout; positions may be negative.
type logicalGeometry struct {
	x, y, width, height int
}

// placedFrame pairs an output's logical placement with its
// physical-resolution frame.
type placedFrame struct {
	logical logicalGeometry
	image   *image.RGBA
}

// layoutBounds is the bounding box spanning every geometry: the
// minimum origin and the total extent. ok is false without geometries.
func layoutBounds(geoms []logicalGeometry) (x, y, w, h int, ok bool) {
	if len(geoms) == 0 {
		return 0, 0, 0, 0, false
	}
	minX, minY := geoms[0].x, geoms[0].y
	maxX, maxY := geoms[0].x+geoms[0].width, geoms[0].y+geoms[0].height
	for _, g := range geoms[1:] {
		minX, minY = min(minX, g.x), min(minY, g.y)
		maxX, maxY = max(maxX, g.x+g.width), max(maxY, g.y+g.height)
	}
	return minX, minY, max(maxX-minX, 0), max(maxY-minY, 0), true
}

// compositeOutputs composites every output's frame into one image
// spanning the layout's logical bounding box: each frame scaled to its
// logical size (fractional scaling makes the physical size differ) and
// placed at its origin minus the layout's top-left. No frames make a
// 0x0 image.
func compositeOutputs(frames []placedFrame) *image.RGBA {
	geoms := make([]logicalGeometry, len(frames))
	for i, f := range frames {
		geoms[i] = f.logical
	}
	ox, oy, w, h, ok := layoutBounds(geoms)
	if !ok {
		return image.NewRGBA(image.Rectangle{})
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, f := range frames {
		lw, lh := max(f.logical.width, 0), max(f.logical.height, 0)
		if lw == 0 || lh == 0 {
			continue
		}
		scaled := f.image
		if b := f.image.Bounds(); b.Dx() != lw || b.Dy() != lh {
			scaled = sharepreview.Resize(f.image, lw, lh)
		}
		dx, dy := max(f.logical.x-ox, 0), max(f.logical.y-oy, 0)
		draw.Draw(canvas, image.Rect(dx, dy, dx+lw, dy+lh), scaled, scaled.Bounds().Min, draw.Src)
	}
	return canvas
}

// cropFrozen crops a logical selection out of an output's frozen frame:
// the selection scales by the frame/logical ratio and clamps to the
// frame.
func cropFrozen(img *image.RGBA, logicalW, logicalH int, sel regionoverlay.Selection) *image.RGBA {
	b := img.Bounds()
	sx := float64(b.Dx()) / float64(max(logicalW, 1))
	sy := float64(b.Dy()) / float64(max(logicalH, 1))
	x := clampNonNeg(math.Round(float64(sel.X) * sx))
	y := clampNonNeg(math.Round(float64(sel.Y) * sy))
	w := clampNonNeg(math.Round(float64(sel.Width) * sx))
	h := clampNonNeg(math.Round(float64(sel.Height) * sy))
	x, y = min(x, b.Dx()), min(y, b.Dy())
	w, h = min(w, b.Dx()-x), min(h, b.Dy()-y)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), img, image.Pt(b.Min.X+x, b.Min.Y+y), draw.Src)
	return out
}

// clampNonNeg converts a rounded float to int the way Rust's `as u32`
// saturates negatives to zero.
func clampNonNeg(v float64) int {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	return int(v)
}
