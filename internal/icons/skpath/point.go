// Package skpath is a port of the parts of tiny-skia-path 0.11
// (Skia's SkPath, SkStroke and SkDashPath) the icon transform needs:
// path building, affine transforms, stroking into fill outlines, and
// dashing. Arithmetic stays in float32 with the same operation order as
// the Rust code so the emitted coordinates match it.
package skpath

import "math"

const (
	scalarNearlyZero   float32 = 1.0 / (1 << 12)
	scalarRoot2Over2   float32 = 0.707106781
	floatPI            float32 = 3.14159265
	float32Epsilon     float32 = 1.1920929e-07
	maxI32FitsInF32    float32 = 2147483520.0
	float32MaxFinite           = math.MaxFloat32
	defaultMiterLimit  float32 = 4.0
	resolutionFallback float32 = 1.0
)

// Point is a 2D point (tiny-skia's Point).
type Point struct{ X, Y float32 }

// Pt builds a point.
func Pt(x, y float32) Point { return Point{x, y} }

func (p Point) add(o Point) Point      { return Point{p.X + o.X, p.Y + o.Y} }
func (p Point) sub(o Point) Point      { return Point{p.X - o.X, p.Y - o.Y} }
func (p Point) neg() Point             { return Point{-p.X, -p.Y} }
func (p Point) scaled(s float32) Point { return Point{p.X * s, p.Y * s} }
func (p Point) dot(o Point) float32    { return p.X*o.X + p.Y*o.Y }
func (p Point) cross(o Point) float32  { return p.X*o.Y - p.Y*o.X }
func (p Point) isZero() bool           { return p.X == 0 && p.Y == 0 }
func (p Point) lengthSqd() float32     { return p.dot(p) }

func (p Point) isFinite() bool { return isFinite(p.X * p.Y) }

func (p Point) canNormalize() bool {
	return isFinite(p.X) && isFinite(p.Y) && (p.X != 0 || p.Y != 0)
}

func (p Point) almostEqual(o Point) bool { return !p.sub(o).canNormalize() }

func (p Point) equalsWithinTolerance(o Point, tol float32) bool {
	return nearlyZeroTol(p.X-o.X, tol) && nearlyZeroTol(p.Y-o.Y, tol)
}

func (p Point) length() float32 {
	mag2 := p.X*p.X + p.Y*p.Y
	if isFinite(mag2) {
		return sqrtf(mag2)
	}
	xx, yy := float64(p.X), float64(p.Y)
	return float32(math.Sqrt(xx*xx + yy*yy))
}

func (p Point) distanceToSqd(o Point) float32 {
	dx := p.X - o.X
	dy := p.Y - o.Y
	return dx*dx + dy*dy
}

func (p Point) distance(o Point) float32 { return p.sub(o).length() }

func (p *Point) normalize() bool { return p.setLengthFrom(p.X, p.Y, 1) }

func (p *Point) setNormalize(x, y float32) bool { return p.setLengthFrom(x, y, 1) }

func (p *Point) setLength(l float32) bool { return p.setLengthFrom(p.X, p.Y, l) }

// setLengthFrom is set_point_length: computed in f64 so large
// components do not overflow.
func (p *Point) setLengthFrom(x, y, length float32) bool {
	xx, yy := float64(x), float64(y)
	dmag := math.Sqrt(xx*xx + yy*yy)
	dscale := float64(length) / dmag
	x *= float32(dscale)
	y *= float32(dscale)
	if !isFinite(x) || !isFinite(y) || (x == 0 && y == 0) {
		*p = Point{}
		return false
	}
	*p = Point{x, y}
	return true
}

func (p *Point) scale(s float32) { p.X *= s; p.Y *= s }

func (p *Point) rotateCW() { p.X, p.Y = -p.Y, p.X }

func (p *Point) rotateCCW() { p.X, p.Y = p.Y, -p.X }

func isFinite(f float32) bool {
	return !math.IsInf(float64(f), 0) && !math.IsNaN(float64(f))
}

func sqrtf(f float32) float32 { return float32(math.Sqrt(float64(f))) }

func absf(f float32) float32 { return float32(math.Abs(float64(f))) }

// minf and maxf are Rust's f32::min/max: a NaN argument yields the other.
func minf(a, b float32) float32 {
	switch {
	case a != a:
		return b
	case b != b:
		return a
	case b < a:
		return b
	default:
		return a
	}
}

func maxf(a, b float32) float32 {
	switch {
	case a != a:
		return b
	case b != b:
		return a
	case b > a:
		return b
	default:
		return a
	}
}

func nearlyZero(f float32) bool { return nearlyZeroTol(f, scalarNearlyZero) }

func nearlyZeroTol(f, tol float32) bool { return absf(f) <= tol }

// bound is Scalar::bound: max.min(self).max(min), max for NaN.
func bound(v, lo, hi float32) float32 { return maxf(minf(hi, v), lo) }

func invert(f float32) float32 { return 1 / f }

// clamp01 is NormalizedF32::new_clamped: NaN and infinities become 0.
func clamp01(f float32) float32 {
	if !isFinite(f) {
		return 0
	}
	return minf(maxf(f, 0), 1)
}

// boundedExclusive is NormalizedF32Exclusive::new_bounded.
func boundedExclusive(f float32) float32 {
	return bound(f, float32Epsilon, 1-float32Epsilon)
}

// validUnitDivide is path_geometry::valid_unit_divide: numer/denom when
// it lies strictly inside (0, 1).
func validUnitDivide(numer, denom float32) (float32, bool) {
	if numer < 0 {
		numer = -numer
		denom = -denom
	}
	if denom == 0 || numer == 0 || numer >= denom {
		return 0, false
	}
	r := numer / denom
	if r > 0 && r < 1 && isFinite(r) {
		return r, true
	}
	return 0, false
}
