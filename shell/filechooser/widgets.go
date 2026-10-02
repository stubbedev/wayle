package filechooser

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// The chooser's layout pieces gelm has no widget for: a drag surface
// (the title bar that moves the sheet, the column and corner grips)
// and the free placement of a child inside a larger rect (the sheet on
// the full-screen overlay, the cards over the file list).

func clamp(s widget.Size, con widget.Constraints) widget.Size {
	return widget.Size{
		W: min(max(s.W, con.Min.W), con.Max.W),
		H: min(max(s.H, con.Min.H), con.Max.H),
	}
}

// dragArea reports a press-and-drag as the offset from the press
// point. With a child (a title bar's buttons and title) presses on the
// child's controls stay theirs and everything else is the drag; with
// none it is a w x h grip.
type dragArea struct {
	widget.Base
	child  widget.Widget
	w, h   int
	cursor string
	// last is the hovered point, which the press anchors on: the
	// router feeds no motion between the press and the first drag.
	last, anchor widget.Point
	onStart      func()
	onDrag       func(dx, dy int)
	onEnd        func()
}

func (d *dragArea) Measure(con widget.Constraints) widget.Size {
	if d.child != nil {
		return d.child.Measure(con)
	}
	return clamp(widget.Size{W: d.w, H: d.h}, con)
}

func (d *dragArea) Arrange(r render.Rect) {
	d.ArrangeSelf(r)
	if d.child != nil {
		d.child.Arrange(r)
		widget.SetParents(d, d.child)
	}
}

func (d *dragArea) ArrangeRoot(r render.Rect) { d.Arrange(r) }

func (d *dragArea) Paint(cv *render.Canvas) {
	if d.child != nil {
		d.child.Paint(cv)
	}
}

func (d *dragArea) HitTest(p widget.Point) widget.Widget {
	if d.child != nil {
		if hit := d.child.HitTest(p); hit != nil && widget.IsInteractive(hit) {
			return hit
		}
	}
	return d.HitLeaf(d, p)
}

func (d *dragArea) Children() []widget.Widget {
	if d.child == nil {
		return nil
	}
	return []widget.Widget{d.child}
}

// HoverMove tracks the point a press will anchor on.
func (d *dragArea) HoverMove(p widget.Point) { d.last = p }

// SetPressed anchors the drag.
func (d *dragArea) SetPressed(on bool) {
	if on {
		d.anchor = d.last
		if d.onStart != nil {
			d.onStart()
		}
	}
}

// DragMove reports the offset from the anchor.
func (d *dragArea) DragMove(p widget.Point) {
	if d.onDrag != nil {
		d.onDrag(p.X-d.anchor.X, p.Y-d.anchor.Y)
	}
}

// PressEnd ends the drag.
func (d *dragArea) PressEnd() {
	if d.onEnd != nil {
		d.onEnd()
	}
}

// CursorName is the grip's pointer shape.
func (d *dragArea) CursorName() string { return d.cursor }

// align is a placement along one axis.
type align uint8

const (
	alignStart align = iota
	alignCenter
	alignEnd
)

func (a align) offset(free int) int {
	switch a {
	case alignCenter:
		return free / 2
	case alignEnd:
		return free
	}
	return 0
}

// place puts a child at its natural size inside its rect, aligned and
// inset by margins, and is transparent around it: a card over the
// list takes no input outside itself.
type place struct {
	widget.Base
	child            widget.Widget
	h, v             align
	left, top, right int
	bottom           int
}

func newPlace(child widget.Widget, h, v align) *place { return &place{child: child, h: h, v: v} }

func (p *place) inner(r render.Rect) render.Rect {
	return render.Rect{
		X: r.X + p.left, Y: r.Y + p.top,
		W: max(r.W-p.left-p.right, 0), H: max(r.H-p.top-p.bottom, 0),
	}
}

func (p *place) Measure(con widget.Constraints) widget.Size {
	mx, my := p.left+p.right, p.top+p.bottom
	sz := p.child.Measure(widget.Constraints{Max: widget.Size{W: max(con.Max.W-mx, 0), H: max(con.Max.H-my, 0)}})
	return clamp(widget.Size{W: sz.W + mx, H: sz.H + my}, con)
}

func (p *place) Arrange(r render.Rect) {
	p.ArrangeSelf(r)
	in := p.inner(r)
	sz := p.child.Measure(widget.Constraints{Max: widget.Size{W: in.W, H: in.H}})
	sz.W, sz.H = min(sz.W, in.W), min(sz.H, in.H)
	p.child.Arrange(render.Rect{
		X: in.X + p.h.offset(in.W-sz.W), Y: in.Y + p.v.offset(in.H-sz.H),
		W: sz.W, H: sz.H,
	})
	widget.SetParents(p, p.child)
}

func (p *place) ArrangeRoot(r render.Rect) { p.Arrange(r) }

func (p *place) Paint(cv *render.Canvas) {
	if widget.IsVisible(p.child) {
		p.child.Paint(cv)
	}
}

func (p *place) HitTest(pt widget.Point) widget.Widget {
	if !widget.IsVisible(p.child) {
		return nil
	}
	return p.child.HitTest(pt)
}

func (p *place) Children() []widget.Widget { return []widget.Widget{p.child} }

// The sheet's size bounds: the floor keeps the sidebar and columns
// usable, the ceiling a sane maximum on huge outputs.
const (
	sheetMinW, sheetMinH = 480, 320
	sheetMaxW, sheetMaxH = 1600, 1100
)

// sheet is the chooser's movable, resizable panel on the full-screen
// overlay: centred until moved, kept inside the output, and its top
// left corner stays put while the corner grip resizes it.
type sheet struct {
	widget.Base
	child  widget.Widget
	w, h   int
	x, y   int
	placed bool
	// onDrop receives the first path of a file dropped on the sheet.
	onDrop func(path string)
}

func newSheet(child widget.Widget, w, h int) *sheet { return &sheet{child: child, w: w, h: h} }

func (s *sheet) Measure(con widget.Constraints) widget.Size { return con.Max }

func (s *sheet) Arrange(r render.Rect) {
	s.ArrangeSelf(r)
	s.w = min(max(s.w, sheetMinW), sheetMaxW, max(r.W, 1))
	s.h = min(max(s.h, sheetMinH), sheetMaxH, max(r.H, 1))
	if !s.placed {
		s.x, s.y, s.placed = (r.W-s.w)/2, (r.H-s.h)/2, true
	}
	s.x = min(max(s.x, 0), max(r.W-s.w, 0))
	s.y = min(max(s.y, 0), max(r.H-s.h, 0))
	// Containers arrange from what they measured; nothing above
	// measures the child at the sheet's size.
	s.child.Measure(widget.Constraints{Min: widget.Size{W: s.w, H: s.h}, Max: widget.Size{W: s.w, H: s.h}})
	s.child.Arrange(render.Rect{X: r.X + s.x, Y: r.Y + s.y, W: s.w, H: s.h})
	widget.SetParents(s, s.child)
}

func (s *sheet) ArrangeRoot(r render.Rect) { s.Arrange(r) }

func (s *sheet) Paint(cv *render.Canvas) { s.child.Paint(cv) }

func (s *sheet) HitTest(p widget.Point) widget.Widget { return s.child.HitTest(p) }

func (s *sheet) Children() []widget.Widget { return []widget.Widget{s.child} }

// moveTo places the top left corner (clamped on the next arrange).
func (s *sheet) moveTo(x, y int) {
	s.x, s.y, s.placed = x, y, true
	s.InvalidateLayout()
}

// resize sets the size (clamped on the next arrange).
func (s *sheet) resize(w, h int) {
	s.w, s.h = w, h
	s.InvalidateLayout()
}
