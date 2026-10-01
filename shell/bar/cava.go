package bar

import (
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	analyzer "github.com/stubbedev/wayle/cava"
	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// spectrumHeightRem is the visualizer's natural thickness (the icon-lg
// token, $base-icon-lg).
const spectrumHeightRem = 1.6

// cavaRate is the fixed capture rate the plan is built for
// (service.rs's DEFAULT_SAMPLERATE).
const cavaRate = 44100

// Renderer constants (rendering/*.rs).
const (
	cavaMinBarHeight  = 2.0   // MIN_BAR_HEIGHT
	cavaMinWaveHeight = 2.0   // MIN_WAVE_HEIGHT
	cavaPeakCap       = 2.0   // PEAK_CAP_HEIGHT
	cavaPeakGravity   = 0.015 // PEAK_GRAVITY, per frame
)

// spectrum paints the analyzer frame the way the Rust module's cairo
// draw func does: bars, a filled wave, or bars with falling peak caps,
// laid along the bar (rotated a quarter turn on a vertical bar) with
// the internal padding at both ends.
type spectrum struct {
	widget.Base
	values   []float64
	peaks    []float64
	style    config.CavaStyle
	dir      config.CavaDirection
	color    render.Color
	width    float64
	gap      float64
	pad      float64
	length   int
	thick    int
	vertical bool
}

// newSpectrum builds the painter for one resolved cava config with
// bars values a frame.
func newSpectrum(cfg config.CavaConfig, bars int, color render.Color, scale float64, vertical bool) *spectrum {
	pad := cfg.InternalPadding.ResolvePx(styling.RemBase, scale)
	return &spectrum{
		values:   make([]float64, bars),
		peaks:    make([]float64, bars),
		style:    cfg.Style,
		dir:      cfg.Direction,
		color:    color,
		width:    float64(cfg.BarWidth),
		gap:      float64(cfg.BarGap),
		pad:      pad,
		length:   cavaWidgetLength(bars, int(cfg.BarWidth), int(cfg.BarGap), pad),
		thick:    int(math.Round(spectrumHeightRem * styling.RemBase * scale)),
		vertical: vertical,
	}
}

// cavaWidgetLength is calculate_widget_length: every bar and gap plus
// the padding at both ends, at least one pixel.
func cavaWidgetLength(bars, barWidth, barGap int, padding float64) int {
	total := float64(bars*barWidth) + float64(max(bars-1, 0)*barGap) + 2*padding
	return max(int(math.Round(total)), 1)
}

// SetFrame swaps in the latest values and, in the peaks style, lets
// each cap rise to its bar or fall by the gravity (update_peak).
func (s *spectrum) SetFrame(values []float64) {
	copy(s.values, values)
	if s.style == config.CavaStylePeaks {
		for i, v := range s.values {
			if v >= s.peaks[i] {
				s.peaks[i] = v
			} else {
				s.peaks[i] = max(s.peaks[i]-cavaPeakGravity, 0)
			}
		}
	}
	s.Invalidate()
}

// size is the natural size: the length along the bar, the thickness
// across it.
func (s *spectrum) size() widget.Size {
	if s.vertical {
		return widget.Size{W: s.thick, H: s.length}
	}
	return widget.Size{W: s.length, H: s.thick}
}

// Measure reports the fixed natural size.
func (s *spectrum) Measure(con widget.Constraints) widget.Size { return clampSize(s.size(), con) }

// MinSize keeps the length: a visualizer squeezed shorter would
// misrepresent the bar layout.
func (s *spectrum) MinSize() widget.Size { return s.size() }

// Arrange records the rect.
func (s *spectrum) Arrange(r render.Rect) { s.Base.Arrange(r) }

// canvas is the draw func's coordinate space: u along the bar from the
// padding's start, v across it from the top; a vertical bar turns it a
// quarter (translate to the bottom, rotate -90°), so u runs upward and
// v rightward.
type spectrumCanvas struct {
	r        render.Rect
	vertical bool
	w, h     float64 // the canvas extent along u and v
}

func (s *spectrum) canvas() spectrumCanvas {
	r := s.Bounds()
	c := spectrumCanvas{r: r, vertical: s.vertical, w: float64(r.W), h: float64(r.H)}
	if s.vertical {
		c.w, c.h = float64(r.H), float64(r.W)
	}
	return c
}

// point maps a canvas point to the widget's logical pixels.
func (c spectrumCanvas) point(u, v float64) (float64, float64) {
	if c.vertical {
		return float64(c.r.X) + v, float64(c.r.Y) + c.w - u
	}
	return float64(c.r.X) + u, float64(c.r.Y) + v
}

// fillRect fills a canvas rect as a path, so fractional bar geometry
// lands as cairo would put it.
func (c spectrumCanvas) fillRect(cv *render.Canvas, u, v, w, h float64, col render.Color) {
	var p render.Path
	p.MoveTo(c.point(u, v))
	p.LineTo(c.point(u+w, v))
	p.LineTo(c.point(u+w, v+h))
	p.LineTo(c.point(u, v+h))
	p.Close()
	cv.FillPath(&p, col)
}

// barOrigin is bar_origin_y.
func barOrigin(dir config.CavaDirection, barHeight, canvasHeight float64) float64 {
	switch dir {
	case config.CavaDirectionReverse:
		return 0
	case config.CavaDirectionMirror:
		return (canvasHeight - barHeight) / 2
	}
	return canvasHeight - barHeight
}

// Paint is the draw func: nothing before the first frame, then the
// configured style.
func (s *spectrum) Paint(cv *render.Canvas) {
	if len(s.values) == 0 {
		return
	}
	c := s.canvas()
	switch s.style {
	case config.CavaStyleWave:
		s.paintWave(cv, c)
	default:
		s.paintBars(cv, c)
	}
}

// paintBars is draw_bars, and draw_peak_bars's caps in the peaks style.
func (s *spectrum) paintBars(cv *render.Canvas, c spectrumCanvas) {
	stride := s.width + s.gap
	for i, amp := range s.values {
		u := s.pad + float64(i)*stride
		h := math.Min(math.Max(amp*c.h, cavaMinBarHeight), c.h)
		c.fillRect(cv, u, barOrigin(s.dir, h, c.h), s.width, h, s.color)
		if s.style != config.CavaStylePeaks {
			continue
		}
		peak := s.peaks[i] * c.h
		if peak <= h {
			continue
		}
		capH := math.Min(cavaPeakCap, c.h)
		switch s.dir {
		case config.CavaDirectionReverse:
			c.fillRect(cv, u, peak, s.width, capH, s.color)
		case config.CavaDirectionMirror:
			center := c.h / 2
			c.fillRect(cv, u, center-peak/2-capH, s.width, capH, s.color)
			c.fillRect(cv, u, center+peak/2, s.width, capH, s.color)
		default:
			c.fillRect(cv, u, c.h-peak-capH, s.width, capH, s.color)
		}
	}
}

// paintWave is draw_wave: a curve through each value, the control
// points halfway between neighbors, closed along the base (or, mirrored,
// back along its reflection).
func (s *spectrum) paintWave(cv *render.Canvas, c spectrumCanvas) {
	n := len(s.values)
	width := math.Max(c.w-2*s.pad, 0)
	spacing := width
	if n > 1 {
		spacing = width / float64(n-1)
	}
	minAmp := cavaMinWaveHeight / c.h
	y := func(amp float64) float64 {
		amp = math.Max(amp, minAmp)
		switch s.dir {
		case config.CavaDirectionReverse:
			return c.h * amp
		case config.CavaDirectionMirror:
			return c.h * (1 - amp) / 2
		}
		return c.h * (1 - amp)
	}
	var p render.Path
	at := func(u, v float64) (float64, float64) { return c.point(s.pad+u, v) }
	p.MoveTo(at(0, y(s.values[0])))
	curve := func(u0, v0, u1, v1 float64) {
		mid := (u0 + u1) / 2
		x1, y1 := at(mid, v0)
		x2, y2 := at(mid, v1)
		x3, y3 := at(u1, v1)
		p.CubeTo(x1, y1, x2, y2, x3, y3)
	}
	for i := 1; i < n; i++ {
		curve(float64(i-1)*spacing, y(s.values[i-1]), float64(i)*spacing, y(s.values[i]))
	}
	switch s.dir {
	case config.CavaDirectionReverse:
		p.LineTo(at(width, 0))
		p.LineTo(at(0, 0))
	case config.CavaDirectionMirror:
		center := c.h / 2
		mirror := func(i int) float64 { return center + math.Max(s.values[i], minAmp)*c.h/2 }
		p.LineTo(at(float64(n-1)*spacing, mirror(n-1)))
		for i := n - 2; i >= 0; i-- {
			curve(float64(i+1)*spacing, mirror(i+1), float64(i)*spacing, mirror(i))
		}
	default:
		p.LineTo(at(width, c.h))
		p.LineTo(at(0, c.h))
	}
	p.Close()
	cv.FillPath(&p, s.color)
}

// HitTest misses: the visualizer takes no input.
func (s *spectrum) HitTest(widget.Point) widget.Widget { return nil }

// cavaModule is the module: the analyzer plan and its capture source,
// ticked at the configured framerate.
type cavaModule struct {
	paint  *spectrum
	plan   *analyzer.Plan
	source *analyzer.Source
}

// cavaBars is the bar count and channel count the service runs with:
// stereo splits the bars between left and right, so an odd count is
// rounded up (adjusted_for_stereo).
func cavaBars(cfg config.CavaConfig) (bars, channels int) {
	bars, channels = int(cfg.Bars), 1
	if cfg.Stereo {
		channels = 2
		if bars%2 != 0 {
			log.Printf("cava: odd bar count %d rounded up to %d for stereo output", bars, bars+1)
			bars++
		}
	}
	return bars, channels
}

// cavaInputSupported reports the inputs the Go shell captures: PipeWire
// and PulseAudio, both through the PulseAudio protocol (pipewire-pulse
// on a PipeWire system). libcava's others have no capture here.
func cavaInputSupported(input config.CavaInput) bool {
	return input == config.CavaInputPipeWire || input == config.CavaInputPulse
}

func newCava(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Cava
	if !cavaInputSupported(cfg.Input) {
		return nil, fmt.Errorf("cava: the %q input is not supported by the Go shell (pipe-wire and pulse are)", cfg.Input)
	}
	// monstercat and waves are passed to libcava's config by the Rust
	// service but only its output stage (never run there) reads them,
	// so they change nothing in either shell.
	bars, channels := cavaBars(cfg)
	plan, err := analyzer.NewPlan(bars/channels, cavaRate, channels, float64(cfg.NoiseReduction), true, int(cfg.LowCutoff), int(cfg.HighCutoff))
	if err != nil {
		return nil, err
	}
	vertical := ctx.Config.Bar.Location.IsVertical()
	paint := newSpectrum(cfg, bars, resolveModuleColor(ctx, cfg.Color), float64(ctx.Config.Bar.Scale), vertical)
	module := &cavaModule{paint: paint, plan: plan}
	if ctx.App == nil {
		return module, nil
	}
	if ctx.Pulse == nil {
		return nil, errCavaNoAudio
	}
	source, err := analyzer.NewSource(ctx.Pulse, cfg.Source, channels, plan.InputSize())
	if err != nil {
		return nil, err
	}
	if err := source.Start(); err != nil {
		return nil, err
	}
	module.source = source
	ctx.App.Every(time.Second/time.Duration(cfg.Framerate), module.tick)
	return module, nil
}

var errCavaNoAudio = errors.New("cava: no audio server connection")

// tick analyzes what arrived and repaints, every frame as the Rust
// service does: with no new samples the bars still fall off. The
// capture keeps itself attached across default changes and server
// kills.
func (m *cavaModule) tick() {
	m.paint.SetFrame(m.plan.Execute(m.source.Drained()))
}

func (m *cavaModule) Root() widget.Widget { return m.paint }

// resolveModuleColor resolves a module color value against the palette.
func resolveModuleColor(ctx ModuleContext, cv config.ColorValue) render.Color {
	color, ok := styling.ResolveColor(cv, ctx.Style.palette)
	if !ok {
		return render.Color(0)
	}
	return color
}
