package bar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestSpectrumMeasureIsFixed(t *testing.T) {
	cfg := config.DefaultsCava()
	cfg.Bars = 10
	cfg.BarWidth = 6
	cfg.BarGap = 1
	cfg.InternalPadding = config.Size{Value: 8, Unit: config.SizePixels}
	s := newSpectrum(cfg, render.RGB(1, 2, 3), 1)
	if got := s.naturalWidth(); got != 2*8+10*6+9*1 {
		t.Errorf("natural width = %d, want %d", got, 2*8+10*6+9*1)
	}
	got := s.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 1000}})
	if got.W != s.naturalWidth() || got.H != s.height {
		t.Errorf("measure = %+v, want the natural size", got)
	}
	if s.MinSize() != got {
		t.Errorf("min size %+v != natural %+v: a squeezed visualizer lies", s.MinSize(), got)
	}
}

func TestSpectrumPaintsBarsByDirection(t *testing.T) {
	const (
		w, h = 30, 20
	)
	red := render.RGB(0xff, 0, 0)
	black := render.RGB(0, 0, 0)
	for _, tc := range []struct {
		dir config.CavaDirection
		y1  render.Color // top probe (1,1)
		y7  render.Color // middle probe (1,7)
		y18 render.Color // bottom probe (1,18)
	}{
		{config.CavaDirectionNormal, black, black, red}, // grows from the bottom: fills 10..20
		{config.CavaDirectionReverse, red, red, black},  // grows from the top: fills 0..10
		{config.CavaDirectionMirror, black, red, black}, // grows from the center: fills 5..15
	} {
		t.Run(string(tc.dir), func(t *testing.T) {
			cfg := config.DefaultsCava()
			cfg.Bars = 2
			cfg.BarWidth = 4
			cfg.BarGap = 2
			cfg.InternalPadding = config.Size{}
			cfg.Direction = tc.dir
			s := newSpectrum(cfg, red, 1)
			s.Measure(widget.Constraints{Max: widget.Size{W: 100, H: 100}})
			s.Arrange(render.Rect{X: 0, Y: 0, W: 30, H: h})

			data := make([]byte, render.Stride(w)*h)
			cv := render.New(data, render.Stride(w), w, h)
			cv.Clear(cv.Rect(), black)
			s.SetFrame([]float64{0.5, 0.5}, nil)
			s.Paint(cv)

			pixel := func(x, y int) render.Color {
				start := y*render.Stride(w) + x*4
				return render.ColorFromBytes(data[start : start+4])
			}
			for _, probe := range []struct {
				y    int
				want render.Color
				note string
			}{
				{1, tc.y1, "top"},
				{7, tc.y7, "middle"},
				{18, tc.y18, "bottom"},
			} {
				if got := pixel(1, probe.y); got != probe.want {
					t.Errorf("pixel %s = %#08x, want %#08x", probe.note, got, probe.want)
				}
			}
		})
	}
}

func TestSpectrumPeakStyleAddsMarkers(t *testing.T) {
	cfg := config.DefaultsCava()
	cfg.Bars = 1
	cfg.BarWidth = 10
	cfg.BarGap = 0
	cfg.InternalPadding = config.Size{}
	cfg.Style = config.CavaStylePeaks
	s := newSpectrum(cfg, render.RGB(0xff, 0, 0), 1)
	s.Measure(widget.Constraints{Max: widget.Size{W: 100, H: 100}})
	s.Arrange(render.Rect{X: 0, Y: 0, W: 100, H: 40})

	data := make([]byte, render.Stride(100)*40)
	cv := render.New(data, render.Stride(100), 100, 40)
	cv.Clear(cv.Rect(), render.RGB(0, 0, 0))
	// A low bar with a high peak: the marker paints near the top even
	// though the bar hugs the bottom.
	s.SetFrame([]float64{0.1}, []float64{0.9})
	s.Paint(cv)
	start := 4*render.Stride(100) + 2*4
	if got := render.ColorFromBytes(data[start : start+4]); got != render.RGB(0xff, 0, 0) {
		t.Errorf("peak marker missing near the top: pixel = %#08x", got)
	}
}

func TestNewCavaRejectsUnportedStyle(t *testing.T) {
	cfg := config.Defaults()
	cfg.Cava.Style = config.CavaStyleWave
	ctx := newTestContext(t, cfg)
	if _, err := Create("cava", ctx); err == nil {
		t.Fatal("wave style: want a not-ported error, got a module")
	}
}

func TestNewCavaHeadlessBuildsPainter(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	module, err := Create("cava", ctx)
	if err != nil {
		t.Fatalf("headless cava: %v", err)
	}
	if module.Root() == nil {
		t.Fatal("headless cava: Root = nil")
	}
}

// writeCavaConfig loads one config.toml snippet.
func writeCavaConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	return c
}

func TestLoadFileAppliesCavaModule(t *testing.T) {
	c := writeCavaConfig(t, `
[modules.cava]
bars = 12
bar-width = 4
color = "#a6e3a1"
direction = "mirror"
framerate = 30
noise-reduction = 0.5
monstercat = 1.5
internal-padding = "3px"
source = "alsa_output.monitor"
`)
	cava := c.Cava
	if cava.Bars != 12 || cava.BarWidth != 4 {
		t.Errorf("bars/width = %d/%d, want 12/4", cava.Bars, cava.BarWidth)
	}
	if cava.Color.Kind != config.ColorCustom || cava.Color.Hex != "#a6e3a1" {
		t.Errorf("color = %+v", cava.Color)
	}
	if cava.Direction != config.CavaDirectionMirror || cava.Framerate != 30 {
		t.Errorf("direction/framerate = %q/%d", cava.Direction, cava.Framerate)
	}
	if cava.NoiseReduction != 0.5 || cava.Monstercat != 1.5 {
		t.Errorf("noise/monstercat = %v/%v", cava.NoiseReduction, cava.Monstercat)
	}
	if cava.InternalPadding.Unit != config.SizePixels || cava.InternalPadding.Value != 3 {
		t.Errorf("internal-padding = %+v, want 3px", cava.InternalPadding)
	}
	if cava.Source != "alsa_output.monitor" {
		t.Errorf("source = %q", cava.Source)
	}
}

func TestLoadFileCavaDefaults(t *testing.T) {
	c := writeCavaConfig(t, "")
	if c.Cava != config.DefaultsCava() {
		t.Errorf("defaults = %+v, want %+v", c.Cava, config.DefaultsCava())
	}
}

func TestLoadFileRejectsBadCava(t *testing.T) {
	for _, content := range []string{
		"[modules.cava]\nstyle = \"spikes\"\n",
		"[modules.cava]\ndirection = \"sideways\"\n",
		"[modules.cava]\ncolor = \"not-a-token\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", strings.TrimSpace(content))
		}
	}
}
