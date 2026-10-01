package skpath

import "math"

// This file ports path_geometry.rs. The f32x2 lane math is written out
// per component in the same operation order.

func interpPt(v0, v1 Point, t float32) Point {
	return Point{v0.X + (v1.X-v0.X)*t, v0.Y + (v1.Y-v0.Y)*t}
}

func times2(p Point) Point { return Point{p.X + p.X, p.Y + p.Y} }

func evalQuadAt(src *[3]Point, t float32) Point {
	c := src[0]
	p1 := src[1]
	p2 := src[2]
	b := times2(p1.sub(c))
	a := p2.sub(times2(p1)).add(c)
	return Point{(a.X*t+b.X)*t + c.X, (a.Y*t+b.Y)*t + c.Y}
}

func evalQuadTangentAt(src *[3]Point, t float32) Point {
	if (t == 0 && src[0] == src[1]) || (t == 1 && src[1] == src[2]) {
		return src[2].sub(src[0])
	}
	b := src[1].sub(src[0])
	a := src[2].sub(src[1]).sub(b)
	tt := Point{a.X*t + b.X, a.Y*t + b.Y}
	return tt.add(tt)
}

func evalCubicPosAt(src *[4]Point, t float32) Point {
	p0, p1, p2, p3 := src[0], src[1], src[2], src[3]
	a := Point{p3.X + 3*(p1.X-p2.X) - p0.X, p3.Y + 3*(p1.Y-p2.Y) - p0.Y}
	b := Point{3 * (p2.X - (p1.X + p1.X) + p0.X), 3 * (p2.Y - (p1.Y + p1.Y) + p0.Y)}
	c := Point{3 * (p1.X - p0.X), 3 * (p1.Y - p0.Y)}
	return Point{((a.X*t+b.X)*t+c.X)*t + p0.X, ((a.Y*t+b.Y)*t+c.Y)*t + p0.Y}
}

func evalCubicTangentAt(src *[4]Point, t float32) Point {
	if (t == 0 && src[0] == src[1]) || (t == 1 && src[2] == src[3]) {
		var tangent Point
		if t == 0 {
			tangent = src[2].sub(src[0])
		} else {
			tangent = src[3].sub(src[1])
		}
		if tangent.X == 0 && tangent.Y == 0 {
			tangent = src[3].sub(src[0])
		}
		return tangent
	}
	return evalCubicDerivative(src, t)
}

func evalCubicDerivative(src *[4]Point, t float32) Point {
	p0, p1, p2, p3 := src[0], src[1], src[2], src[3]
	a := Point{p3.X + 3*(p1.X-p2.X) - p0.X, p3.Y + 3*(p1.Y-p2.Y) - p0.Y}
	b := times2(p2.sub(times2(p1)).add(p0))
	c := p1.sub(p0)
	return Point{(a.X*t+b.X)*t + c.X, (a.Y*t+b.Y)*t + c.Y}
}

func chopQuadAt(src []Point, t float32, dst *[5]Point) {
	p0, p1, p2 := src[0], src[1], src[2]
	p01 := interpPt(p0, p1, t)
	p12 := interpPt(p1, p2, t)
	dst[0] = p0
	dst[1] = p01
	dst[2] = interpPt(p01, p12, t)
	dst[3] = p12
	dst[4] = p2
}

func chopCubicAt2(src *[4]Point, t float32, dst []Point) {
	p0, p1, p2, p3 := src[0], src[1], src[2], src[3]
	ab := interpPt(p0, p1, t)
	bc := interpPt(p1, p2, t)
	cd := interpPt(p2, p3, t)
	abc := interpPt(ab, bc, t)
	bcd := interpPt(bc, cd, t)
	abcd := interpPt(abc, bcd, t)
	dst[0], dst[1], dst[2], dst[3], dst[4], dst[5], dst[6] = p0, ab, abc, abcd, bcd, cd, p3
}

// findUnitQuadRoots solves At^2+Bt+C in (0,1) (Numerical Recipes).
func findUnitQuadRoots(a, b, c float32, roots *[3]float32) int {
	if a == 0 {
		if r, ok := validUnitDivide(-c, b); ok {
			roots[0] = r
			return 1
		}
		return 0
	}
	dr := float64(b)*float64(b) - 4*float64(a)*float64(c)
	if dr < 0 {
		return 0
	}
	dr = math.Sqrt(dr)
	r := float32(dr)
	if !isFinite(r) {
		return 0
	}
	var q float32
	if b < 0 {
		q = -(b - r) / 2
	} else {
		q = -(b + r) / 2
	}
	n := 0
	if v, ok := validUnitDivide(q, a); ok {
		roots[n] = v
		n++
	}
	if v, ok := validUnitDivide(c, q); ok {
		roots[n] = v
		n++
	}
	if n == 2 {
		if roots[0] > roots[1] {
			roots[0], roots[1] = roots[1], roots[0]
		} else if roots[0] == roots[1] {
			n--
		}
	}
	return n
}

func findCubicExtrema(a, b, c, d float32, t *[3]float32) int {
	aa := d - a + 3*(b-c)
	bb := 2 * (a - b - b + c)
	cc := b - a
	return findUnitQuadRoots(aa, bb, cc, t)
}

func findQuadMaxCurvature(src *[3]Point) float32 {
	ax := src[1].X - src[0].X
	ay := src[1].Y - src[0].Y
	bx := src[0].X - src[1].X - src[1].X + src[2].X
	by := src[0].Y - src[1].Y - src[1].Y + src[2].Y
	numer := -(ax*bx + ay*by)
	denom := bx*bx + by*by
	if denom < 0 {
		numer = -numer
		denom = -denom
	}
	if numer <= 0 {
		return 0
	}
	if numer >= denom {
		return 1
	}
	return numer / denom
}

func findCubicMaxCurvature(src *[4]Point, t *[3]float32) int {
	cx := formulateF1DotF2([4]float32{src[0].X, src[1].X, src[2].X, src[3].X})
	cy := formulateF1DotF2([4]float32{src[0].Y, src[1].Y, src[2].Y, src[3].Y})
	for i := range 4 {
		cx[i] += cy[i]
	}
	return solveCubicPoly(&cx, t)
}

func formulateF1DotF2(s [4]float32) [4]float32 {
	a := s[1] - s[0]
	b := s[2] - 2*s[1] + s[0]
	c := s[3] + 3*(s[1]-s[2]) - s[0]
	return [4]float32{c * c, 3 * b * c, 2*b*b + c*a, a * b}
}

func cosf(x float32) float32 { return float32(math.Cos(float64(x))) }

func solveCubicPoly(coeff *[4]float32, t *[3]float32) int {
	if nearlyZero(coeff[0]) {
		var tmp [3]float32
		n := findUnitQuadRoots(coeff[1], coeff[2], coeff[3], &tmp)
		copy(t[:n], tmp[:n])
		return n
	}
	inva := invert(coeff[0])
	a := coeff[1] * inva
	b := coeff[2] * inva
	c := coeff[3] * inva
	q := (a*a - b*3) / 9
	r := (2*a*a*a - 9*a*b + 27*c) / 54
	q3 := q * q * q
	r2MinusQ3 := r*r - q3
	adiv3 := a / 3
	if r2MinusQ3 < 0 {
		theta := float32(math.Acos(float64(bound(r/sqrtf(q3), -1, 1))))
		neg2RootQ := -2 * sqrtf(q)
		t[0] = clamp01(neg2RootQ*cosf(theta/3) - adiv3)
		t[1] = clamp01(neg2RootQ*cosf((theta+2*floatPI)/3) - adiv3)
		t[2] = clamp01(neg2RootQ*cosf((theta-2*floatPI)/3) - adiv3)
		if t[0] > t[1] {
			t[0], t[1] = t[1], t[0]
		}
		if t[1] > t[2] {
			t[1], t[2] = t[2], t[1]
		}
		if t[0] > t[1] {
			t[0], t[1] = t[1], t[0]
		}
		n := 3
		if t[1] == t[2] {
			n = 2
		}
		if t[0] == t[1] {
			n = 1
		}
		return n
	}
	aa := absf(r) + sqrtf(r2MinusQ3)
	aa = float32(math.Pow(float64(aa), float64(float32(0.3333333))))
	if r > 0 {
		aa = -aa
	}
	if aa != 0 {
		aa += q / aa
	}
	t[0] = clamp01(aa - adiv3)
	return 1
}

func findCubicInflections(src *[4]Point, t *[3]float32) int {
	ax := src[1].X - src[0].X
	ay := src[1].Y - src[0].Y
	bx := src[2].X - 2*src[1].X + src[0].X
	by := src[2].Y - 2*src[1].Y + src[0].Y
	cx := src[3].X + 3*(src[1].X-src[2].X) - src[0].X
	cy := src[3].Y + 3*(src[1].Y-src[2].Y) - src[0].Y
	return findUnitQuadRoots(bx*cy-by*cx, ax*cy-ay*cx, ax*by-ay*bx, t)
}

func findCubicCusp(src *[4]Point) (float32, bool) {
	if src[0] == src[1] || src[2] == src[3] {
		return 0, false
	}
	if onSameSide(src, 0, 2) || onSameSide(src, 2, 0) {
		return 0, false
	}
	var t [3]float32
	n := findCubicMaxCurvature(src, &t)
	for _, tt := range t[:n] {
		if 0 >= tt || tt >= 1 {
			continue
		}
		d := evalCubicDerivative(src, tt)
		if d.lengthSqd() < calcCubicPrecision(src) {
			return boundedExclusive(tt), true
		}
	}
	return 0, false
}

func onSameSide(src *[4]Point, testIndex, lineIndex int) bool {
	origin := src[lineIndex]
	line := src[lineIndex+1].sub(origin)
	var crosses [2]float32
	for i := range 2 {
		crosses[i] = line.cross(src[testIndex+i].sub(origin))
	}
	return crosses[0]*crosses[1] >= 0
}

func calcCubicPrecision(src *[4]Point) float32 {
	return (src[1].distanceToSqd(src[0]) + src[2].distanceToSqd(src[1]) + src[3].distanceToSqd(src[2])) * 1e-8
}

// conic is path_geometry::Conic.
type conic struct {
	points [3]Point
	weight float32
}

func (c conic) computeQuadPow2(tol float32) (int, bool) {
	if tol < 0 || !isFinite(tol) {
		return 0, false
	}
	if !c.points[0].isFinite() || !c.points[1].isFinite() || !c.points[2].isFinite() {
		return 0, false
	}
	a := c.weight - 1
	k := a / (4 * (2 + a))
	x := k * (c.points[0].X - 2*c.points[1].X + c.points[2].X)
	y := k * (c.points[0].Y - 2*c.points[1].Y + c.points[2].Y)
	e := sqrtf(x*x + y*y)
	pow2 := 0
	for range 4 {
		if e <= tol {
			break
		}
		e *= 0.25
		pow2++
	}
	return max(pow2, 1), true
}

func (c conic) chopIntoQuadsPow2(pow2 int, points []Point) int {
	points[0] = c.points[0]
	subdivide(c, points[1:], pow2)
	quadCount := 1 << pow2
	ptCount := 2*quadCount + 1
	for _, p := range points[:ptCount] {
		if !p.isFinite() {
			for i := 1; i < ptCount-1; i++ {
				points[i] = c.points[1]
			}
			break
		}
	}
	return quadCount
}

func (c conic) chop() (conic, conic) {
	scale := invert(1 + c.weight)
	newW := sqrtf(0.5 + c.weight*0.5)
	p0, p1, p2 := c.points[0], c.points[1], c.points[2]
	w := c.weight
	wp1 := Point{w * p1.X, w * p1.Y}
	m := Point{
		(p0.X + (wp1.X + wp1.X) + p2.X) * scale * 0.5,
		(p0.Y + (wp1.Y + wp1.Y) + p2.Y) * scale * 0.5,
	}
	if !m.isFinite() {
		wd := float64(c.weight)
		w2 := wd * 2
		sh := 1 / (1 + wd) * 0.5
		m.X = float32((float64(p0.X) + w2*float64(p1.X) + float64(p2.X)) * sh)
		m.Y = float32((float64(p0.Y) + w2*float64(p1.Y) + float64(p2.Y)) * sh)
	}
	return conic{[3]Point{p0, {(p0.X + wp1.X) * scale, (p0.Y + wp1.Y) * scale}, m}, newW},
		conic{[3]Point{m, {(wp1.X + p2.X) * scale, (wp1.Y + p2.Y) * scale}, p2}, newW}
}

func between(a, b, c float32) bool { return (a-b)*(c-b) <= 0 }

func subdivide(src conic, points []Point, level int) []Point {
	if level == 0 {
		points[0] = src.points[1]
		points[1] = src.points[2]
		return points[2:]
	}
	d0, d1 := src.chop()
	startY := src.points[0].Y
	endY := src.points[2].Y
	if between(startY, src.points[1].Y, endY) {
		midY := d0.points[2].Y
		if !between(startY, midY, endY) {
			closer := endY
			if absf(midY-startY) < absf(midY-endY) {
				closer = startY
			}
			d0.points[2].Y = closer
			d1.points[0].Y = closer
		}
		if !between(startY, d0.points[1].Y, d0.points[2].Y) {
			d0.points[1].Y = startY
		}
		if !between(d1.points[0].Y, d1.points[1].Y, endY) {
			d1.points[1].Y = endY
		}
	}
	level--
	points = subdivide(d0, points, level)
	return subdivide(d1, points, level)
}

func autoConicToQuads(p0, p1, p2 Point, weight float32) ([64]Point, int, bool) {
	c := conic{[3]Point{p0, p1, p2}, weight}
	var pts [64]Point
	pow2, ok := c.computeQuadPow2(0.25)
	if !ok {
		return pts, 0, false
	}
	n := c.chopIntoQuadsPow2(pow2, pts[:])
	return pts, n, true
}

type pathDirection uint8

const (
	dirCW pathDirection = iota
	dirCCW
)

// buildUnitArc is Conic::build_unit_arc.
func buildUnitArc(uStart, uStop Point, dir pathDirection, user Transform, dst *[5]conic) []conic {
	x := uStart.dot(uStop)
	y := uStart.cross(uStop)
	absY := absf(y)
	if absY <= scalarNearlyZero && x > 0 && ((y >= 0 && dir == dirCW) || (y <= 0 && dir == dirCCW)) {
		return nil
	}
	if dir == dirCCW {
		y = -y
	}
	quadrant := 0
	switch {
	case y == 0:
		quadrant = 2
	case x == 0:
		if y > 0 {
			quadrant = 1
		} else {
			quadrant = 3
		}
	default:
		if y < 0 {
			quadrant += 2
		}
		if (x < 0) != (y < 0) {
			quadrant++
		}
	}
	quadrantPoints := [8]Point{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	count := quadrant
	for i := range count {
		dst[i] = conic{[3]Point{quadrantPoints[i*2], quadrantPoints[i*2+1], quadrantPoints[(i*2+2)%8]}, scalarRoot2Over2}
	}
	finalPt := Point{x, y}
	lastQ := quadrantPoints[quadrant*2]
	dot := lastQ.dot(finalPt)
	if dot < 1 {
		offCurve := Point{lastQ.X + x, lastQ.Y + y}
		cosThetaOver2 := sqrtf((1 + dot) / 2)
		offCurve.setLength(invert(cosThetaOver2))
		if !lastQ.almostEqual(offCurve) {
			dst[count] = conic{[3]Point{lastQ, offCurve, finalPt}, cosThetaOver2}
			count++
		}
	}
	ts := fromSinCos(uStart.Y, uStart.X)
	if dir == dirCCW {
		ts = ts.PreScale(1, -1)
	}
	ts = ts.PostConcat(user)
	for i := range count {
		ts.MapPoints(dst[i].points[:])
	}
	if count == 0 {
		return nil
	}
	return dst[:count]
}
