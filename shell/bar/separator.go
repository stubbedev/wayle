package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/styling"
)

// separator module: a vertical line sized by the config, in the wayle
// margin model.
type separatorModule struct {
	paint *separator
}

func newSeparator(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Separator
	color, ok := styling.ResolveColor(cfg.Color, ctx.Style.palette)
	if !ok {
		color = render.Color(0)
	}
	length := int(math.Round(cfg.Length.ResolvePx(styling.RemBase, float64(ctx.Config.Bar.Scale))))
	m := &separatorModule{paint: newSeparatorPaint(length, int(cfg.Size), color)}
	return m, nil
}

func (m *separatorModule) Root() widget.Widget { return m.paint }

// separator paints one vertical line.
type separator struct {
	widget.Base
	length int
	size   int
	color  render.Color
}

func newSeparatorPaint(length, size int, color render.Color) *separator {
	return &separator{length: length, size: size, color: color}
}

// Measure reports the line's box.
func (s *separator) Measure(con widget.Constraints) widget.Size {
	return clampSize(widget.Size{W: s.size, H: s.length}, con)
}

// MinSize is the natural size: a squeezed separator clips, which
// reads as a rendering bug rather than a layout choice.
func (s *separator) MinSize() widget.Size { return widget.Size{W: s.size, H: s.length} }

// Arrange records the rect.
func (s *separator) Arrange(r render.Rect) { s.Base.Arrange(r) }

// Paint draws the line centered in its box.
func (s *separator) Paint(cv *render.Canvas) {
	r := s.Bounds()
	x := r.X + (r.W-s.size)/2
	y := r.Y + (r.H-s.length)/2
	cv.RoundedRect(render.Rect{X: max(x, r.X), Y: max(y, r.Y), W: s.size, H: s.length}, 0, s.color)
}

// HitTest misses: separators are not interactive.
func (s *separator) HitTest(widget.Point) widget.Widget { return nil }

// clampSize confines s to the constraint box.
func clampSize(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}
