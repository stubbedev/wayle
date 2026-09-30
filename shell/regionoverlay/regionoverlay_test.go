package regionoverlay

import (
	"image"
	"image/color"
	"testing"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

var mons = []Monitor{
	{Connector: "DP-1", X: 0, Y: 0, Width: 1920, Height: 1080},
	{Connector: "DP-2", X: 1920, Y: 0, Width: 1280, Height: 1024},
}

func TestFinalize(t *testing.T) {
	t.Run("output-relative on the monitor holding the top-left", func(t *testing.T) {
		// Dragged right to left across the boundary: normalized first.
		sel, ok := finalize(dragRect{startX: 2000.4, startY: 300, endX: 1900, endY: 100}, mons)
		want := Selection{Output: "DP-1", X: 1900, Y: 100, Width: 100, Height: 200}
		if !ok || sel != want {
			t.Fatalf("finalize = %+v %v, want %+v", sel, ok, want)
		}
		sel, ok = finalize(dragRect{startX: 1930, startY: 10, endX: 2030, endY: 60}, mons)
		if !ok || sel.Output != "DP-2" || sel.X != 10 || sel.Y != 10 {
			t.Fatalf("second monitor = %+v", sel)
		}
	})
	t.Run("a top-left outside every monitor falls back to the first", func(t *testing.T) {
		sel, ok := finalize(dragRect{startX: 2000, startY: 1050, endX: 2100, endY: 1100}, mons)
		if !ok || sel.Output != "DP-1" || sel.X != 2000 || sel.Y != 1050 {
			t.Fatalf("fallback = %+v", sel)
		}
	})
	t.Run("under a pixel is no selection", func(t *testing.T) {
		if _, ok := finalize(dragRect{startX: 5, startY: 5, endX: 5.5, endY: 90}, mons); ok {
			t.Fatal("a zero-width drag selected")
		}
		if _, ok := finalize(dragRect{startX: 5, startY: 5, endX: 5, endY: 5}, mons); ok {
			t.Fatal("a click selected")
		}
	})
	t.Run("no monitors is no selection", func(t *testing.T) {
		if _, ok := finalize(dragRect{endX: 50, endY: 50}, nil); ok {
			t.Fatal("selected with no monitors")
		}
	})
}

func TestMonitorOf(t *testing.T) {
	m := MonitorOf(&app.Output{Name: "DP-1", LogicalX: -10, LogicalY: 5, LogicalW: 1280, LogicalH: 720, ModeW: 2560, ModeH: 1440, Scale: 2})
	if m != (Monitor{Connector: "DP-1", X: -10, Y: 5, Width: 1280, Height: 720}) {
		t.Fatalf("logical = %+v", m)
	}
	m = MonitorOf(&app.Output{Name: "X", ModeW: 2560, ModeH: 1440, Scale: 2})
	if m.Width != 1280 || m.Height != 720 {
		t.Fatalf("without xdg-output the mode / scale is used: %+v", m)
	}
}

func TestLabelOrigin(t *testing.T) {
	if x, y := labelOrigin(render.Rect{X: 10, Y: 100, W: 50, H: 50}, 26); x != 10 || y != 70 {
		t.Errorf("above = %d,%d", x, y)
	}
	if x, y := labelOrigin(render.Rect{X: 10, Y: 20, W: 50, H: 50}, 26); x != 10 || y != 24 {
		t.Errorf("inside = %d,%d", x, y)
	}
	if got := labelText(640, 480); got != "640 × 480" {
		t.Errorf("label = %q", got)
	}
}

// harness drives one area headlessly: the overlay has no application
// and no surfaces, only the reply channel a request would carry.
func harness(frame *image.RGBA) (*Overlay, *area, chan result) {
	o := &Overlay{monitors: mons[:1]}
	ch := make(chan result, 1)
	o.reply = ch
	a := &area{o: o, mon: mons[0], frame: frame}
	o.areas = []*area{a}
	a.Arrange(render.Rect{W: 40, H: 30})
	return o, a, ch
}

func TestDragGesture(t *testing.T) {
	t.Run("press, drag, release answers the rectangle", func(t *testing.T) {
		_, a, ch := harness(nil)
		a.HoverMove(widget.Point{X: 5, Y: 6})
		a.SetPressed(true)
		a.DragMove(widget.Point{X: 25, Y: 16})
		a.PressEnd()
		r := <-ch
		if !r.ok || r.sel != (Selection{Output: "DP-1", X: 5, Y: 6, Width: 20, Height: 10}) {
			t.Fatalf("answer = %+v", r)
		}
	})
	t.Run("a click without a drag cancels", func(t *testing.T) {
		_, a, ch := harness(nil)
		a.HoverMove(widget.Point{X: 5, Y: 6})
		a.SetPressed(true)
		a.PressEnd()
		if r := <-ch; r.ok {
			t.Fatalf("a click selected %+v", r.sel)
		}
	})
	t.Run("a finished request answers once", func(t *testing.T) {
		o, a, ch := harness(nil)
		o.finish(Selection{}, false)
		a.PressEnd()
		<-ch
		select {
		case r := <-ch:
			t.Fatalf("second answer %+v", r)
		default:
		}
	})
}

func paint(a *area) []byte {
	data := make([]byte, render.Stride(40)*30)
	cv := render.New(data, render.Stride(40), 40, 30)
	a.Paint(cv)
	return data
}

func px(data []byte, x, y int) render.Color {
	return render.ColorFromBytes(data[y*render.Stride(40)+x*4:])
}

func TestPaintLive(t *testing.T) {
	o, a, _ := harness(nil)
	o.style.Accent = render.RGB(0xb4, 0xbe, 0xfe)
	data := paint(a)
	if got := px(data, 20, 20); got != render.Color(dimAlpha<<24) {
		t.Fatalf("idle wash = %#x", uint32(got))
	}
	o.drag = &dragRect{startX: 10, startY: 10, endX: 30, endY: 25}
	data = paint(a)
	if got := px(data, 20, 18); got != 0 {
		t.Fatalf("the hole is %#x, want transparent", uint32(got))
	}
	if got := px(data, 2, 2); got != render.Color(dimAlpha<<24) {
		t.Fatalf("outside the hole %#x, want the wash", uint32(got))
	}
	if got := px(data, 10, 18); got != o.style.Accent {
		t.Fatalf("border %#x, want the accent", uint32(got))
	}
}

func TestPaintFrozen(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for i := 0; i < len(frame.Pix); i += 4 {
		frame.Pix[i], frame.Pix[i+3] = 200, 255
	}
	o, a, _ := harness(frame)
	o.drag = &dragRect{startX: 10, startY: 10, endX: 30, endY: 25}
	data := paint(a)
	if got := px(data, 20, 18); got != render.RGB(200, 0, 0) {
		t.Fatalf("inside the selection %#x, want the bright frame", uint32(got))
	}
	dim := px(data, 2, 2)
	if dim.A() != 255 || dim.R() >= 200 || dim.R() == 0 {
		t.Fatalf("outside the selection %#x, want the washed frame", uint32(dim))
	}
	if want := uint8((200*(255-dimAlpha) + 127) / 255); dim.R() != want {
		t.Fatalf("wash red %d, want %d", dim.R(), want)
	}
}

func TestWashKeepsOpaque(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	w := wash(img).RGBAAt(0, 0)
	if w.A != 255 || w.R != 166 {
		t.Fatalf("wash = %v", w)
	}
}
