package colorpicker

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

func TestParseHex(t *testing.T) {
	if c, ok := parseHex("#FF8000"); !ok || c != (RGB{255, 128, 0}) {
		t.Errorf("#FF8000 = %v %v", c, ok)
	}
	if c, ok := parseHex("  #000000 "); !ok || c != (RGB{}) {
		t.Errorf("padded = %v %v", c, ok)
	}
	for _, bad := range []string{"FF8000", "#FFF", "#GGGGGG", "", "#FF80001"} {
		if _, ok := parseHex(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if got := (RGB{255, 128, 0}).Hex(); got != "#FF8000" {
		t.Errorf("Hex = %q", got)
	}
}

func TestPushHistory(t *testing.T) {
	var h []RGB
	for i := range 10 {
		h = pushHistory(h, RGB{R: uint8(i)})
	}
	if len(h) != historyMax || h[0].R != 9 || h[historyMax-1].R != 2 {
		t.Fatalf("history = %v", h)
	}
	h = pushHistory(h, RGB{R: 5})
	if h[0].R != 5 || len(h) != historyMax {
		t.Fatalf("re-pick = %v", h)
	}
	for _, c := range h[1:] {
		if c.R == 5 {
			t.Fatal("the re-picked color kept its old slot")
		}
	}
}

func TestHistoryPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayle", "color-history")
	saveHistory(path, []RGB{{1, 2, 3}, {255, 255, 255}})
	if got := loadHistory(path); len(got) != 2 || got[0] != (RGB{1, 2, 3}) {
		t.Fatalf("round trip = %v", got)
	}
	if err := os.WriteFile(path, []byte("#010203\ngarbage\n#FFFFFF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadHistory(path); len(got) != 2 {
		t.Fatalf("garbage lines survived: %v", got)
	}
	if got := loadHistory(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Fatalf("missing file = %v", got)
	}
	saveHistory("", []RGB{{1, 1, 1}}) // no path: a no-op, not a crash
}

func TestHistoryPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := HistoryPath(); got != "/data/wayle/color-history" {
		t.Errorf("xdg = %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := HistoryPath(); got != "/home/u/.local/share/wayle/color-history" {
		t.Errorf("home = %q", got)
	}
	t.Setenv("HOME", "")
	if got := HistoryPath(); got != "" {
		t.Errorf("no home = %q", got)
	}
}

// gradient is a 2x physical frame over a 60x40 logical output.
func gradient() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := range 80 {
		for x := range 120 {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 2), uint8(y * 3), 7, 255})
		}
	}
	return img
}

func TestSampleAt(t *testing.T) {
	img := gradient()
	if x, y := sampleAt(img, 60, 40, 10.6, 5.2); x != 21 || y != 10 {
		t.Errorf("scaled sample = %d,%d", x, y)
	}
	if x, y := sampleAt(img, 60, 40, -5, 900); x != 0 || y != 79 {
		t.Errorf("clamped sample = %d,%d", x, y)
	}
}

func TestClickPicksAndRemembers(t *testing.T) {
	dir := t.TempDir()
	p := &Picker{historyPath: filepath.Join(dir, "h")}
	ch := make(chan result, 1)
	p.reply = ch
	l := &loupe{p: p, frame: gradient(), logicalW: 60, logicalH: 40}
	l.Arrange(render.Rect{W: 60, H: 40})
	l.ClickAt(widget.Point{X: 10, Y: 5})
	r := <-ch
	if !r.ok || r.color != (RGB{40, 30, 7}) {
		t.Fatalf("pick = %+v", r)
	}
	if len(p.history) != 1 || p.history[0] != r.color {
		t.Fatalf("history = %v", p.history)
	}
	if got := loadHistory(p.historyPath); len(got) != 1 {
		t.Fatalf("the pick was not persisted: %v", got)
	}
	red, green, blue := r.color.Float()
	if red != 40.0/255 || green != 30.0/255 || blue != 7.0/255 {
		t.Fatalf("Float = %v %v %v", red, green, blue)
	}
}

func TestCancelRemembersNothing(t *testing.T) {
	p := &Picker{historyPath: filepath.Join(t.TempDir(), "h")}
	ch := make(chan result, 1)
	p.reply = ch
	p.finish(RGB{1, 2, 3}, false)
	if r := <-ch; r.ok {
		t.Fatal("cancel reported a pick")
	}
	if len(p.history) != 0 {
		t.Fatal("cancel entered the history")
	}
	if _, err := os.Stat(p.historyPath); err == nil {
		t.Fatal("cancel wrote the history file")
	}
}

func TestLoupePaints(t *testing.T) {
	const w, h = 400, 400
	data := make([]byte, render.Stride(w)*h)
	cv := render.New(data, render.Stride(w), w, h)
	img := gradient()
	box := render.Rect{W: w, H: h}
	at := func(x, y int) render.Color { return render.ColorFromBytes(data[y*render.Stride(w)+x*4:]) }

	drawLoupe(cv, nil, img, 60, 40, box, 50, 50, []RGB{{255, 0, 0}})
	// The grid's top-left cell sits just right-below the cursor offset
	// and shows the pixel radius cells up-left of the center.
	cx, cy := sampleAt(img, 60, 40, 50, 50)
	want := img.RGBAAt(max(cx-radius, 0), max(cy-radius, 0))
	if got := at(50+24+1, 50+24+1); got != render.RGB(want.R, want.G, want.B) {
		t.Fatalf("grid cell = %#x, want %v", uint32(got), want)
	}
	// The first history swatch is red.
	swatchY := 50 + 24 + (2*radius+1)*zoom + readoutH + 5
	if got := at(50+24+5, swatchY); got != render.RGB(255, 0, 0) {
		t.Fatalf("swatch = %#x", uint32(got))
	}

	// Near the bottom-right corner the card flips to the cursor's
	// other side and stays on screen.
	for i := range data {
		data[i] = 0
	}
	drawLoupe(cv, nil, img, 60, 40, box, w-5, h-5, nil)
	if got := at(w-5-24-1, h-5-24-1); got.A() == 0 {
		t.Fatal("the flipped card did not paint up-left of the cursor")
	}
	if got := at(w-1, h-1); got.A() != 0 {
		t.Fatal("the card painted past the cursor corner")
	}
}
