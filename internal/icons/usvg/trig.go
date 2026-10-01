package usvg

import (
	"math"
	"math/big"
)

// The Rust code's f64 sin, cos, tan and atan2 come from the system
// libm, which returns the correctly rounded result for the arguments
// arcs produce; Go's math package can be an ulp off, enough to flip the
// sign of a coordinate that should be zero ("-0.00" vs "0.00" in the
// emitted icon). These evaluate in 320-bit floats and round once.

const trigPrec = 320

func bf() *big.Float { return new(big.Float).SetPrec(trigPrec) }

var (
	bigPi = func() *big.Float {
		pi, _, err := big.ParseFloat("3.14159265358979323846264338327950288419716939937510582097494459230781640628620899862803482534211706798214808651328230664709384460955058223172535940812848111745028410270193852110555964462294895493038196", 10, trigPrec, big.ToNearestEven)
		if err != nil {
			panic(err)
		}
		return pi
	}()
	bigTwoPi  = bf().Mul(bigPi, big.NewFloat(2))
	bigHalfPi = bf().Quo(bigPi, big.NewFloat(2))
	bigOne    = bf().SetInt64(1)
	bigEps    = bf().SetMantExp(big.NewFloat(1), -trigPrec)
)

func special(x float64) bool { return x == 0 || math.IsNaN(x) || math.IsInf(x, 0) }

// bigSincos evaluates sin and cos of x reduced into [-pi, pi].
func bigSincos(x float64) (*big.Float, *big.Float) {
	r := bf().SetFloat64(x)
	if k := math.Round(x / (2 * math.Pi)); k != 0 {
		r.Sub(r, bf().Mul(bigTwoPi, big.NewFloat(k)))
	}
	x2 := bf().Mul(r, r)
	sin, cos := bf().Set(r), bf().SetInt64(1)
	termS, termC := bf().Set(r), bf().SetInt64(1)
	for n := int64(1); n < 200; n++ {
		termS.Mul(termS, x2).Quo(termS, bf().SetInt64(-(2*n)*(2*n+1)))
		termC.Mul(termC, x2).Quo(termC, bf().SetInt64(-(2*n-1)*(2*n)))
		sin.Add(sin, termS)
		cos.Add(cos, termC)
		if bf().Abs(termS).Cmp(bigEps) < 0 && bf().Abs(termC).Cmp(bigEps) < 0 {
			break
		}
	}
	return sin, cos
}

// sincos returns the correctly rounded sin and cos of x.
func sincos(x float64) (float64, float64) {
	if special(x) {
		return math.Sincos(x)
	}
	s, c := bigSincos(x)
	sf, _ := s.Float64()
	cf, _ := c.Float64()
	return sf, cf
}

// tanR returns the correctly rounded tan of x.
func tanR(x float64) float64 {
	if special(x) {
		return math.Tan(x)
	}
	s, c := bigSincos(x)
	t, _ := bf().Quo(s, c).Float64()
	return t
}

// atan2R returns the correctly rounded atan2(y, x).
func atan2R(y, x float64) float64 {
	if y == 0 || x == 0 || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return math.Atan2(y, x)
	}
	by, bx := bf().SetFloat64(y), bf().SetFloat64(x)
	var a *big.Float
	if math.Abs(y) <= math.Abs(x) {
		a = bigAtan(bf().Quo(by, bx))
		switch {
		case x < 0 && y > 0:
			a.Add(a, bigPi)
		case x < 0:
			a.Sub(a, bigPi)
		}
	} else {
		half := bf().Set(bigHalfPi)
		if y < 0 {
			half.Neg(half)
		}
		a = bf().Sub(half, bigAtan(bf().Quo(bx, by)))
	}
	f, _ := a.Float64()
	return f
}

// bigAtan is atan for |t| <= 1: three argument halvings
// (atan t = 2 atan(t / (1 + sqrt(1 + t^2)))), then the series.
func bigAtan(t *big.Float) *big.Float {
	x := bf().Set(t)
	for range 3 {
		s := bf().Mul(x, x)
		s.Add(s, bigOne).Sqrt(s).Add(s, bigOne)
		x.Quo(x, s)
	}
	x2 := bf().Mul(x, x)
	sum, term := bf().Set(x), bf().Set(x)
	for n := int64(1); n < 200; n++ {
		term.Mul(term, x2).Neg(term)
		step := bf().Quo(term, bf().SetInt64(2*n+1))
		sum.Add(sum, step)
		if bf().Abs(step).Cmp(bigEps) < 0 {
			break
		}
	}
	return sum.Mul(sum, bf().SetInt64(8))
}
