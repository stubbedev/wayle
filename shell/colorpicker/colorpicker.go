// Package colorpicker is the magnifier-loupe color picker
// (crates/wayle-shell/src/shell/color_picker): a frozen-frame layer
// surface per monitor with a zoom loupe that follows the pointer - the
// magnified pixels around it, a crosshair on the exact pixel a click
// picks, a hex readout, and a row of recently picked swatches. A click
// samples that pixel; Escape cancels. The screenshot host captures the
// frames and hands them over (it owns the capture path), and the
// portal's Screenshot.PickColor reaches it over com.wayle.Screenshot1.
package colorpicker

import (
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/shell/regionoverlay"
)

// Loupe geometry of color_picker/mod.rs.
const (
	// zoom is the size of one magnified source pixel.
	zoom = 11
	// radius is the loupe radius in source pixels: the grid is
	// 2*radius+1 square.
	radius = 8
	// historyMax is the most swatches kept.
	historyMax = 8

	offset     = 24.0
	panelPad   = 10
	readoutH   = 28
	swatchSize = 16
	swatchGap  = 4
	hexPx      = 13.0
)

// Colors of the loupe card.
var (
	cardBacking   = render.RGBA(20, 20, 26, 235)
	crossWhite    = render.RGBA(255, 255, 255, 242)
	crossBlack    = render.RGBA(0, 0, 0, 230)
	hexColor      = render.RGBA(255, 255, 255, 242)
	swatchOutline = render.RGBA(255, 255, 255, 77)
)

// escapeKeycode is the evdev code of Escape.
const escapeKeycode = 1

// RGB is one 8-bit color.
type RGB struct{ R, G, B uint8 }

// Hex is the color as #RRGGBB.
func (c RGB) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// Float is the color as sRGB channels in [0, 1], the PickColor reply.
func (c RGB) Float() (r, g, b float64) {
	return float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255
}

// result is one request's answer.
type result struct {
	color RGB
	ok    bool
}

// Picker is the picker component. Request is its entry point; the rest
// runs on the gelm loop goroutine.
type Picker struct {
	app     *app.Application
	outputs func() []*app.Output
	font    render.Font
	// historyPath is where the swatches persist; empty disables it.
	historyPath string

	history  []RGB
	reply    chan result
	surfaces []*app.LayerWindow
}

// New builds the picker. font is the readout's monospace bold face;
// the history loads from historyPath (HistoryPath for the real one).
func New(a *app.Application, outputs func() []*app.Output, font render.Font, historyPath string) *Picker {
	return &Picker{app: a, outputs: outputs, font: font, historyPath: historyPath, history: loadHistory(historyPath)}
}

// HistoryPath is the persisted swatch file:
// $XDG_DATA_HOME/wayle/color-history, else ~/.local/share/wayle/color-history.
// Empty when neither variable is set.
func HistoryPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "wayle", "color-history")
}

// Request opens the picker over the frozen frames (keyed by connector)
// and blocks until a click picks a pixel (ok) or the user cancels. A
// monitor without a frame gets no surface; no surface at all cancels
// at once. Call it from any goroutine but the loop's.
func (p *Picker) Request(frames map[string]*image.RGBA) (RGB, bool) {
	ch := make(chan result, 1)
	p.app.Invoke(func() { p.show(ch, frames) })
	r := <-ch
	return r.color, r.ok
}

func (p *Picker) show(ch chan result, frames map[string]*image.RGBA) {
	p.finish(RGB{}, false)
	p.reply = ch
	for _, out := range p.outputs() {
		mon := regionoverlay.MonitorOf(out)
		frame, ok := frames[mon.Connector]
		if !ok || frame == nil {
			continue
		}
		l := &loupe{p: p, frame: frame, logicalW: mon.Width, logicalH: mon.Height}
		layer, err := p.app.NewLayer(app.LayerConfig{
			Output:        out,
			Layer:         app.LayerOverlay,
			Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
			ExclusiveZone: -1,
			Keyboard:      app.KeyboardExclusive,
			Namespace:     "wayle-color-picker",
			Root:          l,
			OnKey: func(_ *widget.Router, keycode uint32, _ app.Mods) {
				if keycode == escapeKeycode {
					p.finish(RGB{}, false)
				}
			},
		})
		if err != nil {
			log.Printf("color picker: surface on %s: %v", mon.Connector, err)
			continue
		}
		p.surfaces = append(p.surfaces, layer)
	}
	// No frame matched any monitor: nothing to pick from.
	if len(p.surfaces) == 0 {
		p.finish(RGB{}, false)
	}
}

// finish answers the open request, remembering a pick in the history,
// and tears the surfaces down.
func (p *Picker) finish(c RGB, ok bool) {
	if ok {
		p.history = pushHistory(p.history, c)
		saveHistory(p.historyPath, p.history)
	}
	if p.reply != nil {
		p.reply <- result{color: c, ok: ok}
		p.reply = nil
	}
	for _, s := range p.surfaces {
		s.Close()
	}
	p.surfaces = nil
}

// pushHistory moves c to the front, dropping its older copy and
// anything past historyMax.
func pushHistory(h []RGB, c RGB) []RGB {
	out := []RGB{c}
	for _, old := range h {
		if old != c && len(out) < historyMax {
			out = append(out, old)
		}
	}
	return out
}

// loadHistory reads the swatches, newest first; an unreadable file is
// an empty history.
func loadHistory(path string) []RGB {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is HistoryPath's fixed data-dir file
	if err != nil {
		return nil
	}
	var out []RGB
	for line := range strings.SplitSeq(string(data), "\n") {
		if c, ok := parseHex(line); ok && len(out) < historyMax {
			out = append(out, c)
		}
	}
	return out
}

// saveHistory persists the swatches, one #RRGGBB per line, best
// effort.
func saveHistory(path string, h []RGB) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755) //nolint:gosec // a plain data directory, like the Rust create_dir_all
	var b strings.Builder
	for _, c := range h {
		b.WriteString(c.Hex())
		b.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(b.String()), 0o644) //nolint:gosec // the history is not secret
}

// parseHex reads one "#RRGGBB" line.
func parseHex(line string) (RGB, bool) {
	hex, ok := strings.CutPrefix(strings.TrimSpace(line), "#")
	if !ok || len(hex) != 6 {
		return RGB{}, false
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return RGB{}, false
	}
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

// sampleAt is the source pixel under a surface-local logical point:
// the point scaled to the frame's physical resolution, truncated and
// clamped.
func sampleAt(img *image.RGBA, logicalW, logicalH int, lx, ly float64) (int, int) {
	b := img.Bounds()
	sx := float64(b.Dx()) / float64(max(logicalW, 1))
	sy := float64(b.Dy()) / float64(max(logicalH, 1))
	return clampIndex(int(lx*sx), b.Dx()-1), clampIndex(int(ly*sy), b.Dy()-1)
}

// pixel reads img at frame coordinates as an opaque 8-bit color.
func pixel(img *image.RGBA, x, y int) RGB {
	c := img.RGBAAt(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
	return RGB{c.R, c.G, c.B}
}

// clampIndex clamps v into [0, hi].
func clampIndex(v, hi int) int { return min(max(v, 0), hi) }

// loupe is one monitor's picker surface.
type loupe struct {
	widget.Base
	p                  *Picker
	frame              *image.RGBA
	logicalW, logicalH int

	cursorX, cursorY float64
	hasCursor        bool

	// bright is the frame resampled to the device rect, built on first
	// paint.
	bright *image.RGBA
}

func (l *loupe) Measure(con widget.Constraints) widget.Size { return con.Max }

func (l *loupe) HitTest(p widget.Point) widget.Widget { return l.HitLeaf(l, p) }

// CursorName implements widget.CursorNamer: the loupe replaces the
// pointer.
func (l *loupe) CursorName() string { return "none" }

// HoverMove implements widget.HoverMover.
func (l *loupe) HoverMove(p widget.Point) {
	b := l.Bounds()
	l.cursorX, l.cursorY, l.hasCursor = float64(p.X-b.X), float64(p.Y-b.Y), true
	l.Invalidate()
}

// SetHovered implements widget.HoverSetter: leaving hides the loupe.
func (l *loupe) SetHovered(on bool) {
	if !on {
		l.hasCursor = false
		l.Invalidate()
	}
}

// ClickAt implements widget.Clicker: the release picks the pixel under
// it.
func (l *loupe) ClickAt(p widget.Point) {
	b := l.Bounds()
	x, y := sampleAt(l.frame, l.logicalW, l.logicalH, float64(p.X-b.X), float64(p.Y-b.Y))
	l.p.finish(pixel(l.frame, x, y), true)
}

func (l *loupe) Paint(cv *render.Canvas) {
	b := l.Bounds()
	dev := cv.MapRect(b)
	if l.bright == nil || l.bright.Bounds().Dx() != dev.W || l.bright.Bounds().Dy() != dev.H {
		l.bright = render.Resample(l.frame, l.frame.Bounds(), dev.W, dev.H)
	}
	cv.DrawImageDevice(l.bright, dev.X, dev.Y)
	if l.hasCursor {
		drawLoupe(cv, l.p.font, l.frame, l.logicalW, l.logicalH, b, l.cursorX, l.cursorY, l.p.history)
	}
}

// drawLoupe paints the loupe card near the cursor (flipping to stay on
// screen): the magnified grid, the crosshair on the center pixel, the
// color chip with its hex, and the history swatches. box is the
// surface's logical rect, (cx, cy) the cursor inside it.
func drawLoupe(cv *render.Canvas, font render.Font, img *image.RGBA, logicalW, logicalH int, box render.Rect, cx, cy float64, history []RGB) {
	centerX, centerY := sampleAt(img, logicalW, logicalH, cx, cy)
	grid := 2*radius + 1
	loupeSize := grid * zoom
	historyH := 0
	if len(history) > 0 {
		historyH = swatchSize + panelPad
	}
	panelW, panelH := loupeSize, loupeSize+readoutH+historyH

	px, py := cx+offset, cy+offset
	if px+float64(panelW) > float64(box.W) {
		px = cx - offset - float64(panelW)
	}
	if py+float64(panelH) > float64(box.H) {
		py = cy - offset - float64(panelH)
	}
	x0 := box.X + int(math.Max(px, 0))
	y0 := box.Y + int(math.Max(py, 0))

	cv.FillRect(render.Rect{X: x0 - 4, Y: y0 - 4, W: panelW + 8, H: panelH + 8}, cardBacking)
	b := img.Bounds()
	for gy := -radius; gy <= radius; gy++ {
		for gx := -radius; gx <= radius; gx++ {
			c := pixel(img, clampIndex(centerX+gx, b.Dx()-1), clampIndex(centerY+gy, b.Dy()-1))
			cv.FillRect(render.Rect{X: x0 + (gx+radius)*zoom, Y: y0 + (gy+radius)*zoom, W: zoom, H: zoom}, render.RGB(c.R, c.G, c.B))
		}
	}
	// The crosshair: a white box on the picked pixel, ringed in black
	// so it reads on any color.
	cx0, cy0 := x0+radius*zoom, y0+radius*zoom
	cv.BorderRect(render.Rect{X: cx0 - 1, Y: cy0 - 1, W: zoom + 2, H: zoom + 2}, 2, crossWhite)
	cv.BorderRect(render.Rect{X: cx0 - 2, Y: cy0 - 2, W: zoom + 4, H: zoom + 4}, 1, crossBlack)

	picked := pixel(img, centerX, centerY)
	ry := y0 + loupeSize
	cv.FillRect(render.Rect{X: x0, Y: ry + 4, W: 20, H: 20}, render.RGB(picked.R, picked.G, picked.B))
	if font != nil {
		shaped := font.Shape(picked.Hex(), hexPx)
		font.Draw(cv, shaped, x0+28, ry+19, hexColor)
	}
	drawHistory(cv, history, x0, ry+readoutH, panelW)
}

// drawHistory paints the swatch row at (x0, y), clipped to the panel
// width.
func drawHistory(cv *render.Canvas, history []RGB, x0, y, panelW int) {
	for i, c := range history {
		hx := x0 + i*(swatchSize+swatchGap)
		if hx+swatchSize > x0+panelW {
			break
		}
		r := render.Rect{X: hx, Y: y, W: swatchSize, H: swatchSize}
		cv.FillRect(r, render.RGB(c.R, c.G, c.B))
		cv.BorderRect(r, 1, swatchOutline)
	}
}
