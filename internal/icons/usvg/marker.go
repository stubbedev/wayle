package usvg

import (
	"math"
	"slices"

	"github.com/stubbedev/wayle/internal/icons/skpath"
)

// This file ports usvg's parser/marker.rs: marker-start, marker-mid and
// marker-end instantiate the linked <marker>'s children at the path's
// vertices, oriented and scaled to the stroke. The clip a marker's
// overflow implies is left out: the icon transform reads no clips.

// markerSeg is marker.rs's Segment: a path segment with quads raised to
// cubics.
type markerSeg struct {
	kind skpath.SegmentKind // SegMove, SegLine, SegCubic or SegClose
	p1   skpath.Point
	p2   skpath.Point
	p    skpath.Point
}

type markerKind uint8

const (
	markerStart markerKind = iota
	markerMiddle
	markerEnd
)

var markerAttrs = [...]struct {
	name string
	kind markerKind
}{{"marker-start", markerStart}, {"marker-mid", markerMiddle}, {"marker-end", markerEnd}}

// markerLink is find_attribute::<SvgNode> for a marker-* property.
func (n *node) markerLink(name string) *node {
	if a := n.findAttributeNode(name); a != nil {
		return a.linkedNode(name)
	}
	return nil
}

// markersValid is marker::is_valid: some marker-* property links an
// element, and the shape is not inside a clipPath.
func markersValid(n *node) bool {
	for _, a := range n.ancestors() {
		if a.tag == "clipPath" {
			return false
		}
	}
	for _, m := range markerAttrs {
		if n.markerLink(m.name) != nil {
			return true
		}
	}
	return false
}

// convertMarkers is marker::convert: each linked <marker>, unless it is
// already being instantiated (a recursive marker), drawn into parent.
func convertMarkers(n *node, data *skpath.Path, st *state, parent *Group) {
	for _, m := range markerAttrs {
		link := n.markerLink(m.name)
		if link == nil || link.tag != "marker" || slices.Contains(st.parentMarkers, link) {
			continue
		}
		resolveMarker(n, data, link, m.kind, st, parent)
	}
}

func resolveMarker(shape *node, data *skpath.Path, marker *node, kind markerKind, st *state, parent *Group) {
	scale, ok := markerStrokeScale(shape, marker, st)
	if !ok {
		return
	}
	r, ok := markerRect(marker, st)
	if !ok {
		return
	}
	vb, hasVB := marker.parseViewBox()
	aspect := marker.aspectAttr()
	segs := markerSegments(data)
	orient := markerOrientation(marker)
	draw := func(p skpath.Point, idx int) {
		ts := skpath.FromTranslate(p.X, p.Y)
		var angle float32
		switch {
		case orient.autoStartReverse && idx == 0:
			angle = f32mod(vertexAngle(segs, idx)+180, 360)
		case orient.auto || orient.autoStartReverse:
			angle = vertexAngle(segs, idx)
		default:
			angle = orient.angle
		}
		if !approxZeroUlps(angle) {
			ts = ts.PreConcat(fromRotate(angle))
		}
		if hasVB {
			sx, sy := viewBoxTransform(vb, aspect, r.w*scale, r.h*scale).Scale()
			ts = ts.PreScale(sx, sy)
		} else {
			ts = ts.PreScale(scale, scale)
		}
		ts = ts.PreTranslate(-r.x, -r.y)
		g := &Group{Transform: ts}
		inner := *st
		inner.parentMarkers = append(slices.Clone(st.parentMarkers), marker)
		convertChildren(marker, &inner, g)
		if len(g.Children) > 0 {
			parent.Children = append(parent.Children, g)
		}
	}
	drawMarkers(segs, kind, draw)
}

// markerStrokeScale is stroke_scale: 1 in userSpaceOnUse units, else
// the shape's stroke width, which must be a valid length.
func markerStrokeScale(shape, marker *node, st *state) (float32, bool) {
	if v, ok := marker.attribute("markerUnits"); ok && v == "userSpaceOnUse" {
		return 1, true
	}
	w := shape.resolveLength("stroke-width", st, 1)
	return w, w > 0 && finite(w)
}

// markerRect is convert_rect: refX/refY and the marker size, which must
// be non-zero.
func markerRect(marker *node, st *state) (rect, bool) {
	r := rect{
		x: marker.userLength("refX", st, length{}),
		y: marker.userLength("refY", st, length{}),
		w: marker.userLength("markerWidth", st, length{3, unitNone}),
		h: marker.userLength("markerHeight", st, length{3, unitNone}),
	}
	ok := r.w > 0 && r.h > 0 && finite(r.x) && finite(r.y) && finite(r.x+r.w) && finite(r.y+r.h)
	return r, ok
}

func markerSegments(data *skpath.Path) []markerSeg {
	var segs []markerSeg
	var prev, prevMove skpath.Point
	it := data.Segments()
	for {
		s, ok := it.Next()
		if !ok {
			return segs
		}
		switch s.Kind {
		case skpath.SegMove:
			segs = append(segs, markerSeg{kind: skpath.SegMove, p: s.P[0]})
			prev, prevMove = s.P[0], s.P[0]
		case skpath.SegLine:
			segs = append(segs, markerSeg{kind: skpath.SegLine, p: s.P[0]})
			prev = s.P[0]
		case skpath.SegQuad:
			// quad_to_curve
			third := func(n1, n2 float32) float32 { return (n1 + n2*2) / 3 }
			p1, p := s.P[0], s.P[1]
			segs = append(segs, markerSeg{
				kind: skpath.SegCubic,
				p1:   skpath.Point{X: third(prev.X, p1.X), Y: third(prev.Y, p1.Y)},
				p2:   skpath.Point{X: third(p.X, p1.X), Y: third(p.Y, p1.Y)},
				p:    p,
			})
			prev = p
		case skpath.SegCubic:
			segs = append(segs, markerSeg{kind: skpath.SegCubic, p1: s.P[0], p2: s.P[1], p: s.P[2]})
			prev = s.P[2]
		case skpath.SegClose:
			segs = append(segs, markerSeg{kind: skpath.SegClose})
			prev = prevMove
		}
	}
}

// drawMarkers is draw_markers: the first move for a start marker, every
// inner vertex for a middle one, the last vertex (a close's subpath
// start) for an end one.
func drawMarkers(segs []markerSeg, kind markerKind, draw func(skpath.Point, int)) {
	switch kind {
	case markerStart:
		if len(segs) > 0 && segs[0].kind == skpath.SegMove {
			draw(segs[0].p, 0)
		}
	case markerMiddle:
		for i := 1; i < len(segs)-1; i++ {
			if segs[i].kind != skpath.SegClose {
				draw(segs[i].p, i)
			}
		}
	case markerEnd:
		idx := len(segs) - 1
		switch segs[idx].kind {
		case skpath.SegLine, skpath.SegCubic:
			draw(segs[idx].p, idx)
		case skpath.SegClose:
			draw(subpathStart(segs, idx), idx)
		}
	}
}

// vertexAngle is calc_vertex_angle: the bisector of the incoming and
// outgoing directions at segs[idx], in degrees.
func vertexAngle(segs []markerSeg, idx int) float32 {
	switch {
	case idx == 0:
		s1, s2 := segs[0], segs[1]
		if s1.kind != skpath.SegMove {
			return 0
		}
		pm := s1.p
		switch s2.kind {
		case skpath.SegLine:
			return lineAngle(pm, s2.p)
		case skpath.SegCubic:
			if approxEqUlps(pm.X, s2.p1.X) && approxEqUlps(pm.Y, s2.p1.Y) {
				return lineAngle(pm, s2.p)
			}
			return lineAngle(pm, s2.p1)
		}
		return 0
	case idx == len(segs)-1:
		s1, s2 := segs[idx-1], segs[idx]
		switch s2.kind {
		case skpath.SegLine:
			return lineAngle(prevVertex(segs, idx), s2.p)
		case skpath.SegCubic:
			if approxEqUlps(s2.p2.X, s2.p.X) && approxEqUlps(s2.p2.Y, s2.p.Y) {
				return lineAngle(s2.p1, s2.p)
			}
			return lineAngle(s2.p2, s2.p)
		case skpath.SegClose:
			switch s1.kind {
			case skpath.SegLine:
				return lineAngle(s1.p, subpathStart(segs, idx))
			case skpath.SegCubic:
				next := subpathStart(segs, idx)
				return curvesAngle(prevVertex(segs, idx), s1.p2, s1.p, next, next)
			}
		}
		return 0
	}
	s1, s2 := segs[idx], segs[idx+1]
	switch {
	case s1.kind == skpath.SegMove && s2.kind == skpath.SegLine:
		return lineAngle(s1.p, s2.p)
	case s1.kind == skpath.SegMove && s2.kind == skpath.SegCubic:
		return lineAngle(s1.p, s2.p1)
	case s1.kind == skpath.SegLine && s2.kind == skpath.SegLine:
		return angle4(prevVertex(segs, idx), s1.p, s1.p, s2.p)
	case s1.kind == skpath.SegCubic && s2.kind == skpath.SegCubic:
		return curvesAngle(prevVertex(segs, idx), s1.p2, s1.p, s2.p1, s2.p)
	case s1.kind == skpath.SegLine && s2.kind == skpath.SegCubic:
		prev := prevVertex(segs, idx)
		return curvesAngle(prev, prev, s1.p, s2.p1, s2.p)
	case s1.kind == skpath.SegCubic && s2.kind == skpath.SegLine:
		return curvesAngle(prevVertex(segs, idx), s1.p2, s1.p, s2.p, s2.p)
	case s1.kind == skpath.SegLine && s2.kind == skpath.SegMove:
		return lineAngle(prevVertex(segs, idx), s1.p)
	case s1.kind == skpath.SegCubic && s2.kind == skpath.SegMove:
		if approxEqUlps(s1.p.X, s1.p2.X) && approxEqUlps(s1.p.Y, s1.p2.Y) {
			return lineAngle(prevVertex(segs, idx), s1.p)
		}
		return lineAngle(s1.p2, s1.p)
	case s1.kind == skpath.SegLine && s2.kind == skpath.SegClose:
		return angle4(prevVertex(segs, idx), s1.p, s1.p, subpathStart(segs, idx))
	case s2.kind == skpath.SegClose:
		return lineAngle(prevVertex(segs, idx), subpathStart(segs, idx))
	}
	return 0
}

func lineAngle(a, b skpath.Point) float32 { return angle4(a, b, a, b) }

// curvesAngle is calc_curves_angle: prev vertex, prev control point,
// the vertex, next control point, next vertex.
func curvesAngle(prev, c1, p, c2, next skpath.Point) float32 {
	switch {
	case approxEqUlps(c1.X, p.X) && approxEqUlps(c1.Y, p.Y):
		return angle4(prev, p, p, c2)
	case approxEqUlps(p.X, c2.X) && approxEqUlps(p.Y, c2.Y):
		return angle4(c1, p, p, next)
	}
	return angle4(c1, p, p, c2)
}

// angle4 is calc_angle: the direction halfway between a1->a2 and
// b1->b2, in degrees within [0, 360), all in f32 as Rust computes it.
func angle4(a1, a2, b1, b2 skpath.Point) float32 {
	const twoPi = float32(math.Pi) * 2
	normalize := func(rad float32) float32 {
		v := f32mod(rad, twoPi)
		if v < 0 {
			return v + twoPi
		}
		return v
	}
	vectorAngle := func(vx, vy float32) float32 {
		rad := float32(atan2R(float64(vy), float64(vx)))
		if rad != rad {
			return 0
		}
		return normalize(rad)
	}
	in := vectorAngle(a2.X-a1.X, a2.Y-a1.Y)
	out := vectorAngle(b2.X-b1.X, b2.Y-b1.Y)
	d := (out - in) * 0.5
	angle := in + d
	if float32(math.Pi/2) < float32(math.Abs(float64(d))) {
		angle -= float32(math.Pi)
	}
	return normalize(angle) * f32DegreesPerRadian
}

// f32DegreesPerRadian is f32::to_degrees's constant.
const f32DegreesPerRadian float32 = 57.2957795130823208767981548141051703

// fromRotate is Transform::from_rotate: angle in degrees, through
// f32::to_radians and the f32 sin and cos.
func fromRotate(angle float32) skpath.Transform {
	pi := float32(math.Pi)
	v := angle * (pi / 180)
	s, c := sincos(float64(v))
	sin, cos := float32(s), float32(c)
	return skpath.FromRow(cos, sin, -sin, cos, 0, 0)
}

// f32mod is Rust's f32 %, fmod: exact, so computing it in f64 loses
// nothing.
func f32mod(a, b float32) float32 { return float32(math.Mod(float64(a), float64(b))) }

func subpathStart(segs []markerSeg, idx int) skpath.Point {
	for i := idx - 1; i >= 0; i-- {
		if segs[i].kind == skpath.SegMove {
			return segs[i].p
		}
	}
	return skpath.Point{}
}

func prevVertex(segs []markerSeg, idx int) skpath.Point {
	if segs[idx-1].kind == skpath.SegClose {
		return subpathStart(segs, idx)
	}
	return segs[idx-1].p
}

type orientation struct {
	auto, autoStartReverse bool
	angle                  float32
}

// markerOrientation is convert_orientation: auto, auto-start-reverse,
// else an angle (0 when it does not parse).
func markerOrientation(marker *node) orientation {
	v, _ := marker.attribute("orient")
	switch v {
	case "auto":
		return orientation{auto: true}
	case "auto-start-reverse":
		return orientation{autoStartReverse: true}
	}
	deg, ok := parseAngleStr(v)
	if !ok {
		return orientation{}
	}
	return orientation{angle: float32(deg)}
}

// parseAngleStr is Angle::from_str's to_degrees: a number with an
// optional deg, grad, rad or turn unit and nothing after.
func parseAngleStr(text string) (float64, bool) {
	s := stream{text: text}
	s.skipSpaces()
	n, err := s.parseNumber()
	if err != nil {
		return 0, false
	}
	deg := n
	switch {
	case s.startsWith("deg"):
		s.pos += 3
	case s.startsWith("grad"):
		s.pos += 4
		deg = n * 180 / 200
	case s.startsWith("rad"):
		s.pos += 3
		pi := math.Pi
		deg = n * (180 / pi)
	case s.startsWith("turn"):
		s.pos += 4
		deg = n * 360
	}
	return deg, s.atEnd()
}

// paintOrderKind is svgtypes::PaintOrderKind.
type paintOrderKind uint8

const (
	orderFill paintOrderKind = iota
	orderStroke
	orderMarkers
)

var defaultPaintOrder = [3]paintOrderKind{orderFill, orderStroke, orderMarkers}

// parsePaintOrder is PaintOrder::from_str: up to three of fill, stroke
// and markers, the missing ones appended in that order; anything else,
// a duplicate, or "normal" is the default.
func parsePaintOrder(text string) [3]paintOrderKind {
	var order []paintOrderKind
	left := defaultPaintOrder[:]
	s := stream{text: text}
	for !s.atEnd() && len(order) < 3 {
		s.skipSpaces()
		name := s.consumeASCIIIdent()
		s.skipSpaces()
		var k paintOrderKind
		switch name {
		case "fill":
			k = orderFill
		case "stroke":
			k = orderStroke
		case "markers":
			k = orderMarkers
		default: // "normal" included
			return defaultPaintOrder
		}
		if i := slices.Index(left, k); i >= 0 {
			left = slices.Delete(slices.Clone(left), i, i+1)
		}
		order = append(order, k)
	}
	s.skipSpaces()
	if !s.atEnd() || len(order) == 0 {
		return defaultPaintOrder
	}
	for len(order) < 3 && len(left) > 0 {
		order, left = append(order, left[0]), left[1:]
	}
	if order[0] == order[1] || order[0] == order[2] || order[1] == order[2] {
		return defaultPaintOrder
	}
	return [3]paintOrderKind{order[0], order[1], order[2]}
}

// approxEqUlps is float_cmp's f32 approx_eq_ulps(other, 4): equal, or
// the same sign and at most 4 representable values apart.
func approxEqUlps(a, b float32) bool {
	if a == b {
		return true
	}
	if math.Signbit(float64(a)) != math.Signbit(float64(b)) {
		return false
	}
	diff := int32(math.Float32bits(a)) - int32(math.Float32bits(b))
	return diff >= -4 && diff <= 4
}
