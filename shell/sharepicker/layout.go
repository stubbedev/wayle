package sharepicker

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// sized pins its child to a fixed size on either axis (0 leaves that
// axis to the constraints): the picker surface's size request and the
// card previews' height.
type sized struct {
	widget.Base
	child widget.Widget
	w, h  int
}

func (s *sized) Measure(con widget.Constraints) widget.Size {
	sz := s.child.Measure(con)
	if s.w > 0 {
		sz.W = min(s.w, con.Max.W)
	}
	if s.h > 0 {
		sz.H = min(s.h, con.Max.H)
	}
	return sz
}

func (s *sized) Arrange(r render.Rect) {
	s.ArrangeSelf(r)
	s.child.Arrange(r)
	widget.SetParents(s, s.child)
}

func (s *sized) Paint(cv *render.Canvas) { s.child.Paint(cv) }

func (s *sized) HitTest(p widget.Point) widget.Widget { return s.child.HitTest(p) }

func (s *sized) Children() []widget.Widget { return []widget.Widget{s.child} }

// outputSlot is one output card's place in the monitor map, in layout
// pixels (after the optional scaling).
type outputSlot struct {
	x, y, width, height int
}

// monitorArea is the pixel bounding box across every output
// (views.rs's MonitorArea).
type monitorArea struct {
	minX, maxX, minY, maxY int
	width, height          int
	aspect                 float64
}

func newMonitorArea(slots []outputSlot) monitorArea {
	if len(slots) == 0 {
		return monitorArea{}
	}
	a := monitorArea{minX: slots[0].x, minY: slots[0].y, maxX: slots[0].x + slots[0].width, maxY: slots[0].y + slots[0].height}
	for _, s := range slots[1:] {
		a.minX, a.minY = min(a.minX, s.x), min(a.minY, s.y)
		a.maxX, a.maxY = max(a.maxX, s.x+s.width), max(a.maxY, s.y+s.height)
	}
	a.width, a.height = a.maxX-a.minX, a.maxY-a.minY
	a.aspect = float64(a.width) / float64(max(a.height, 1))
	return a
}

// place maps a slot into a w x h container: the whole map scales to
// fit (by width when it is wider than the container, else by height)
// and centers in the slack (append_output_on_allocation).
func (a monitorArea) place(s outputSlot, w, h int) render.Rect {
	containerAspect := float64(w) / float64(max(h, 1))
	mw, mh := float64(max(a.width, 1)), float64(max(a.height, 1))
	tx := func(v int) float64 {
		if a.aspect > containerAspect {
			return float64(v) / mw * float64(w)
		}
		return float64(v) / mw * float64(h) * a.aspect
	}
	ty := func(v int) float64 {
		if a.aspect > containerAspect {
			return float64(v) / mh * float64(w) / a.aspect
		}
		return float64(v) / mh * float64(h)
	}
	offX := max(float64(w)-tx(a.width), 0) / 2
	offY := max(float64(h)-ty(a.height), 0) / 2
	return render.Rect{
		X: int(offX + tx(s.x-a.minX)),
		Y: int(offY + ty(s.y-a.minY)),
		W: int(tx(s.width)),
		H: int(ty(s.height)),
	}
}

// margins is the per-side spacing an output card keeps from its
// neighbors: every side not on the map's outer edge.
func (a monitorArea) margins(s outputSlot, spacing int) (left, top, right, bottom int) {
	if a.minX != s.x {
		left = spacing
	}
	if a.maxX != s.x+s.width {
		right = spacing
	}
	if a.minY != s.y {
		top = spacing
	}
	if a.maxY != s.y+s.height {
		bottom = spacing
	}
	return left, top, right, bottom
}

// outputMap lays the output cards out as the monitors are arranged.
type outputMap struct {
	widget.Base
	area    monitorArea
	slots   []outputSlot
	cards   []widget.Widget
	spacing int
}

func (m *outputMap) Measure(con widget.Constraints) widget.Size { return con.Max }

func (m *outputMap) Arrange(r render.Rect) {
	m.ArrangeSelf(r)
	for i, c := range m.cards {
		cell := m.area.place(m.slots[i], r.W, r.H)
		left, top, right, bottom := m.area.margins(m.slots[i], m.spacing)
		c.Arrange(render.Rect{
			X: r.X + cell.X + left,
			Y: r.Y + cell.Y + top,
			W: max(cell.W-left-right, 0),
			H: max(cell.H-top-bottom, 0),
		})
	}
	widget.SetParents(m, m.cards...)
}

func (m *outputMap) Paint(cv *render.Canvas) {
	for _, c := range m.cards {
		c.Paint(cv)
	}
}

func (m *outputMap) HitTest(p widget.Point) widget.Widget {
	for _, c := range m.cards {
		if hit := c.HitTest(p); hit != nil {
			return hit
		}
	}
	return nil
}

func (m *outputMap) Children() []widget.Widget { return m.cards }

// gridColumns is how many window cards share a row: the configured
// maximum, capped by the card count, never under the minimum.
func gridColumns(n, minPerRow, maxPerRow int) int {
	cols := min(maxPerRow, n)
	return max(cols, minPerRow, 1)
}
