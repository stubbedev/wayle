package launcher

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// rowPx is the fixed row height the list is sized and laid out with
// (views.rs ROW_PX).
const rowPx = 40

// resultList is the virtualized results view: only the rows inside the
// viewport exist as widgets, so a ten-thousand-row dmenu costs a
// viewport's worth. It owns the selection and the scroll offset; the
// row widgets are the surface's (factory), styled by CSS classes. It is
// a widget of its own rather than a gelm List because the launcher
// binds every pointer button, click count, and wheel direction itself.
type resultList struct {
	widget.Base
	count    int
	factory  func(i int) widget.Widget
	selected int
	offset   int
	// visibleRows is how many rows the viewport shows.
	visibleRows int
	// fixed holds the viewport at visibleRows rows (-fixed-num-lines);
	// otherwise it is as tall as its rows, up to visibleRows.
	fixed bool
	rows  map[int]widget.Widget
	// onScroll consumes a wheel step the bindings claim (true).
	onScroll func(dy int) bool
}

func newResultList(factory func(i int) widget.Widget) *resultList {
	l := &resultList{factory: factory, rows: map[int]widget.Widget{}, visibleRows: 10, selected: -1}
	// GTK's ListView element, so the stylesheet's listview > row rules
	// reach the rows.
	l.SetElement("listview")
	return l
}

// reset replaces the row count, dropping every cached row.
func (l *resultList) reset(count int) {
	l.count = count
	l.rows = map[int]widget.Widget{}
	l.offset = min(l.offset, max(0, count-l.visibleRows))
	l.selected = min(l.selected, count-1)
	l.InvalidateLayout()
}

// refresh rebuilds one row (a ballot toggle).
func (l *resultList) refresh(i int) {
	delete(l.rows, i)
	l.InvalidateLayout()
}

// setSelected moves the selection and scrolls it into view; -1 clears.
func (l *resultList) setSelected(i int) {
	if i >= l.count {
		i = l.count - 1
	}
	if i < 0 {
		i = -1
	}
	if i == l.selected {
		return
	}
	prev := l.selected
	l.selected = i
	if i >= 0 {
		if i < l.offset {
			l.offset = i
		} else if i >= l.offset+l.visibleRows {
			l.offset = i - l.visibleRows + 1
		}
	}
	delete(l.rows, prev)
	delete(l.rows, i)
	l.InvalidateLayout()
}

// visible is the half-open range of rows in the viewport.
func (l *resultList) visible() (int, int) {
	first := min(l.offset, max(0, l.count-1))
	return first, min(l.count, first+l.visibleRows)
}

// rowAt returns the row widget for index i, building it when absent.
func (l *resultList) rowAt(i int) widget.Widget {
	if w, ok := l.rows[i]; ok {
		return w
	}
	w := l.factory(i)
	l.rows[i] = w
	return w
}

// Measure is the viewport: the offered width by visibleRows rows when
// fixed, by its rows up to visibleRows otherwise.
func (l *resultList) Measure(con widget.Constraints) widget.Size {
	rows := l.visibleRows
	if !l.fixed {
		rows = min(l.count, l.visibleRows)
	}
	return widget.Size{
		W: min(max(con.Max.W, con.Min.W), con.Max.W),
		H: min(max(rows*rowPx, con.Min.H), con.Max.H),
	}
}

// Arrange lays the visible rows out and evicts the rest.
func (l *resultList) Arrange(r render.Rect) {
	l.ArrangeSelf(r)
	first, last := l.visible()
	for i := range l.rows {
		if i < first || i >= last {
			delete(l.rows, i)
		}
	}
	kids := make([]widget.Widget, 0, last-first)
	for i := first; i < last; i++ {
		w := l.rowAt(i)
		w.Arrange(render.Rect{X: r.X, Y: r.Y + (i-first)*rowPx, W: r.W, H: rowPx})
		kids = append(kids, w)
	}
	widget.SetParents(l, kids...)
}

// ArrangeRoot mirrors Arrange for the root path.
func (l *resultList) ArrangeRoot(r render.Rect) { l.Arrange(r) }

// Paint paints the visible rows inside the viewport.
func (l *resultList) Paint(cv *render.Canvas) {
	prev := cv.PushClip(l.Bounds())
	first, last := l.visible()
	for i := first; i < last; i++ {
		if w, ok := l.rows[i]; ok {
			widget.PaintChild(cv, w)
		}
	}
	cv.PopClip(prev)
}

// Children exposes the visible rows to the style cascade and hover.
func (l *resultList) Children() []widget.Widget {
	first, last := l.visible()
	out := make([]widget.Widget, 0, last-first)
	for i := first; i < last; i++ {
		if w, ok := l.rows[i]; ok {
			out = append(out, w)
		}
	}
	return out
}

// HitTest descends into the row under p.
func (l *resultList) HitTest(p widget.Point) widget.Widget {
	b := l.Bounds()
	if p.X < b.X || p.Y < b.Y || p.X >= b.X+b.W || p.Y >= b.Y+b.H {
		return nil
	}
	first, last := l.visible()
	if i := first + (p.Y-b.Y)/rowPx; i < last {
		if w, ok := l.rows[i]; ok {
			if hit := w.HitTest(p); hit != nil {
				return hit
			}
		}
	}
	return l.HitLeaf(l, p)
}

// ScrollInput gives the bindings the wheel first (ml-row-up/down move
// the selection rather than the viewport); unclaimed, it scrolls the
// viewport. Either way the step stops here.
func (l *resultList) ScrollInput(dy int) bool {
	if l.onScroll == nil || !l.onScroll(dy) {
		l.ScrollBy(0, dy)
	}
	return true
}

// ScrollBy scrolls the viewport by whole rows.
func (l *resultList) ScrollBy(_, dy int) {
	next := min(max(0, l.offset+dy), max(0, l.count-l.visibleRows))
	if next != l.offset {
		l.offset = next
		l.InvalidateLayout()
	}
}
