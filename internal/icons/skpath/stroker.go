package skpath

// This file ports stroker.rs (SkStroke.cpp): outlining a path's stroke
// into a fillable path.

// LineCap is a stroke's end cap.
type LineCap uint8

// The caps.
const (
	CapButt LineCap = iota
	CapRound
	CapSquare
)

// LineJoin is a stroke's corner join.
type LineJoin uint8

// The joins.
const (
	JoinMiter LineJoin = iota
	JoinMiterClip
	JoinRound
	JoinBevel
)

// Stroke describes a stroke to outline.
type Stroke struct {
	Width      float32
	MiterLimit float32
	LineCap    LineCap
	LineJoin   LineJoin
	Dash       *StrokeDash
}

type reductionType uint8

const (
	reducePoint reductionType = iota
	reduceLine
	reduceQuad
	reduceDegenerate
	reduceDegenerate2
	reduceDegenerate3
)

type strokeType int8

const (
	strokeOuter strokeType = 1
	strokeInner strokeType = -1
)

type resultType uint8

const (
	resultSplit resultType = iota
	resultDegenerate
	resultQuad
)

type intersectRayType uint8

const (
	rayCtrlPt intersectRayType = iota
	rayResultType
)

const quadRecursiveLimit = 3

var recursiveLimits = [4]int{5 * 3, 26 * 3, 11 * 3, 11 * 3}

type quadConstruct struct {
	quad             [3]Point
	tangentStart     Point
	tangentEnd       Point
	startT, midT     float32
	endT             float32
	startSet, endSet bool
	oppositeTangents bool
}

func (q *quadConstruct) init(start, end float32) bool {
	q.startT = start
	q.midT = clamp01((start + end) * 0.5)
	q.endT = end
	q.startSet = false
	q.endSet = false
	return q.startT < q.midT && q.midT < q.endT
}

func (q *quadConstruct) initWithStart(parent *quadConstruct) bool {
	if !q.init(parent.startT, parent.midT) {
		return false
	}
	q.quad[0] = parent.quad[0]
	q.tangentStart = parent.tangentStart
	q.startSet = true
	return true
}

func (q *quadConstruct) initWithEnd(parent *quadConstruct) bool {
	if !q.init(parent.midT, parent.endT) {
		return false
	}
	q.quad[2] = parent.quad[2]
	q.tangentEnd = parent.tangentEnd
	q.endSet = true
	return true
}

type stroker struct {
	radius             float32
	invMiterLimit      float32
	resScale           float32
	invResScale        float32
	invResScaleSquared float32

	firstNormal     Point
	prevNormal      Point
	firstUnitNormal Point
	prevUnitNormal  Point

	firstPt Point
	prevPt  Point

	firstOuterPt               Point
	firstOuterPtIndexInContour int
	segmentCount               int
	prevIsLine                 bool

	capper LineCap
	joiner LineJoin

	inner  *Builder
	outer  *Builder
	cusper *Builder

	strokeType strokeType

	recursionDepth int
	foundTangents  bool
	joinCompleted  bool
}

// StrokePath is Path::stroke: the fill outline of the stroke.
func (p *Path) StrokePath(s Stroke, resScale float32) (*Path, bool) {
	if !(s.Width > 0 && isFinite(s.Width)) {
		return nil, false
	}
	st := &stroker{inner: NewBuilder(), outer: NewBuilder(), cusper: NewBuilder()}
	return st.stroke(p, s.Width, s.MiterLimit, s.LineCap, s.LineJoin, resScale)
}

func (s *stroker) stroke(path *Path, width, miterLimit float32, lineCap LineCap, lineJoin LineJoin, resScale float32) (*Path, bool) {
	var invMiterLimit float32
	if lineJoin == JoinMiter {
		if miterLimit <= 1 {
			lineJoin = JoinBevel
		} else {
			invMiterLimit = invert(miterLimit)
		}
	}
	if lineJoin == JoinMiterClip {
		invMiterLimit = invert(miterLimit)
	}
	s.resScale = resScale
	s.invResScale = invert(resScale * 4)
	s.invResScaleSquared = s.invResScale * s.invResScale
	s.radius = width * 0.5
	s.invMiterLimit = invMiterLimit
	s.segmentCount = -1
	s.capper = lineCap
	s.joiner = lineJoin
	s.strokeType = strokeOuter

	lastSegmentIsLine := false
	it := path.Segments()
	it.autoClose = true
	for {
		seg, ok := it.next()
		if !ok {
			break
		}
		switch seg.Kind {
		case SegMove:
			s.moveTo(seg.P[0])
		case SegLine:
			s.lineTo(seg.P[0], it)
			lastSegmentIsLine = true
		case SegQuad:
			s.quadTo(seg.P[0], seg.P[1])
			lastSegmentIsLine = false
		case SegCubic:
			s.cubicTo(seg.P[0], seg.P[1], seg.P[2])
			lastSegmentIsLine = false
		case SegClose:
			if lineCap != CapButt {
				if s.segmentCount == 0 {
					s.lineTo(s.firstPt, nil)
					lastSegmentIsLine = true
					continue
				}
				if s.isCurrentContourEmpty() {
					lastSegmentIsLine = true
					continue
				}
			}
			s.finishContour(true, lastSegmentIsLine)
		}
	}
	return s.finish(lastSegmentIsLine)
}

func (s *stroker) moveTo(p Point) {
	if s.segmentCount > 0 {
		s.finishContour(false, false)
	}
	s.segmentCount = 0
	s.firstPt = p
	s.prevPt = p
	s.joinCompleted = false
}

func (s *stroker) lineTo(p Point, it *segmentsIter) {
	teeny := s.prevPt.equalsWithinTolerance(p, scalarNearlyZero*s.invResScale)
	if s.capper == CapButt && teeny {
		return
	}
	if teeny && (s.joinCompleted || (it != nil && it.hasValidTangent())) {
		return
	}
	var normal, unitNormal Point
	if !s.preJoinTo(p, true, &normal, &unitNormal) {
		return
	}
	s.outer.LineTo(p.X+normal.X, p.Y+normal.Y)
	s.inner.LineTo(p.X-normal.X, p.Y-normal.Y)
	s.postJoinTo(p, normal, unitNormal)
}

func (s *stroker) quadTo(p1, p2 Point) {
	quad := [3]Point{s.prevPt, p1, p2}
	reduction, rt := checkQuadLinear(&quad)
	switch rt {
	case reducePoint, reduceLine:
		s.lineTo(p2, nil)
		return
	case reduceDegenerate:
		s.lineTo(reduction, nil)
		save := s.joiner
		s.joiner = JoinRound
		s.lineTo(p2, nil)
		s.joiner = save
		return
	}
	var normalAB, unitAB, normalBC, unitBC Point
	if !s.preJoinTo(p1, false, &normalAB, &unitAB) {
		s.lineTo(p2, nil)
		return
	}
	var qp quadConstruct
	s.initQuad(strokeOuter, 0, 1, &qp)
	s.quadStroke(&quad, &qp)
	s.initQuad(strokeInner, 0, 1, &qp)
	s.quadStroke(&quad, &qp)
	if !setNormalUnitNormal(quad[1], quad[2], s.resScale, s.radius, &normalBC, &unitBC) {
		normalBC = normalAB
		unitBC = unitAB
	}
	s.postJoinTo(p2, normalBC, unitBC)
}

func (s *stroker) cubicTo(pt1, pt2, pt3 Point) {
	cubic := [4]Point{s.prevPt, pt1, pt2, pt3}
	var reduction [3]Point
	var tangentPt Point
	rt := checkCubicLinear(&cubic, &reduction, &tangentPt)
	switch {
	case rt == reducePoint || rt == reduceLine:
		s.lineTo(pt3, nil)
		return
	case rt >= reduceDegenerate && rt <= reduceDegenerate3:
		s.lineTo(reduction[0], nil)
		save := s.joiner
		s.joiner = JoinRound
		if rt >= reduceDegenerate2 {
			s.lineTo(reduction[1], nil)
		}
		if rt == reduceDegenerate3 {
			s.lineTo(reduction[2], nil)
		}
		s.lineTo(pt3, nil)
		s.joiner = save
		return
	}
	var normalAB, unitAB, normalCD, unitCD Point
	if !s.preJoinTo(tangentPt, false, &normalAB, &unitAB) {
		s.lineTo(pt3, nil)
		return
	}
	var tv [3]float32
	n := findCubicInflections(&cubic, &tv)
	var lastT float32
	for i := 0; i <= n; i++ {
		nextT := float32(1)
		if i < n {
			nextT = tv[i]
		}
		var qp quadConstruct
		s.initQuad(strokeOuter, lastT, nextT, &qp)
		s.cubicStroke(&cubic, &qp)
		s.initQuad(strokeInner, lastT, nextT, &qp)
		s.cubicStroke(&cubic, &qp)
		lastT = nextT
	}
	if cusp, ok := findCubicCusp(&cubic); ok {
		loc := evalCubicPosAt(&cubic, cusp)
		s.cusper.pushCircle(loc.X, loc.Y, s.radius)
	}
	s.setCubicEndNormal(&cubic, normalAB, unitAB, &normalCD, &unitCD)
	s.postJoinTo(pt3, normalCD, unitCD)
}

func (s *stroker) cubicStroke(cubic *[4]Point, qp *quadConstruct) bool {
	if !s.foundTangents {
		rt := s.tangentsMeet(cubic, qp)
		if rt != resultQuad {
			ok := pointsWithinDist(qp.quad[0], qp.quad[2], s.invResScale)
			if (rt == resultDegenerate || ok) && s.cubicMidOnLine(cubic, qp) {
				s.addDegenerateLine(qp)
				return true
			}
		} else {
			s.foundTangents = true
		}
	}
	if s.foundTangents {
		rt := s.compareQuadCubic(cubic, qp)
		if rt == resultQuad {
			st := qp.quad
			b := s.inner
			if s.strokeType == strokeOuter {
				b = s.outer
			}
			b.QuadTo(st[1].X, st[1].Y, st[2].X, st[2].Y)
			return true
		}
		if rt == resultDegenerate && !qp.oppositeTangents {
			s.addDegenerateLine(qp)
			return true
		}
	}
	if !isFinite(qp.quad[2].X) {
		return false
	}
	s.recursionDepth++
	limitIndex := 0
	if s.foundTangents {
		limitIndex = 1
	}
	if s.recursionDepth > recursiveLimits[limitIndex] {
		return false
	}
	var half quadConstruct
	if !half.initWithStart(qp) {
		s.addDegenerateLine(qp)
		s.recursionDepth--
		return true
	}
	if !s.cubicStroke(cubic, &half) {
		return false
	}
	if !half.initWithEnd(qp) {
		s.addDegenerateLine(qp)
		s.recursionDepth--
		return true
	}
	if !s.cubicStroke(cubic, &half) {
		return false
	}
	s.recursionDepth--
	return true
}

func (s *stroker) cubicMidOnLine(cubic *[4]Point, qp *quadConstruct) bool {
	var mid Point
	s.cubicQuadMid(cubic, qp, &mid)
	return ptToLine(mid, qp.quad[0], qp.quad[2]) < s.invResScaleSquared
}

func (s *stroker) cubicQuadMid(cubic *[4]Point, qp *quadConstruct, mid *Point) {
	var cubicMid Point
	s.cubicPerpRay(cubic, qp.midT, &cubicMid, mid, nil)
}

func (s *stroker) cubicPerpRay(cubic *[4]Point, t float32, tPt, onPt, tangent *Point) {
	*tPt = evalCubicPosAt(cubic, t)
	dxy := evalCubicTangentAt(cubic, t)
	if dxy.X == 0 && dxy.Y == 0 {
		cPoints := cubic[:]
		var chopped [7]Point
		switch {
		case nearlyZero(t):
			dxy = cubic[2].sub(cubic[0])
		case nearlyZero(1 - t):
			dxy = cubic[3].sub(cubic[1])
		default:
			chopCubicAt2(cubic, t, chopped[:])
			dxy = chopped[3].sub(chopped[2])
			if dxy.X == 0 && dxy.Y == 0 {
				dxy = chopped[3].sub(chopped[1])
				cPoints = chopped[:]
			}
		}
		if dxy.X == 0 && dxy.Y == 0 {
			dxy = cPoints[3].sub(cPoints[0])
		}
	}
	s.setRayPoints(*tPt, &dxy, onPt, tangent)
}

func (s *stroker) setCubicEndNormal(cubic *[4]Point, normalAB, unitAB Point, normalCD, unitCD *Point) {
	ab := cubic[1].sub(cubic[0])
	cd := cubic[3].sub(cubic[2])
	degAB := !ab.canNormalize()
	degCB := !cd.canNormalize()
	if degAB && degCB {
		*normalCD = normalAB
		*unitCD = unitAB
		return
	}
	if degAB {
		ab = cubic[2].sub(cubic[0])
		degAB = !ab.canNormalize()
	}
	if degCB {
		cd = cubic[3].sub(cubic[1])
		degCB = !cd.canNormalize()
	}
	if degAB || degCB {
		*normalCD = normalAB
		*unitCD = unitAB
		return
	}
	setNormalUnitNormal2(cd, s.radius, normalCD, unitCD)
}

func (s *stroker) compareQuadCubic(cubic *[4]Point, qp *quadConstruct) resultType {
	s.cubicQuadEnds(cubic, qp)
	if rt := s.intersectRay(rayCtrlPt, qp); rt != resultQuad {
		return rt
	}
	var ray0, ray1 Point
	s.cubicPerpRay(cubic, qp.midT, &ray1, &ray0, nil)
	stroke := qp.quad
	return s.strokeCloseEnough(&stroke, [2]Point{ray0, ray1}, qp)
}

func (s *stroker) cubicQuadEnds(cubic *[4]Point, qp *quadConstruct) {
	if !qp.startSet {
		var p Point
		s.cubicPerpRay(cubic, qp.startT, &p, &qp.quad[0], &qp.tangentStart)
		qp.startSet = true
	}
	if !qp.endSet {
		var p Point
		s.cubicPerpRay(cubic, qp.endT, &p, &qp.quad[2], &qp.tangentEnd)
		qp.endSet = true
	}
}

func (s *stroker) join(beforeUnit, pivot, afterUnit Point, currIsLine bool) {
	b := swappable{inner: s.inner, outer: s.outer}
	switch s.joiner {
	case JoinMiter:
		miterJoinerInner(beforeUnit, pivot, afterUnit, s.radius, s.invMiterLimit, false, s.prevIsLine, currIsLine, b)
	case JoinMiterClip:
		miterJoinerInner(beforeUnit, pivot, afterUnit, s.radius, s.invMiterLimit, true, s.prevIsLine, currIsLine, b)
	case JoinRound:
		roundJoiner(beforeUnit, pivot, afterUnit, s.radius, b)
	case JoinBevel:
		bevelJoiner(beforeUnit, pivot, afterUnit, s.radius, b)
	}
}

func (s *stroker) capTo(pivot, normal, stop Point, other bool) {
	switch s.capper {
	case CapButt:
		s.outer.LineTo(stop.X, stop.Y)
	case CapRound:
		roundCapper(pivot, normal, stop, s.outer)
	case CapSquare:
		squareCapper(pivot, normal, stop, other, s.outer)
	}
}

func (s *stroker) finishContour(close, currIsLine bool) {
	if s.segmentCount > 0 {
		if close {
			s.join(s.prevUnitNormal, s.prevPt, s.firstUnitNormal, currIsLine)
			s.outer.Close()
			pt, _ := s.inner.LastPoint()
			s.outer.MoveTo(pt.X, pt.Y)
			s.outer.reversePathTo(s.inner)
			s.outer.Close()
		} else {
			pt, _ := s.inner.LastPoint()
			s.capTo(s.prevPt, s.prevNormal, pt, currIsLine)
			s.outer.reversePathTo(s.inner)
			s.capTo(s.firstPt, s.firstNormal.neg(), s.firstOuterPt, s.prevIsLine)
			s.outer.Close()
		}
		if !s.cusper.IsEmpty() {
			s.outer.pushBuilder(s.cusper)
			s.cusper.clear()
		}
	}
	s.inner.clear()
	s.segmentCount = -1
	s.firstOuterPtIndexInContour = len(s.outer.points)
}

func (s *stroker) preJoinTo(p Point, currIsLine bool, normal, unitNormal *Point) bool {
	prevX, prevY := s.prevPt.X, s.prevPt.Y
	if !setNormalUnitNormal(s.prevPt, p, s.resScale, s.radius, normal, unitNormal) {
		if s.capper == CapButt {
			return false
		}
		*normal = Point{s.radius, 0}
		*unitNormal = Point{1, 0}
	}
	if s.segmentCount == 0 {
		s.firstNormal = *normal
		s.firstUnitNormal = *unitNormal
		s.firstOuterPt = Point{prevX + normal.X, prevY + normal.Y}
		s.outer.MoveTo(s.firstOuterPt.X, s.firstOuterPt.Y)
		s.inner.MoveTo(prevX-normal.X, prevY-normal.Y)
	} else {
		s.join(s.prevUnitNormal, s.prevPt, *unitNormal, currIsLine)
	}
	s.prevIsLine = currIsLine
	return true
}

func (s *stroker) postJoinTo(p, normal, unitNormal Point) {
	s.joinCompleted = true
	s.prevPt = p
	s.prevUnitNormal = unitNormal
	s.prevNormal = normal
	s.segmentCount++
}

func (s *stroker) initQuad(st strokeType, start, end float32, qp *quadConstruct) {
	s.strokeType = st
	s.foundTangents = false
	qp.init(start, end)
}

func (s *stroker) quadStroke(quad *[3]Point, qp *quadConstruct) bool {
	rt := s.compareQuadQuad(quad, qp)
	if rt == resultQuad {
		b := s.inner
		if s.strokeType == strokeOuter {
			b = s.outer
		}
		b.QuadTo(qp.quad[1].X, qp.quad[1].Y, qp.quad[2].X, qp.quad[2].Y)
		return true
	}
	if rt == resultDegenerate {
		s.addDegenerateLine(qp)
		return true
	}
	s.recursionDepth++
	if s.recursionDepth > recursiveLimits[quadRecursiveLimit] {
		return false
	}
	var half quadConstruct
	half.initWithStart(qp)
	if !s.quadStroke(quad, &half) {
		return false
	}
	half.initWithEnd(qp)
	if !s.quadStroke(quad, &half) {
		return false
	}
	s.recursionDepth--
	return true
}

func (s *stroker) compareQuadQuad(quad *[3]Point, qp *quadConstruct) resultType {
	if !qp.startSet {
		var p Point
		s.quadPerpRay(quad, qp.startT, &p, &qp.quad[0], &qp.tangentStart)
		qp.startSet = true
	}
	if !qp.endSet {
		var p Point
		s.quadPerpRay(quad, qp.endT, &p, &qp.quad[2], &qp.tangentEnd)
		qp.endSet = true
	}
	if rt := s.intersectRay(rayCtrlPt, qp); rt != resultQuad {
		return rt
	}
	var ray0, ray1 Point
	s.quadPerpRay(quad, qp.midT, &ray1, &ray0, nil)
	stroke := qp.quad
	return s.strokeCloseEnough(&stroke, [2]Point{ray0, ray1}, qp)
}

func (s *stroker) setRayPoints(tp Point, dxy, onP, tangent *Point) {
	if !dxy.setLength(s.radius) {
		*dxy = Point{s.radius, 0}
	}
	flip := float32(s.strokeType)
	onP.X = tp.X + flip*dxy.Y
	onP.Y = tp.Y - flip*dxy.X
	if tangent != nil {
		tangent.X = onP.X + dxy.X
		tangent.Y = onP.Y + dxy.Y
	}
}

func (s *stroker) quadPerpRay(quad *[3]Point, t float32, tp, onP, tangent *Point) {
	*tp = evalQuadAt(quad, t)
	dxy := evalQuadTangentAt(quad, t)
	if dxy.isZero() {
		dxy = quad[2].sub(quad[0])
	}
	s.setRayPoints(*tp, &dxy, onP, tangent)
}

func (s *stroker) addDegenerateLine(qp *quadConstruct) {
	b := s.inner
	if s.strokeType == strokeOuter {
		b = s.outer
	}
	b.LineTo(qp.quad[2].X, qp.quad[2].Y)
}

func (s *stroker) strokeCloseEnough(stroke *[3]Point, ray [2]Point, qp *quadConstruct) resultType {
	strokeMid := evalQuadAt(stroke, 0.5)
	if pointsWithinDist(ray[0], strokeMid, s.invResScale) {
		if sharpAngle(&qp.quad) {
			return resultSplit
		}
		return resultQuad
	}
	if !ptInQuadBounds(stroke, ray[0], s.invResScale) {
		return resultSplit
	}
	var roots [3]float32
	n := intersectQuadRay(ray, stroke, &roots)
	if n != 1 {
		return resultSplit
	}
	quadPt := evalQuadAt(stroke, roots[0])
	e := s.invResScale * (1 - absf(roots[0]-0.5)*2)
	if pointsWithinDist(ray[0], quadPt, e) {
		if sharpAngle(&qp.quad) {
			return resultSplit
		}
		return resultQuad
	}
	return resultSplit
}

func (s *stroker) intersectRay(kind intersectRayType, qp *quadConstruct) resultType {
	start := qp.quad[0]
	end := qp.quad[2]
	aLen := qp.tangentStart.sub(start)
	bLen := qp.tangentEnd.sub(end)
	denom := aLen.cross(bLen)
	if denom == 0 || !isFinite(denom) {
		qp.oppositeTangents = aLen.dot(bLen) < 0
		return resultDegenerate
	}
	qp.oppositeTangents = false
	ab0 := start.sub(end)
	numerA := bLen.cross(ab0)
	numerB := aLen.cross(ab0)
	if (numerA >= 0) == (numerB >= 0) {
		dist1 := ptToLine(start, end, qp.tangentEnd)
		dist2 := ptToLine(end, start, qp.tangentStart)
		if maxf(dist1, dist2) <= s.invResScaleSquared {
			return resultDegenerate
		}
		return resultSplit
	}
	numerA /= denom
	if numerA > numerA-1 {
		if kind == rayCtrlPt {
			qp.quad[1].X = start.X*(1-numerA) + qp.tangentStart.X*numerA
			qp.quad[1].Y = start.Y*(1-numerA) + qp.tangentStart.Y*numerA
		}
		return resultQuad
	}
	qp.oppositeTangents = aLen.dot(bLen) < 0
	return resultDegenerate
}

func (s *stroker) tangentsMeet(cubic *[4]Point, qp *quadConstruct) resultType {
	s.cubicQuadEnds(cubic, qp)
	return s.intersectRay(rayResultType, qp)
}

func (s *stroker) finish(isLine bool) (*Path, bool) {
	s.finishContour(false, isLine)
	buf := s.outer
	s.outer = NewBuilder()
	return buf.Finish()
}

func (s *stroker) isCurrentContourEmpty() bool {
	return s.inner.isZeroLengthSincePoint(0) && s.outer.isZeroLengthSincePoint(s.firstOuterPtIndexInContour)
}

// swappable is SwappableBuilders: joiners swap which builder is outer.
type swappable struct{ inner, outer *Builder }

func (b *swappable) swap() { b.inner, b.outer = b.outer, b.inner }

func roundCapper(pivot, normal, stop Point, path *Builder) {
	parallel := normal
	parallel.rotateCW()
	center := pivot.add(parallel)
	path.conicPointsTo(center.add(normal), center, scalarRoot2Over2)
	path.conicPointsTo(center.sub(normal), stop, scalarRoot2Over2)
}

func squareCapper(pivot, normal, stop Point, other bool, path *Builder) {
	parallel := normal
	parallel.rotateCW()
	if other {
		path.setLastPoint(Point{pivot.X + normal.X + parallel.X, pivot.Y + normal.Y + parallel.Y})
		path.LineTo(pivot.X-normal.X+parallel.X, pivot.Y-normal.Y+parallel.Y)
		return
	}
	path.LineTo(pivot.X+normal.X+parallel.X, pivot.Y+normal.Y+parallel.Y)
	path.LineTo(pivot.X-normal.X+parallel.X, pivot.Y-normal.Y+parallel.Y)
	path.LineTo(stop.X, stop.Y)
}

func isClockwise(before, after Point) bool { return before.X*after.Y > before.Y*after.X }

type angleType uint8

const (
	angleNearly180 angleType = iota
	angleSharp
	angleShallow
	angleNearlyLine
)

func dotToAngleType(dot float32) angleType {
	if dot >= 0 {
		if nearlyZero(1 - dot) {
			return angleNearlyLine
		}
		return angleShallow
	}
	if nearlyZero(1 + dot) {
		return angleNearly180
	}
	return angleSharp
}

func handleInnerJoin(pivot, after Point, inner *Builder) {
	inner.LineTo(pivot.X, pivot.Y)
	inner.LineTo(pivot.X-after.X, pivot.Y-after.Y)
}

func bevelJoiner(beforeUnit, pivot, afterUnit Point, radius float32, b swappable) {
	after := afterUnit.scaled(radius)
	if !isClockwise(beforeUnit, afterUnit) {
		b.swap()
		after = after.neg()
	}
	b.outer.LineTo(pivot.X+after.X, pivot.Y+after.Y)
	handleInnerJoin(pivot, after, b.inner)
}

func roundJoiner(beforeUnit, pivot, afterUnit Point, radius float32, b swappable) {
	dotProd := beforeUnit.dot(afterUnit)
	if dotToAngleType(dotProd) == angleNearlyLine {
		return
	}
	before, after := beforeUnit, afterUnit
	dir := dirCW
	if !isClockwise(before, after) {
		b.swap()
		before = before.neg()
		after = after.neg()
		dir = dirCCW
	}
	ts := FromRow(radius, 0, 0, radius, pivot.X, pivot.Y)
	var conics [5]conic
	arc := buildUnitArc(before, after, dir, ts, &conics)
	if arc == nil {
		return
	}
	for _, c := range arc {
		b.outer.conicPointsTo(c.points[1], c.points[2], c.weight)
	}
	after.scale(radius)
	handleInnerJoin(pivot, after, b.inner)
}

func miterJoinerInner(beforeUnit, pivot, afterUnit Point, radius, invMiterLimit float32, miterClip, prevIsLine, currIsLine bool, b swappable) {
	doBluntOrClipped := func(b swappable, currIsLine bool, before, mid, after Point) {
		after.scale(radius)
		if miterClip {
			mid.normalize()
			cosBeta := before.dot(mid)
			sinBeta := before.cross(mid)
			var x float32
			if absf(sinBeta) <= scalarNearlyZero {
				x = 1 / invMiterLimit
			} else {
				x = ((1 / invMiterLimit) - cosBeta) / sinBeta
			}
			before.scale(radius)
			beforeTangent := before
			beforeTangent.rotateCW()
			afterTangent := after
			afterTangent.rotateCCW()
			c1 := pivot.add(before).add(beforeTangent.scaled(x))
			c2 := pivot.add(after).add(afterTangent.scaled(x))
			if prevIsLine {
				b.outer.setLastPoint(c1)
			} else {
				b.outer.LineTo(c1.X, c1.Y)
			}
			b.outer.LineTo(c2.X, c2.Y)
		}
		if !currIsLine {
			b.outer.LineTo(pivot.X+after.X, pivot.Y+after.Y)
		}
		handleInnerJoin(pivot, after, b.inner)
	}
	doMiter := func(b swappable, mid, after Point) {
		after.scale(radius)
		if prevIsLine {
			b.outer.setLastPoint(Point{pivot.X + mid.X, pivot.Y + mid.Y})
		} else {
			b.outer.LineTo(pivot.X+mid.X, pivot.Y+mid.Y)
		}
		if !currIsLine {
			b.outer.LineTo(pivot.X+after.X, pivot.Y+after.Y)
		}
		handleInnerJoin(pivot, after, b.inner)
	}

	dotProd := beforeUnit.dot(afterUnit)
	at := dotToAngleType(dotProd)
	before, after := beforeUnit, afterUnit
	if at == angleNearlyLine {
		return
	}
	if at == angleNearly180 {
		mid := after.sub(before).scaled(radius / 2)
		doBluntOrClipped(b, false, before, mid, after)
		return
	}
	ccw := !isClockwise(before, after)
	if ccw {
		b.swap()
		before = before.neg()
		after = after.neg()
	}
	if dotProd == 0 && invMiterLimit <= scalarRoot2Over2 {
		mid := before.add(after).scaled(radius)
		doMiter(b, mid, after)
		return
	}
	var mid Point
	if at == angleSharp {
		mid = Point{after.Y - before.Y, before.X - after.X}
		if ccw {
			mid = mid.neg()
		}
	} else {
		mid = Point{before.X + after.X, before.Y + after.Y}
	}
	sinHalfAngle := sqrtf((1 + dotProd) * 0.5)
	if sinHalfAngle < invMiterLimit {
		doBluntOrClipped(b, false, before, mid, after)
		return
	}
	mid.setLength(radius / sinHalfAngle)
	doMiter(b, mid, after)
}

func setNormalUnitNormal(before, after Point, scale, radius float32, normal, unitNormal *Point) bool {
	if !unitNormal.setNormalize((after.X-before.X)*scale, (after.Y-before.Y)*scale) {
		return false
	}
	unitNormal.rotateCCW()
	*normal = unitNormal.scaled(radius)
	return true
}

func setNormalUnitNormal2(vec Point, radius float32, normal, unitNormal *Point) bool {
	if !unitNormal.setNormalize(vec.X, vec.Y) {
		return false
	}
	unitNormal.rotateCCW()
	*normal = unitNormal.scaled(radius)
	return true
}

func checkQuadLinear(quad *[3]Point) (Point, reductionType) {
	degAB := !quad[1].sub(quad[0]).canNormalize()
	degBC := !quad[2].sub(quad[1]).canNormalize()
	if degAB && degBC {
		return Point{}, reducePoint
	}
	if degAB || degBC {
		return Point{}, reduceLine
	}
	if !quadInLine(quad) {
		return Point{}, reduceQuad
	}
	t := findQuadMaxCurvature(quad)
	if t == 0 || t == 1 {
		return Point{}, reduceLine
	}
	return evalQuadAt(quad, t), reduceDegenerate
}

func quadInLine(quad *[3]Point) bool {
	ptMax := float32(-1)
	outer1, outer2 := 0, 0
	for index := range 2 {
		for inner := index + 1; inner < 3; inner++ {
			d := quad[inner].sub(quad[index])
			if m := maxf(absf(d.X), absf(d.Y)); ptMax < m {
				outer1, outer2, ptMax = index, inner, m
			}
		}
	}
	mid := outer1 ^ outer2 ^ 3
	const curvatureSlop float32 = 0.000005
	lineSlop := ptMax * ptMax * curvatureSlop
	return ptToLine(quad[mid], quad[outer1], quad[outer2]) <= lineSlop
}

func ptToLine(pt, lineStart, lineEnd Point) float32 {
	dxy := lineEnd.sub(lineStart)
	ab0 := pt.sub(lineStart)
	numer := dxy.dot(ab0)
	denom := dxy.dot(dxy)
	t := numer / denom
	if t >= 0 && t <= 1 {
		hit := Point{lineStart.X*(1-t) + lineEnd.X*t, lineStart.Y*(1-t) + lineEnd.Y*t}
		return hit.distanceToSqd(pt)
	}
	return pt.distanceToSqd(lineStart)
}

func intersectQuadRay(line [2]Point, quad *[3]Point, roots *[3]float32) int {
	vec := line[1].sub(line[0])
	var r [3]float32
	for n := range 3 {
		r[n] = (quad[n].Y-line[0].Y)*vec.X - (quad[n].X-line[0].X)*vec.Y
	}
	a, b, c := r[2], r[1], r[0]
	a += c - 2*b
	b -= c
	return findUnitQuadRoots(a, 2*b, c, roots)
}

func pointsWithinDist(near, far Point, limit float32) bool {
	return near.distanceToSqd(far) <= limit*limit
}

func sharpAngle(quad *[3]Point) bool {
	smaller := quad[1].sub(quad[0])
	larger := quad[1].sub(quad[2])
	smallerLen := smaller.lengthSqd()
	largerLen := larger.lengthSqd()
	if smallerLen > largerLen {
		smaller, larger = larger, smaller
		largerLen = smallerLen
	}
	if !smaller.setLength(largerLen) {
		return false
	}
	return smaller.dot(larger) > 0
}

func ptInQuadBounds(quad *[3]Point, pt Point, inv float32) bool {
	if pt.X+inv < minf(minf(quad[0].X, quad[1].X), quad[2].X) {
		return false
	}
	if pt.X-inv > maxf(maxf(quad[0].X, quad[1].X), quad[2].X) {
		return false
	}
	if pt.Y+inv < minf(minf(quad[0].Y, quad[1].Y), quad[2].Y) {
		return false
	}
	if pt.Y-inv > maxf(maxf(quad[0].Y, quad[1].Y), quad[2].Y) {
		return false
	}
	return true
}

func checkCubicLinear(cubic *[4]Point, reduction *[3]Point, tangentPt *Point) reductionType {
	degAB := !cubic[1].sub(cubic[0]).canNormalize()
	degBC := !cubic[2].sub(cubic[1]).canNormalize()
	degCD := !cubic[3].sub(cubic[2]).canNormalize()
	if degAB && degBC && degCD {
		return reducePoint
	}
	count := 0
	for _, d := range []bool{degAB, degBC, degCD} {
		if d {
			count++
		}
	}
	if count == 2 {
		return reduceLine
	}
	if !cubicInLine(cubic) {
		if degAB {
			*tangentPt = cubic[2]
		} else {
			*tangentPt = cubic[1]
		}
		return reduceQuad
	}
	var tv [3]float32
	n := findCubicMaxCurvature(cubic, &tv)
	rCount := 0
	for _, t := range tv[:n] {
		if 0 >= t || t >= 1 {
			continue
		}
		reduction[rCount] = evalCubicPosAt(cubic, t)
		if reduction[rCount] != cubic[0] && reduction[rCount] != cubic[3] {
			rCount++
		}
	}
	return [4]reductionType{reduceLine, reduceDegenerate, reduceDegenerate2, reduceDegenerate3}[rCount]
}

func cubicInLine(cubic *[4]Point) bool {
	ptMax := float32(-1)
	outer1, outer2 := 0, 0
	for index := range 3 {
		for inner := index + 1; inner < 4; inner++ {
			d := cubic[inner].sub(cubic[index])
			if m := maxf(absf(d.X), absf(d.Y)); ptMax < m {
				outer1, outer2, ptMax = index, inner, m
			}
		}
	}
	mid1 := (1 + (2 >> outer2)) >> outer1
	mid2 := outer1 ^ outer2 ^ mid1
	lineSlop := ptMax * ptMax * 0.00001
	return ptToLine(cubic[mid1], cubic[outer1], cubic[outer2]) <= lineSlop &&
		ptToLine(cubic[mid2], cubic[outer1], cubic[outer2]) <= lineSlop
}
