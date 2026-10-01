package osd

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// plate paints the OSD background under its content, inside the
// revealer, so the enter and exit move the background with the rest
// (the window itself stays transparent).
type plate struct {
	widget.Base
	child widget.Widget
	bg    render.Color
}

func newPlate(child widget.Widget, bg render.Color) *plate { return &plate{child: child, bg: bg} }

func (p *plate) Measure(con widget.Constraints) widget.Size { return p.child.Measure(con) }

func (p *plate) Arrange(r render.Rect) {
	p.ArrangeSelf(r)
	widget.SetParents(p, p.child)
	p.child.Arrange(r)
}

func (p *plate) Paint(cv *render.Canvas) {
	cv.FillRect(p.Bounds(), p.bg)
	p.child.Paint(cv)
}

func (p *plate) Children() []widget.Widget { return []widget.Widget{p.child} }

func (p *plate) HitTest(pt widget.Point) widget.Widget { return p.child.HitTest(pt) }

// genieEdge is the edge a genie collapses toward for the OSD position
// (genie_edge).
func genieEdge(position config.OsdPosition) widget.Edge {
	switch position {
	case config.OsdTop, config.OsdTopLeft, config.OsdTopRight:
		return widget.EdgeTop
	case config.OsdLeft:
		return widget.EdgeLeft
	case config.OsdRight:
		return widget.EdgeRight
	}
	return widget.EdgeBottom
}
