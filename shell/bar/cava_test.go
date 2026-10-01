package bar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	analyzer "github.com/stubbedev/wayle/cava"
	"github.com/stubbedev/wayle/config"
)

var (
	cavaRed   = render.RGB(0xff, 0, 0)
	cavaBlack = render.RGB(0, 0, 0)
)

// paintSpectrum paints one frame of s into a w x h canvas and returns
// a pixel reader.
func paintSpectrum(s *spectrum, w, h int, values ...float64) func(x, y int) render.Color {
	s.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 1000}})
	s.Arrange(render.Rect{W: w, H: h})
	data := make([]byte, render.Stride(w)*h)
	cv := render.New(data, render.Stride(w), w, h)
	cv.Clear(cv.Rect(), cavaBlack)
	if values != nil {
		s.SetFrame(values)
	}
	s.Paint(cv)
	return func(x, y int) render.Color {
		start := y*render.Stride(w) + x*4
		return render.ColorFromBytes(data[start : start+4])
	}
}

func cavaTestConfig(bars int) config.CavaConfig {
	cfg := config.DefaultsCava()
	cfg.Bars = config.BarCount(bars)
	cfg.BarWidth = 4
	cfg.BarGap = 2
	cfg.InternalPadding = config.Size{}
	return cfg
}

func TestSpectrumLength(t *testing.T) {
	cfg := cavaTestConfig(10)
	cfg.BarWidth, cfg.BarGap = 6, 1
	cfg.InternalPadding = config.Size{Value: 8, Unit: config.SizePixels}
	s := newSpectrum(cfg, 10, cavaRed, 1, false)
	if s.length != 2*8+10*6+9*1 {
		t.Errorf("length = %d, want %d", s.length, 2*8+10*6+9*1)
	}
	got := s.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 1000}})
	if got != (widget.Size{W: s.length, H: s.thick}) || s.MinSize() != got {
		t.Errorf("horizontal size = %+v", got)
	}
	v := newSpectrum(cfg, 10, cavaRed, 1, true)
	if got := v.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 1000}}); got != (widget.Size{W: v.thick, H: v.length}) {
		t.Errorf("vertical size = %+v, want the length along the height", got)
	}
	if cavaWidgetLength(1, 3, 1, 0) != 3 || cavaWidgetLength(20, 3, 1, 8) != 95 || cavaWidgetLength(0, 3, 1, 0) != 1 {
		t.Error("calculate_widget_length")
	}
}

func TestSpectrumPaintsBarsByDirection(t *testing.T) {
	for _, tc := range []struct {
		dir           config.CavaDirection
		top, mid, bot render.Color // probes at y 1, 7, 18 of a 20px canvas
	}{
		{config.CavaDirectionNormal, cavaBlack, cavaBlack, cavaRed}, // 10..20
		{config.CavaDirectionReverse, cavaRed, cavaRed, cavaBlack},  // 0..10
		{config.CavaDirectionMirror, cavaBlack, cavaRed, cavaBlack}, // 5..15
	} {
		cfg := cavaTestConfig(2)
		cfg.Direction = tc.dir
		px := paintSpectrum(newSpectrum(cfg, 2, cavaRed, 1, false), 30, 20, 0.5, 0.5)
		if px(1, 1) != tc.top || px(1, 7) != tc.mid || px(1, 18) != tc.bot {
			t.Errorf("%s: probes %#08x %#08x %#08x", tc.dir, px(1, 1), px(1, 7), px(1, 18))
		}
		if px(5, 18) != cavaBlack && tc.dir == config.CavaDirectionNormal {
			t.Error("the gap between bars was painted")
		}
	}
}

func TestSpectrumSilentBarsKeepTheirMinimum(t *testing.T) {
	px := paintSpectrum(newSpectrum(cavaTestConfig(1), 1, cavaRed, 1, false), 10, 20, 0)
	if px(1, 19) != cavaRed || px(1, 18) != cavaRed || px(1, 17) != cavaBlack {
		t.Error("a silent bar must still draw MIN_BAR_HEIGHT (2px)")
	}
	// Before the first frame nothing paints.
	empty := newSpectrum(cavaTestConfig(0), 0, cavaRed, 1, false)
	if px := paintSpectrum(empty, 10, 20); px(1, 19) != cavaBlack {
		t.Error("an empty frame painted")
	}
}

func TestSpectrumPadsOnlyAlongTheBar(t *testing.T) {
	cfg := cavaTestConfig(1)
	cfg.InternalPadding = config.Size{Value: 5, Unit: config.SizePixels}
	px := paintSpectrum(newSpectrum(cfg, 1, cavaRed, 1, false), 20, 20, 1)
	if px(2, 10) != cavaBlack || px(6, 10) != cavaRed {
		t.Error("the bar does not start after the padding")
	}
	if px(6, 0) != cavaRed || px(6, 19) != cavaRed {
		t.Error("a full bar must run the whole height: the padding is only along the bar")
	}
}

func TestSpectrumPeakCapsFall(t *testing.T) {
	cfg := cavaTestConfig(1)
	cfg.BarWidth = 10
	cfg.Style = config.CavaStylePeaks
	s := newSpectrum(cfg, 1, cavaRed, 1, false)
	paintSpectrum(s, 20, 40, 0.9)
	px := paintSpectrum(s, 20, 40, 0.1)
	if s.peaks[0] != 0.9-cavaPeakGravity {
		t.Errorf("peak = %v, want 0.9 less one frame of gravity", s.peaks[0])
	}
	// The cap sits just above the fallen peak, 40*(1-0.885) = 4.6px down.
	if px(2, 3) != cavaRed || px(2, 10) != cavaBlack {
		t.Errorf("cap probes %#08x / %#08x", px(2, 3), px(2, 10))
	}
	bars := newSpectrum(cavaTestConfig(1), 1, cavaRed, 1, false)
	paintSpectrum(bars, 20, 40, 0.9)
	if px := paintSpectrum(bars, 20, 40, 0.1); px(2, 3) != cavaBlack {
		t.Error("the bars style drew a peak cap")
	}
}

func TestSpectrumWave(t *testing.T) {
	for _, tc := range []struct {
		dir           config.CavaDirection
		top, mid, bot render.Color
	}{
		{config.CavaDirectionNormal, cavaBlack, cavaBlack, cavaRed},
		{config.CavaDirectionReverse, cavaRed, cavaBlack, cavaBlack},
		{config.CavaDirectionMirror, cavaBlack, cavaRed, cavaBlack},
	} {
		cfg := cavaTestConfig(3)
		cfg.Style = config.CavaStyleWave
		cfg.Direction = tc.dir
		px := paintSpectrum(newSpectrum(cfg, 3, cavaRed, 1, false), 30, 40, 0.4, 0.4, 0.4)
		if px(15, 2) != tc.top || px(15, 20) != tc.mid || px(15, 37) != tc.bot {
			t.Errorf("%s wave probes %#08x %#08x %#08x", tc.dir, px(15, 2), px(15, 20), px(15, 37))
		}
	}
	// A peak in the middle rises above its flat neighbors.
	cfg := cavaTestConfig(3)
	cfg.Style = config.CavaStyleWave
	px := paintSpectrum(newSpectrum(cfg, 3, cavaRed, 1, false), 30, 40, 0.1, 0.9, 0.1)
	if px(15, 10) != cavaRed || px(1, 10) != cavaBlack {
		t.Error("the curve does not follow the values")
	}
}

func TestSpectrumVerticalTurnsAQuarter(t *testing.T) {
	cfg := cavaTestConfig(2)
	s := newSpectrum(cfg, 2, cavaRed, 1, true)
	// On a vertical bar the first bar sits at the bottom and grows from
	// the right edge leftward (translate to the bottom, rotate -90°).
	px := paintSpectrum(s, 20, 30, 0.5, 0)
	if px(15, 28) != cavaRed || px(5, 28) != cavaBlack {
		t.Errorf("first bar probes %#08x / %#08x", px(15, 28), px(5, 28))
	}
	if px(15, 23) != cavaBlack || px(19, 23) != cavaRed {
		t.Error("the second (silent) bar must be its 2px minimum at the right edge")
	}
}

func TestCavaBarsSplitForStereo(t *testing.T) {
	cfg := config.DefaultsCava()
	cfg.Bars = 21
	if b, ch := cavaBars(cfg); b != 21 || ch != 1 {
		t.Errorf("mono = %d/%d", b, ch)
	}
	cfg.Stereo = true
	if b, ch := cavaBars(cfg); b != 22 || ch != 2 {
		t.Errorf("stereo of 21 = %d/%d, want 22 over 2 channels", b, ch)
	}
	cfg.Bars = 20
	if b, _ := cavaBars(cfg); b != 20 {
		t.Errorf("stereo of 20 = %d", b)
	}
}

func TestNewCavaBuildsEveryStyleAndStereo(t *testing.T) {
	for _, style := range []config.CavaStyle{config.CavaStyleBars, config.CavaStyleWave, config.CavaStylePeaks} {
		cfg := config.Defaults()
		cfg.Cava.Style = style
		cfg.Cava.Stereo = true
		cfg.Cava.Bars = 9
		module, err := Create("cava", newTestContext(t, cfg))
		if err != nil {
			t.Fatalf("%s: %v", style, err)
		}
		m := module.(*cavaModule)
		if m.plan.Channels() != 2 || len(m.paint.values) != 10 {
			t.Errorf("%s stereo: %d channels, %d values", style, m.plan.Channels(), len(m.paint.values))
		}
		if got := len(m.plan.Execute(nil)); got != 10 {
			t.Errorf("a stereo frame has %d values, want 10", got)
		}
	}
}

func TestNewCavaRejectsAnUnsupportedInput(t *testing.T) {
	for input, ok := range map[config.CavaInput]bool{
		config.CavaInputPipeWire: true, config.CavaInputPulse: true,
		config.CavaInputFifo: true, config.CavaInputShmem: true,
		config.CavaInputAlsa: false, config.CavaInputJack: false, config.CavaInputOss: false,
		config.CavaInputSndio: false, config.CavaInputPortAudio: false, config.CavaInputWinscap: false,
	} {
		cfg := config.Defaults()
		cfg.Cava.Input = input
		_, err := Create("cava", newTestContext(t, cfg))
		if (err == nil) != ok {
			t.Errorf("input %q: err %v", input, err)
		}
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

// TestCavaInputPicksTheCapture pins the input dispatch: fifo and
// shmem read their source path, the rest record through pulse, which
// they need.
func TestCavaInputPicksTheCapture(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	cfg := config.DefaultsCava()
	cfg.Input, cfg.Source = config.CavaInputFifo, "/tmp/cava.fifo"
	if in, err := cavaInput(ctx, cfg, 1, 4096); err != nil {
		t.Errorf("fifo: %v", err)
	} else if _, ok := in.(*analyzer.FifoInput); !ok {
		t.Errorf("fifo built %T", in)
	}
	cfg.Input = config.CavaInputShmem
	if in, err := cavaInput(ctx, cfg, 1, 4096); err != nil {
		t.Errorf("shmem: %v", err)
	} else if _, ok := in.(*analyzer.ShmemInput); !ok {
		t.Errorf("shmem built %T", in)
	}
	cfg.Input = config.CavaInputPipeWire
	ctx.Pulse = nil
	if _, err := cavaInput(ctx, cfg, 1, 4096); err == nil {
		t.Error("pipe-wire without an audio connection built a capture")
	}
}

type fakeCavaInput struct{ stopped chan struct{} }

func (f *fakeCavaInput) Start() error       { return nil }
func (f *fakeCavaInput) Drained() []float64 { return nil }
func (f *fakeCavaInput) Stop()              { close(f.stopped) }

// TestCavaModuleEndsWithItsGeneration pins the lifetime: the frame
// ticker runs while the bar generation lives and, once it retires,
// the ticker and the capture stop on the loop.
func TestCavaModuleEndsWithItsGeneration(t *testing.T) {
	plan, err := analyzer.NewPlan(10, 44100, 1, 0.77, true, 50, 10000)
	if err != nil {
		t.Fatal(err)
	}
	in := &fakeCavaInput{stopped: make(chan struct{})}
	m := &cavaModule{paint: newSpectrum(config.DefaultsCava(), 10, 0, 1, false), plan: plan, source: in}
	life, retire := context.WithCancel(context.Background())
	ticking := true
	var invoked int
	m.follow(life, time.Second/60,
		func(time.Duration, func()) func() { return func() { ticking = false } },
		func(fn func()) { invoked++; fn() })
	if !ticking {
		t.Fatal("the ticker stopped before the generation retired")
	}
	retire()
	select {
	case <-in.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the capture outlived its generation")
	}
	if ticking || invoked != 1 {
		t.Errorf("after retiring: ticking %v, %d invokes; want stopped on the loop once", ticking, invoked)
	}
}
