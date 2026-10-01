package skpath

import "math"

// This file ports dash.rs (SkDashPath, SkContourMeasure).

// StrokeDash is a validated dash pattern.
type StrokeDash struct {
	array       []float32
	offset      float32
	intervalLen float32
	firstLen    float32
	firstIndex  int
}

// NewStrokeDash is StrokeDash::new: an even-length, non-negative array
// with a positive sum and a finite offset.
func NewStrokeDash(array []float32, offset float32) (*StrokeDash, bool) {
	if !isFinite(offset) {
		return nil, false
	}
	if len(array) < 2 || len(array)%2 != 0 {
		return nil, false
	}
	var sum float32
	for _, n := range array {
		if n < 0 {
			return nil, false
		}
		sum += n
	}
	if !(sum > 0 && isFinite(sum)) {
		return nil, false
	}
	offset = adjustDashOffset(offset, sum)
	firstLen, firstIndex := findFirstInterval(array, offset)
	return &StrokeDash{array: array, offset: offset, intervalLen: sum, firstLen: firstLen, firstIndex: firstIndex}, true
}

func fmodf(a, b float32) float32 { return float32(math.Mod(float64(a), float64(b))) }

func adjustDashOffset(offset, length float32) float32 {
	switch {
	case offset < 0:
		offset = -offset
		if offset > length {
			offset = fmodf(offset, length)
		}
		offset = length - offset
		if offset == length {
			offset = 0
		}
		return offset
	case offset >= length:
		return fmodf(offset, length)
	default:
		return offset
	}
}

func findFirstInterval(array []float32, offset float32) (float32, int) {
	for i, gap := range array {
		if offset > gap || (offset == gap && gap != 0) {
			offset -= gap
		} else {
			return gap - offset, i
		}
	}
	return array[0], 0
}

// DashPath is Path::dash.
func (p *Path) DashPath(d *StrokeDash, resScale float32) (*Path, bool) {
	pb := NewBuilder()
	var dashCount float32
	it := contourMeasureIter{iter: p.Segments(), tolerance: 0.5 * invert(resScale)}
	for {
		contour, ok := it.next()
		if !ok {
			break
		}
		skipFirst := contour.isClosed
		added := false
		length := contour.length
		index := d.firstIndex
		const maxDashCount = 1000000
		dashCount += length * float32(len(d.array)>>1) / d.intervalLen
		if dashCount > maxDashCount {
			return nil, false
		}
		var distance float32
		dLen := d.firstLen
		for distance < length {
			added = false
			if index%2 == 0 && !skipFirst {
				added = true
				contour.pushSegment(distance, distance+dLen, true, pb)
			}
			distance += dLen
			skipFirst = false
			index++
			if index == len(d.array) {
				index = 0
			}
			dLen = d.array[index]
		}
		if contour.isClosed && d.firstIndex%2 == 0 && d.firstLen >= 0 {
			contour.pushSegment(0, d.firstLen, !added, pb)
		}
	}
	return pb.Finish()
}

const maxTValue uint32 = 0x3FFFFFFF

type contourMeasureIter struct {
	iter      *segmentsIter
	tolerance float32
}

type segKind uint8

const (
	measureLine segKind = iota
	measureQuad
	measureCubic
)

type measureSegment struct {
	distance   float32
	pointIndex int
	tValue     uint32
	kind       segKind
}

// maxTReciprocal is 1/kMaxTValue with the divisor rounded to f32 first,
// as the Rust constant is.
var maxTReciprocal = 1 / float32(maxTValue)

func (s measureSegment) scalarT() float32 { return float32(s.tValue) * maxTReciprocal }

type contourMeasure struct {
	segments []measureSegment
	points   []Point
	length   float32
	isClosed bool
}

// next is ContourMeasureIter::next; a non-finite length or an empty
// contour ends the iteration, as in Rust.
func (it *contourMeasureIter) next() (*contourMeasure, bool) {
	c := &contourMeasure{}
	pointIndex := 0
	var distance float32
	haveSeenClose := false
	var prev Point
	for {
		seg, ok := it.iter.next()
		if !ok {
			break
		}
		switch seg.Kind {
		case SegMove:
			c.points = append(c.points, seg.P[0])
			prev = seg.P[0]
		case SegLine:
			prevD := distance
			distance = c.computeLineSeg(prev, seg.P[0], distance, pointIndex)
			if distance > prevD {
				c.points = append(c.points, seg.P[0])
				pointIndex++
			}
			prev = seg.P[0]
		case SegQuad:
			prevD := distance
			distance = c.computeQuadSegs(prev, seg.P[0], seg.P[1], distance, 0, maxTValue, pointIndex, it.tolerance)
			if distance > prevD {
				c.points = append(c.points, seg.P[0], seg.P[1])
				pointIndex += 2
			}
			prev = seg.P[1]
		case SegCubic:
			prevD := distance
			distance = c.computeCubicSegs(prev, seg.P[0], seg.P[1], seg.P[2], distance, 0, maxTValue, pointIndex, it.tolerance)
			if distance > prevD {
				c.points = append(c.points, seg.P[0], seg.P[1], seg.P[2])
				pointIndex += 3
			}
			prev = seg.P[2]
		case SegClose:
			haveSeenClose = true
		}
		if v, ok := it.iter.nextVerb(); ok && v == VerbMove {
			break
		}
	}
	if !isFinite(distance) {
		return nil, false
	}
	if haveSeenClose {
		prevD := distance
		first := c.points[0]
		distance = c.computeLineSeg(c.points[pointIndex], first, distance, pointIndex)
		if distance > prevD {
			c.points = append(c.points, first)
		}
	}
	c.length = distance
	c.isClosed = haveSeenClose
	if len(c.points) == 0 {
		return nil, false
	}
	return c, true
}

func (c *contourMeasure) computeLineSeg(p0, p1 Point, distance float32, pointIndex int) float32 {
	d := p0.distance(p1)
	prevD := distance
	distance += d
	if distance > prevD {
		c.segments = append(c.segments, measureSegment{distance, pointIndex, maxTValue, measureLine})
	}
	return distance
}

func (c *contourMeasure) computeQuadSegs(p0, p1, p2 Point, distance float32, minT, maxT uint32, pointIndex int, tol float32) float32 {
	if (maxT-minT)>>10 != 0 && quadTooCurvy(p0, p1, p2, tol) {
		var tmp [5]Point
		halfT := (minT + maxT) >> 1
		chopQuadAt([]Point{p0, p1, p2}, 0.5, &tmp)
		distance = c.computeQuadSegs(tmp[0], tmp[1], tmp[2], distance, minT, halfT, pointIndex, tol)
		return c.computeQuadSegs(tmp[2], tmp[3], tmp[4], distance, halfT, maxT, pointIndex, tol)
	}
	d := p0.distance(p2)
	prevD := distance
	distance += d
	if distance > prevD {
		c.segments = append(c.segments, measureSegment{distance, pointIndex, maxT, measureQuad})
	}
	return distance
}

func (c *contourMeasure) computeCubicSegs(p0, p1, p2, p3 Point, distance float32, minT, maxT uint32, pointIndex int, tol float32) float32 {
	if (maxT-minT)>>10 != 0 && cubicTooCurvy(p0, p1, p2, p3, tol) {
		var tmp [7]Point
		halfT := (minT + maxT) >> 1
		chopCubicAt2(&[4]Point{p0, p1, p2, p3}, 0.5, tmp[:])
		distance = c.computeCubicSegs(tmp[0], tmp[1], tmp[2], tmp[3], distance, minT, halfT, pointIndex, tol)
		return c.computeCubicSegs(tmp[3], tmp[4], tmp[5], tmp[6], distance, halfT, maxT, pointIndex, tol)
	}
	d := p0.distance(p3)
	prevD := distance
	distance += d
	if distance > prevD {
		c.segments = append(c.segments, measureSegment{distance, pointIndex, maxT, measureCubic})
	}
	return distance
}

func (c *contourMeasure) pushSegment(startD, stopD float32, startWithMoveTo bool, pb *Builder) {
	if startD < 0 {
		startD = 0
	}
	if stopD > c.length {
		stopD = c.length
	}
	if !(startD <= stopD) || len(c.segments) == 0 {
		return
	}
	segIndex, startT, ok := c.distanceToSegment(startD)
	if !ok {
		return
	}
	seg := c.segments[segIndex]
	stopIndex, stopT, ok := c.distanceToSegment(stopD)
	if !ok {
		return
	}
	stopSeg := c.segments[stopIndex]
	if startWithMoveTo {
		p := computePos(c.points[seg.pointIndex:], seg.kind, startT)
		pb.MoveTo(p.X, p.Y)
	}
	if seg.pointIndex == stopSeg.pointIndex {
		segmentTo(c.points[seg.pointIndex:], seg.kind, startT, stopT, pb)
		return
	}
	newIndex := segIndex
	for {
		segmentTo(c.points[seg.pointIndex:], seg.kind, startT, 1, pb)
		old := seg.pointIndex
		for {
			newIndex++
			if c.segments[newIndex].pointIndex != old {
				break
			}
		}
		seg = c.segments[newIndex]
		startT = 0
		if seg.pointIndex >= stopSeg.pointIndex {
			break
		}
	}
	segmentTo(c.points[seg.pointIndex:], seg.kind, 0, stopT, pb)
}

func (c *contourMeasure) distanceToSegment(distance float32) (int, float32, bool) {
	index := findSegment(c.segments, distance)
	index ^= index >> 31
	seg := c.segments[index]
	var startT, startD float32
	if index > 0 {
		startD = c.segments[index-1].distance
		if c.segments[index-1].pointIndex == seg.pointIndex {
			startT = c.segments[index-1].scalarT()
		}
	}
	t := startT + (seg.scalarT()-startT)*(distance-startD)/(seg.distance-startD)
	if !(t >= 0 && t <= 1) {
		return 0, 0, false
	}
	return int(index), t, true
}

func findSegment(base []measureSegment, key float32) int32 {
	lo := uint32(0)
	hi := uint32(len(base) - 1)
	for lo < hi {
		mid := (hi + lo) >> 1
		if base[mid].distance < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if base[hi].distance < key {
		hi++
		hi = ^hi
	} else if key < base[hi].distance {
		hi = ^hi
	}
	return int32(hi)
}

func interpf(a, b, t float32) float32 { return a + (b-a)*t }

func computePos(points []Point, kind segKind, t float32) Point {
	switch kind {
	case measureLine:
		return Point{interpf(points[0].X, points[1].X, t), interpf(points[0].Y, points[1].Y, t)}
	case measureQuad:
		return evalQuadAt((*[3]Point)(points[:3]), t)
	default:
		return evalCubicPosAt((*[4]Point)(points[:4]), t)
	}
}

func segmentTo(points []Point, kind segKind, startT, stopT float32, pb *Builder) {
	if startT == stopT {
		if pt, ok := pb.LastPoint(); ok {
			pb.LineTo(pt.X, pt.Y)
		}
		return
	}
	switch kind {
	case measureLine:
		if stopT == 1 {
			pb.LineTo(points[1].X, points[1].Y)
		} else {
			pb.LineTo(interpf(points[0].X, points[1].X, stopT), interpf(points[0].Y, points[1].Y, stopT))
		}
	case measureQuad:
		var tmp0, tmp1 [5]Point
		if startT == 0 {
			if stopT == 1 {
				pb.quadToPt(points[1], points[2])
			} else {
				chopQuadAt(points, boundedExclusive(stopT), &tmp0)
				pb.quadToPt(tmp0[1], tmp0[2])
			}
			return
		}
		chopQuadAt(points, boundedExclusive(startT), &tmp0)
		if stopT == 1 {
			pb.quadToPt(tmp0[3], tmp0[4])
		} else {
			newT := boundedExclusive((stopT - startT) / (1 - startT))
			chopQuadAt(tmp0[2:], newT, &tmp1)
			pb.quadToPt(tmp1[1], tmp1[2])
		}
	case measureCubic:
		var tmp0, tmp1 [7]Point
		src := (*[4]Point)(points[:4])
		if startT == 0 {
			if stopT == 1 {
				pb.cubicToPt(points[1], points[2], points[3])
			} else {
				chopCubicAt2(src, boundedExclusive(stopT), tmp0[:])
				pb.cubicToPt(tmp0[1], tmp0[2], tmp0[3])
			}
			return
		}
		chopCubicAt2(src, boundedExclusive(startT), tmp0[:])
		if stopT == 1 {
			pb.cubicToPt(tmp0[4], tmp0[5], tmp0[6])
		} else {
			newT := boundedExclusive((stopT - startT) / (1 - startT))
			chopCubicAt2((*[4]Point)(tmp0[3:7]), newT, tmp1[:])
			pb.cubicToPt(tmp1[1], tmp1[2], tmp1[3])
		}
	}
}

func quadTooCurvy(p0, p1, p2 Point, tol float32) bool {
	dx := p1.X*0.5 - (p0.X+p2.X)*0.5*0.5
	dy := p1.Y*0.5 - (p0.Y+p2.Y)*0.5*0.5
	return maxf(absf(dx), absf(dy)) > tol
}

func cubicTooCurvy(p0, p1, p2, p3 Point, tol float32) bool {
	return cheapDistExceedsLimit(p1, interpf(p0.X, p3.X, 1.0/3.0), interpf(p0.Y, p3.Y, 1.0/3.0), tol) ||
		cheapDistExceedsLimit(p2, interpf(p0.X, p3.X, 2.0/3.0), interpf(p0.Y, p3.Y, 2.0/3.0), tol)
}

func cheapDistExceedsLimit(pt Point, x, y, tol float32) bool {
	return maxf(absf(x-pt.X), absf(y-pt.Y)) > tol
}
