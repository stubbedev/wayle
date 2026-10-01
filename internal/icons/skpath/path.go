package skpath

import "slices"

// Verb is one path command.
type Verb uint8

// The verbs.
const (
	VerbMove Verb = iota
	VerbLine
	VerbQuad
	VerbCubic
	VerbClose
)

// Path is an immutable path with at least two verbs (tiny-skia's Path).
type Path struct {
	verbs  []Verb
	points []Point
	bounds Rect
}

// Len is the verb count.
func (p *Path) Len() int { return len(p.verbs) }

// Bounds is the control-point bounds.
func (p *Path) Bounds() Rect { return p.bounds }

// SegmentKind tags a Segment.
type SegmentKind uint8

// The segment kinds.
const (
	SegMove SegmentKind = iota
	SegLine
	SegQuad
	SegCubic
	SegClose
)

// Segment is one iterated path segment; P holds its points (the target
// last).
type Segment struct {
	Kind SegmentKind
	P    [3]Point
}

// segmentsIter is PathSegmentsIter.
type segmentsIter struct {
	path        *Path
	verbIndex   int
	pointsIndex int
	autoClose   bool
	lastMoveTo  Point
	lastPoint   Point
}

// Segments iterates the path.
func (p *Path) Segments() *segmentsIter { return &segmentsIter{path: p} }

func (it *segmentsIter) next() (Segment, bool) {
	if it.verbIndex >= len(it.path.verbs) {
		return Segment{}, false
	}
	verb := it.path.verbs[it.verbIndex]
	it.verbIndex++
	pts := it.path.points
	switch verb {
	case VerbMove:
		it.pointsIndex++
		it.lastMoveTo = pts[it.pointsIndex-1]
		it.lastPoint = it.lastMoveTo
		return Segment{Kind: SegMove, P: [3]Point{it.lastMoveTo}}, true
	case VerbLine:
		it.pointsIndex++
		it.lastPoint = pts[it.pointsIndex-1]
		return Segment{Kind: SegLine, P: [3]Point{it.lastPoint}}, true
	case VerbQuad:
		it.pointsIndex += 2
		it.lastPoint = pts[it.pointsIndex-1]
		return Segment{Kind: SegQuad, P: [3]Point{pts[it.pointsIndex-2], it.lastPoint}}, true
	case VerbCubic:
		it.pointsIndex += 3
		it.lastPoint = pts[it.pointsIndex-1]
		return Segment{Kind: SegCubic, P: [3]Point{pts[it.pointsIndex-3], pts[it.pointsIndex-2], it.lastPoint}}, true
	default:
		seg := it.autoCloseSeg()
		it.lastPoint = it.lastMoveTo
		return seg, true
	}
}

// Next returns the next segment.
func (it *segmentsIter) Next() (Segment, bool) { return it.next() }

func (it *segmentsIter) autoCloseSeg() Segment {
	if it.autoClose && it.lastPoint != it.lastMoveTo {
		it.verbIndex--
		return Segment{Kind: SegLine, P: [3]Point{it.lastMoveTo}}
	}
	return Segment{Kind: SegClose}
}

func (it *segmentsIter) nextVerb() (Verb, bool) {
	if it.verbIndex < len(it.path.verbs) {
		return it.path.verbs[it.verbIndex], true
	}
	return 0, false
}

func (it *segmentsIter) hasValidTangent() bool {
	c := *it
	for {
		seg, ok := c.next()
		if !ok {
			return false
		}
		switch seg.Kind {
		case SegMove, SegClose:
			return false
		case SegLine:
			if c.lastPoint == seg.P[0] {
				continue
			}
			return true
		case SegQuad:
			if c.lastPoint == seg.P[0] && c.lastPoint == seg.P[1] {
				continue
			}
			return true
		case SegCubic:
			if c.lastPoint == seg.P[0] && c.lastPoint == seg.P[1] && c.lastPoint == seg.P[2] {
				continue
			}
			return true
		}
	}
}

// Transform maps the path through ts; false when the result has no
// finite bounds.
func (p *Path) Transform(ts Transform) (*Path, bool) {
	if ts.IsIdentity() {
		return p, true
	}
	pts := append([]Point(nil), p.points...)
	ts.MapPoints(pts)
	bounds, ok := rectFromPoints(pts)
	if !ok {
		return nil, false
	}
	return &Path{verbs: p.verbs, points: pts, bounds: bounds}, true
}

// ComputeTightBounds is Path::compute_tight_bounds.
func (p *Path) ComputeTightBounds() (Rect, bool) {
	var extremas [5]Point
	mn, mx := p.points[0], p.points[0]
	it := p.Segments()
	var last Point
	for {
		seg, ok := it.next()
		if !ok {
			break
		}
		count := 0
		switch seg.Kind {
		case SegMove, SegLine:
			extremas[0] = seg.P[0]
			count = 1
		case SegQuad:
			count = computeQuadExtremas(last, seg.P[0], seg.P[1], &extremas)
		case SegCubic:
			count = computeCubicExtremas(last, seg.P[0], seg.P[1], seg.P[2], &extremas)
		}
		last = it.lastPoint
		for _, e := range extremas[:count] {
			mn.X = minf(mn.X, e.X)
			mn.Y = minf(mn.Y, e.Y)
			mx.X = maxf(mx.X, e.X)
			mx.Y = maxf(mx.Y, e.Y)
		}
	}
	return RectFromLTRB(mn.X, mn.Y, mx.X, mx.Y)
}

func computeQuadExtremas(p0, p1, p2 Point, extremas *[5]Point) int {
	src := [3]Point{p0, p1, p2}
	i := 0
	if t, ok := validUnitDivide(p0.X-p1.X, p0.X-p1.X-p1.X+p2.X); ok {
		extremas[i] = evalQuadAt(&src, t)
		i++
	}
	if t, ok := validUnitDivide(p0.Y-p1.Y, p0.Y-p1.Y-p1.Y+p2.Y); ok {
		extremas[i] = evalQuadAt(&src, t)
		i++
	}
	extremas[i] = p2
	return i + 1
}

func computeCubicExtremas(p0, p1, p2, p3 Point, extremas *[5]Point) int {
	var ts0, ts1 [3]float32
	n0 := findCubicExtrema(p0.X, p1.X, p2.X, p3.X, &ts0)
	n1 := findCubicExtrema(p0.Y, p1.Y, p2.Y, p3.Y, &ts1)
	src := [4]Point{p0, p1, p2, p3}
	i := 0
	for _, t := range ts0[:n0] {
		extremas[i] = evalCubicPosAt(&src, t)
		i++
	}
	for _, t := range ts1[:n1] {
		extremas[i] = evalCubicPosAt(&src, t)
		i++
	}
	extremas[n0+n1] = p3
	return n0 + n1 + 1
}

// Builder accumulates a path (tiny-skia's PathBuilder, i.e. SkPath).
type Builder struct {
	verbs           []Verb
	points          []Point
	lastMoveToIndex int
	moveToRequired  bool
}

// NewBuilder returns an empty builder.
func NewBuilder() *Builder { return &Builder{moveToRequired: true} }

// FromRect is PathBuilder::from_rect.
func FromRect(r Rect) *Path {
	return &Path{
		verbs:  []Verb{VerbMove, VerbLine, VerbLine, VerbLine, VerbClose},
		points: []Point{{r.Left, r.Top}, {r.Right, r.Top}, {r.Right, r.Bottom}, {r.Left, r.Bottom}},
		bounds: r,
	}
}

// Len is the verb count.
func (b *Builder) Len() int { return len(b.verbs) }

// IsEmpty reports no verbs.
func (b *Builder) IsEmpty() bool { return len(b.verbs) == 0 }

// MoveTo starts a contour (replacing a trailing move).
func (b *Builder) MoveTo(x, y float32) {
	if n := len(b.verbs); n > 0 && b.verbs[n-1] == VerbMove {
		b.points[len(b.points)-1] = Point{x, y}
		return
	}
	b.lastMoveToIndex = len(b.points)
	b.moveToRequired = false
	b.verbs = append(b.verbs, VerbMove)
	b.points = append(b.points, Point{x, y})
}

func (b *Builder) injectMoveToIfNeeded() {
	if !b.moveToRequired {
		return
	}
	if b.lastMoveToIndex < len(b.points) {
		p := b.points[b.lastMoveToIndex]
		b.MoveTo(p.X, p.Y)
	} else {
		b.MoveTo(0, 0)
	}
}

// LineTo adds a line.
func (b *Builder) LineTo(x, y float32) {
	b.injectMoveToIfNeeded()
	b.verbs = append(b.verbs, VerbLine)
	b.points = append(b.points, Point{x, y})
}

// QuadTo adds a quadratic.
func (b *Builder) QuadTo(x1, y1, x, y float32) {
	b.injectMoveToIfNeeded()
	b.verbs = append(b.verbs, VerbQuad)
	b.points = append(b.points, Point{x1, y1}, Point{x, y})
}

func (b *Builder) quadToPt(p1, p Point) { b.QuadTo(p1.X, p1.Y, p.X, p.Y) }

// CubicTo adds a cubic.
func (b *Builder) CubicTo(x1, y1, x2, y2, x, y float32) {
	b.injectMoveToIfNeeded()
	b.verbs = append(b.verbs, VerbCubic)
	b.points = append(b.points, Point{x1, y1}, Point{x2, y2}, Point{x, y})
}

func (b *Builder) cubicToPt(p1, p2, p Point) { b.CubicTo(p1.X, p1.Y, p2.X, p2.Y, p.X, p.Y) }

// conicTo approximates a conic with quads (PathBuilder::conic_to).
func (b *Builder) conicTo(x1, y1, x, y, weight float32) {
	switch {
	case !(weight > 0):
		b.LineTo(x, y)
	case !isFinite(weight):
		b.LineTo(x1, y1)
		b.LineTo(x, y)
	case weight == 1:
		b.QuadTo(x1, y1, x, y)
	default:
		b.injectMoveToIfNeeded()
		last, _ := b.LastPoint()
		pts, n, ok := autoConicToQuads(last, Point{x1, y1}, Point{x, y}, weight)
		if !ok {
			return
		}
		offset := 1
		for range n {
			b.QuadTo(pts[offset].X, pts[offset].Y, pts[offset+1].X, pts[offset+1].Y)
			offset += 2
		}
	}
}

func (b *Builder) conicPointsTo(p1, p2 Point, weight float32) {
	b.conicTo(p1.X, p1.Y, p2.X, p2.Y, weight)
}

// Close closes the contour (never twice, never first).
func (b *Builder) Close() {
	if n := len(b.verbs); n > 0 && b.verbs[n-1] != VerbClose {
		b.verbs = append(b.verbs, VerbClose)
	}
	b.moveToRequired = true
}

// LastPoint is the last point added.
func (b *Builder) LastPoint() (Point, bool) {
	if len(b.points) == 0 {
		return Point{}, false
	}
	return b.points[len(b.points)-1], true
}

func (b *Builder) setLastPoint(p Point) {
	if len(b.points) == 0 {
		b.MoveTo(p.X, p.Y)
		return
	}
	b.points[len(b.points)-1] = p
}

func (b *Builder) isZeroLengthSincePoint(start int) bool {
	count := len(b.points) - start
	if count < 2 {
		return true
	}
	first := b.points[start]
	for i := 1; i < count; i++ {
		if first != b.points[start+i] {
			return false
		}
	}
	return true
}

func (b *Builder) pushOval(oval Rect) {
	cx := oval.Left*0.5 + oval.Right*0.5
	cy := oval.Top*0.5 + oval.Bottom*0.5
	ovalPoints := [4]Point{{cx, oval.Bottom}, {oval.Left, cy}, {cx, oval.Top}, {oval.Right, cy}}
	rectPoints := [4]Point{{oval.Right, oval.Bottom}, {oval.Left, oval.Bottom}, {oval.Left, oval.Top}, {oval.Right, oval.Top}}
	b.MoveTo(ovalPoints[3].X, ovalPoints[3].Y)
	for i := range 4 {
		b.conicPointsTo(rectPoints[i], ovalPoints[i], scalarRoot2Over2)
	}
	b.Close()
}

func (b *Builder) pushCircle(x, y, r float32) {
	if rect, ok := RectFromXYWH(x-r, y-r, r+r, r+r); ok {
		b.pushOval(rect)
	}
}

func (b *Builder) pushBuilder(o *Builder) {
	if o.IsEmpty() {
		return
	}
	if b.lastMoveToIndex != 0 {
		b.lastMoveToIndex = len(b.points) + o.lastMoveToIndex
	}
	b.verbs = append(b.verbs, o.verbs...)
	b.points = append(b.points, o.points...)
}

func (b *Builder) reversePathTo(o *Builder) {
	if o.IsEmpty() {
		return
	}
	off := len(o.points) - 1
	for _, v := range slices.Backward(o.verbs) {
		switch v {
		case VerbMove:
			return
		case VerbLine:
			p := o.points[off-1]
			off--
			b.LineTo(p.X, p.Y)
		case VerbQuad:
			p1, p2 := o.points[off-1], o.points[off-2]
			off -= 2
			b.QuadTo(p1.X, p1.Y, p2.X, p2.Y)
		case VerbCubic:
			p1, p2, p3 := o.points[off-1], o.points[off-2], o.points[off-3]
			off -= 3
			b.CubicTo(p1.X, p1.Y, p2.X, p2.Y, p3.X, p3.Y)
		}
	}
}

func (b *Builder) clear() {
	b.verbs = b.verbs[:0]
	b.points = b.points[:0]
	b.lastMoveToIndex = 0
	b.moveToRequired = true
}

// Finish builds the path; false for fewer than two verbs or unbounded
// points.
func (b *Builder) Finish() (*Path, bool) {
	if len(b.verbs) <= 1 {
		return nil, false
	}
	bounds, ok := rectFromPoints(b.points)
	if !ok {
		return nil, false
	}
	return &Path{verbs: b.verbs, points: b.points, bounds: bounds}, true
}
