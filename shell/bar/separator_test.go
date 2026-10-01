package bar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
)

func TestSeparatorPaintsCenteredLine(t *testing.T) {
	const (
		w, h = 10, 30
	)
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	module, err := Create("separator", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	s := module.Root().(*separator)
	if s.length != 24 { // 1.5 * 16
		t.Fatalf("length = %d, want 24", s.length)
	}
	s.Measure(widgetConstraintsMax(100, 100))
	s.Arrange(render.Rect{X: 0, Y: 0, W: w, H: h})

	data := make([]byte, render.Stride(w)*h)
	cv := render.New(data, render.Stride(w), w, h)
	cv.Clear(cv.Rect(), render.RGB(0, 0, 0))
	s.Paint(cv)

	pixel := func(x, y int) render.Color {
		start := y*render.Stride(w) + x*4
		return render.ColorFromBytes(data[start : start+4])
	}
	// The line is 1px wide, centered in the 10px box (x=4), and 24px
	// tall centered in the 30px box (y=3..27).
	if got := pixel(4, 15); got>>16&0xFF == 0 && got>>8&0xFF == 0 {
		t.Errorf("line center = %#08x, want the fg-subtle color", got)
	}
	if got := pixel(0, 15); got != render.RGB(0, 0, 0) {
		t.Errorf("left of the line = %#08x, want black", got)
	}
	if got := pixel(4, 0); got != render.RGB(0, 0, 0) {
		t.Errorf("above the line = %#08x, want black", got)
	}
}

func TestSeparatorMeasureIsNatural(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	module, err := Create("separator", ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := module.Root().(*separator)
	if got := s.MinSize(); got.W != 1 || got.H != 24 {
		t.Errorf("min size = %+v, want 1x24", got)
	}
	if got := s.Measure(widgetConstraintsMax(2, 2)); got.W != 1 || got.H != 2 {
		t.Errorf("clamped measure = %+v, want {1 2} (clamp caps, never grows)", got)
	}
	if got := s.HitTest(widgetPoint{X: 0, Y: 0}); got != nil {
		t.Errorf("HitTest = %v, want nil (separators are not interactive)", got)
	}
}

type widgetPoint = struct{ X, Y int }

func TestLoadFileAppliesSeparator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.separator]\ncolor = \"#f38ba8\"\nlength = \"12px\"\nsize = 2\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	sep := c.Separator
	if sep.Color.Hex != "#f38ba8" {
		t.Errorf("color = %+v", sep.Color)
	}
	if sep.Length.Unit != config.SizePixels || sep.Length.Value != 12 {
		t.Errorf("length = %+v, want 12px", sep.Length)
	}
	if sep.Size != 2 {
		t.Errorf("size = %d, want 2", sep.Size)
	}
}

func TestLoadFileRejectsBadSeparator(t *testing.T) {
	for _, content := range []string{
		"[modules.separator]\ncolor = \"not-a-token\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}
