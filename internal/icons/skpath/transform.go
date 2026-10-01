package skpath

import "math"

// Transform is an affine matrix (tiny-skia's Transform): x' = sx*x +
// kx*y + tx, y' = ky*x + sy*y + ty.
type Transform struct {
	SX, KY, KX, SY, TX, TY float32
}

// Identity is the identity transform.
var Identity = Transform{SX: 1, SY: 1}

// FromRow builds a transform from its row-major components.
func FromRow(sx, ky, kx, sy, tx, ty float32) Transform {
	return Transform{SX: sx, KY: ky, KX: kx, SY: sy, TX: tx, TY: ty}
}

// FromTranslate is a translation.
func FromTranslate(tx, ty float32) Transform { return FromRow(1, 0, 0, 1, tx, ty) }

// FromScale is a scale.
func FromScale(sx, sy float32) Transform { return FromRow(sx, 0, 0, sy, 0, 0) }

func (t Transform) isFinite() bool {
	return isFinite(t.SX) && isFinite(t.KY) && isFinite(t.KX) && isFinite(t.SY) && isFinite(t.TX) && isFinite(t.TY)
}

// IsValid reports a finite transform whose scale is not degenerate.
func (t Transform) IsValid() bool {
	if !t.isFinite() {
		return false
	}
	sx, sy := t.Scale()
	return !(nearlyZeroTol(sx, float32Epsilon) || nearlyZeroTol(sy, float32Epsilon))
}

// IsIdentity reports the identity.
func (t Transform) IsIdentity() bool { return t == Identity }

func (t Transform) hasScale() bool     { return t.SX != 1 || t.SY != 1 }
func (t Transform) hasSkew() bool      { return t.KX != 0 || t.KY != 0 }
func (t Transform) hasTranslate() bool { return t.TX != 0 || t.TY != 0 }

func (t Transform) isTranslate() bool {
	return !t.hasScale() && !t.hasSkew() && t.hasTranslate()
}

func (t Transform) isScaleTranslate() bool {
	return (t.hasScale() || t.hasTranslate()) && !t.hasSkew()
}

// Scale is get_scale: the lengths of the transformed unit axes.
func (t Transform) Scale() (float32, float32) {
	return sqrtf(t.SX*t.SX + t.KX*t.KX), sqrtf(t.KY*t.KY + t.SY*t.SY)
}

// PreConcat is self * other.
func (t Transform) PreConcat(o Transform) Transform { return concat(t, o) }

// PostConcat is other * self.
func (t Transform) PostConcat(o Transform) Transform { return concat(o, t) }

// PreTranslate is self * translate(tx, ty).
func (t Transform) PreTranslate(tx, ty float32) Transform {
	return t.PreConcat(FromTranslate(tx, ty))
}

// PreScale is self * scale(sx, sy).
func (t Transform) PreScale(sx, sy float32) Transform { return t.PreConcat(FromScale(sx, sy)) }

func fromSinCos(sin, cos float32) Transform { return FromRow(cos, sin, -sin, cos, 0, 0) }

func concat(a, b Transform) Transform {
	switch {
	case a.IsIdentity():
		return b
	case b.IsIdentity():
		return a
	case !a.hasSkew() && !b.hasSkew():
		return FromRow(a.SX*b.SX, 0, 0, a.SY*b.SY, a.SX*b.TX+a.TX, a.SY*b.TY+a.TY)
	default:
		return FromRow(
			mulAddMul(a.SX, b.SX, a.KX, b.KY),
			mulAddMul(a.KY, b.SX, a.SY, b.KY),
			mulAddMul(a.SX, b.KX, a.KX, b.SY),
			mulAddMul(a.KY, b.KX, a.SY, b.SY),
			mulAddMul(a.SX, b.TX, a.KX, b.TY)+a.TX,
			mulAddMul(a.KY, b.TX, a.SY, b.TY)+a.TY,
		)
	}
}

func mulAddMul(a, b, c, d float32) float32 {
	return float32(float64(a)*float64(b) + float64(c)*float64(d))
}

// MapPoints transforms points in place.
func (t Transform) MapPoints(points []Point) {
	switch {
	case len(points) == 0 || t.IsIdentity():
	case t.isTranslate():
		for i := range points {
			points[i].X += t.TX
			points[i].Y += t.TY
		}
	case t.isScaleTranslate():
		for i := range points {
			points[i].X = points[i].X*t.SX + t.TX
			points[i].Y = points[i].Y*t.SY + t.TY
		}
	default:
		for i := range points {
			p := points[i]
			points[i] = Point{p.X*t.SX + p.Y*t.KX + t.TX, p.X*t.KY + p.Y*t.SY + t.TY}
		}
	}
}

// ComputeResolutionScale is PathStroker::compute_resolution_scale.
func ComputeResolutionScale(t Transform) float32 {
	sx := Point{t.SX, t.KX}.length()
	sy := Point{t.KY, t.SY}.length()
	if isFinite(sx) && isFinite(sy) {
		if s := maxf(sx, sy); s > 0 {
			return s
		}
	}
	return 1
}

// Rect is a finite rectangle with left <= right and top <= bottom.
type Rect struct{ Left, Top, Right, Bottom float32 }

// RectFromLTRB validates and builds a rect.
func RectFromLTRB(l, t, r, b float32) (Rect, bool) {
	if !isFinite(l) || !isFinite(t) || !isFinite(r) || !isFinite(b) {
		return Rect{}, false
	}
	if !(l <= r && t <= b) {
		return Rect{}, false
	}
	if !checkedSub(r, l) || !checkedSub(b, t) {
		return Rect{}, false
	}
	return Rect{l, t, r, b}, true
}

// RectFromXYWH is RectFromLTRB(x, y, w+x, h+y).
func RectFromXYWH(x, y, w, h float32) (Rect, bool) { return RectFromLTRB(x, y, w+x, h+y) }

func checkedSub(a, b float32) bool {
	n := float64(a) - float64(b)
	return n > -math.MaxFloat32 && n < math.MaxFloat32
}

// Width is right - left.
func (r Rect) Width() float32 { return r.Right - r.Left }

// Height is bottom - top.
func (r Rect) Height() float32 { return r.Bottom - r.Top }

// rectFromPoints is Rect::from_points, including its quirk of checking
// finiteness only on the points after the first pair.
func rectFromPoints(points []Point) (Rect, bool) {
	if len(points) == 0 {
		return Rect{}, false
	}
	offset := 0
	var mn, mx [4]float32
	if len(points)&1 != 0 {
		p := points[0]
		mn = [4]float32{p.X, p.Y, p.X, p.Y}
		offset = 1
	} else {
		p0, p1 := points[0], points[1]
		mn = [4]float32{p0.X, p0.Y, p1.X, p1.Y}
		offset = 2
	}
	mx = mn
	var accum [4]float32
	for offset != len(points) {
		p0, p1 := points[offset], points[offset+1]
		xy := [4]float32{p0.X, p0.Y, p1.X, p1.Y}
		for i := range 4 {
			accum[i] *= xy[i]
			mn[i] = minf(mn[i], xy[i])
			mx[i] = maxf(mx[i], xy[i])
		}
		offset += 2
	}
	for i := range 4 {
		if accum[i]*0 != 0 {
			return Rect{}, false
		}
	}
	return RectFromLTRB(minf(mn[0], mn[2]), minf(mn[1], mn[3]), maxf(mx[0], mx[2]), maxf(mx[1], mx[3]))
}
