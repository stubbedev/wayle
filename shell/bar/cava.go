package bar

import (
	"errors"
	"math"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	analyzer "github.com/stubbedev/wayle/cava"
	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// spectrumHeightRem is the visualizer's natural height (the icon-lg
// token, $base-icon-lg).
const spectrumHeightRem = 1.6

// cavaRate is the fixed capture rate the plan is built for
// (service.rs's DEFAULT_SAMPLERATE).
const cavaRate = 44100

// spectrum paints the analyzer bars. The plan runs in the module's
// update tick; the painter only reads the last frame's heights.
type spectrum struct {
	widget.Base
	bars   []float64
	peaks  []float64
	color  render.Color
	width  int
	gap    int
	pad    int
	dir    config.CavaDirection
	peaked bool
	height int
}

// newSpectrum builds the painter for one resolved cava config.
func newSpectrum(cfg config.CavaConfig, color render.Color, scale float64) *spectrum {
	height := int(math.Round(spectrumHeightRem * styling.RemBase * scale))
	pad := int(math.Round(cfg.InternalPadding.ResolvePx(styling.RemBase, scale)))
	return &spectrum{
		bars:   make([]float64, cfg.Bars),
		peaks:  make([]float64, cfg.Bars),
		color:  color,
		width:  int(cfg.BarWidth),
		gap:    int(cfg.BarGap),
		pad:    pad,
		dir:    cfg.Direction,
		peaked: cfg.Style == config.CavaStylePeaks,
		height: height,
	}
}

// naturalWidth is every bar plus its gap, plus the end padding.
func (s *spectrum) naturalWidth() int {
	n := len(s.bars)
	return 2*s.pad + n*s.width + (n-1)*s.gap
}

// SetFrame swaps in the latest heights and repaints.
func (s *spectrum) SetFrame(bars, peaks []float64) {
	copy(s.bars, bars)
	if s.peaked {
		copy(s.peaks, peaks)
	}
	s.Invalidate()
}

// Measure reports the fixed natural size.
func (s *spectrum) Measure(con widget.Constraints) widget.Size {
	return clampSize(widget.Size{W: s.naturalWidth(), H: s.height}, con)
}

// MinSize is the natural size: a visualizer squeezed narrower would
// misrepresent the bar layout.
func (s *spectrum) MinSize() widget.Size {
	return widget.Size{W: s.naturalWidth(), H: s.height}
}

// Arrange records the rect.
func (s *spectrum) Arrange(r render.Rect) { s.Base.Arrange(r) }

// Paint draws one column per bar, grown from the edge or center per
// the direction, plus peak markers in the peaks style.
func (s *spectrum) Paint(cv *render.Canvas) {
	r := s.Bounds()
	innerX := r.X + s.pad
	innerY := r.Y + s.pad
	innerH := r.H - 2*s.pad
	for i, v := range s.bars {
		if v <= 0 {
			continue
		}
		h := int(math.Round(v * float64(innerH)))
		h = max(min(h, innerH), 1)
		x := innerX + i*(s.width+s.gap)
		var y int
		switch s.dir {
		case config.CavaDirectionReverse:
			y = innerY
		case config.CavaDirectionMirror:
			y = innerY + (innerH-h)/2
		default:
			y = innerY + innerH - h
		}
		cv.RoundedRect(render.Rect{X: x, Y: y, W: s.width, H: h}, 0, s.color)
		if s.peaked && s.peaks[i] > 0 {
			p := s.peaks[i]
			ph := max(min(int(math.Round(p*float64(innerH))), innerH), 1)
			var py int
			switch s.dir {
			case config.CavaDirectionReverse:
				py = innerY + ph - 1
			case config.CavaDirectionMirror:
				py = innerY + (innerH-ph)/2 + ph - 1
			default:
				py = innerY + innerH - ph
			}
			cv.RoundedRect(render.Rect{X: x, Y: py, W: s.width, H: 1}, 0, s.color)
		}
	}
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

func newCava(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Cava
	if cfg.Style == config.CavaStyleWave {
		return nil, errors.New("cava: the wave style is not ported to the Go shell yet")
	}
	plan, err := analyzer.NewPlan(int(cfg.Bars), cavaRate, float64(cfg.NoiseReduction), true, int(cfg.LowCutoff), int(cfg.HighCutoff))
	if err != nil {
		return nil, err
	}
	paint := newSpectrum(cfg, resolveModuleColor(ctx, cfg.Color), float64(ctx.Config.Bar.Scale))
	module := &cavaModule{paint: paint, plan: plan}
	if ctx.App == nil {
		return module, nil
	}
	if ctx.Pulse == nil {
		return nil, errCavaNoAudio
	}
	source, err := analyzer.NewSource(ctx.Pulse, cfg.Source, plan.InputSize())
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
	m.paint.SetFrame(m.plan.Execute(m.source.Drained()), m.plan.Peaks())
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
