package usvg

import (
	"math"
	"testing"
)

func TestApproxEqUlps(t *testing.T) {
	one := float32(1)
	next := func(f float32, n int) float32 {
		return math.Float32frombits(uint32(int32(math.Float32bits(f)) + int32(n)))
	}
	for _, tc := range []struct {
		name string
		a, b float32
		want bool
	}{
		{"equal", one, one, true},
		{"4 ulps apart", one, next(one, 4), true},
		{"4 ulps below", next(one, -4), one, true},
		{"5 ulps apart", one, next(one, 5), false},
		{"signed zeros are equal", float32(math.Copysign(0, -1)), 0, true},
		{"the smallest negative is not zero", -math.SmallestNonzeroFloat32, 0, false},
		{"the smallest positive is zero", math.SmallestNonzeroFloat32, 0, true},
		// float_cmp compares bit patterns: one NaN is 0 ulps from itself.
		{"identical NaNs", float32(math.NaN()), float32(math.NaN()), true},
		{"NaN is not a number", float32(math.NaN()), one, false},
		// -0 and the largest NaN pattern are 1 apart once the bits wrap.
		{"opposite signs never meet across the wrap", float32(math.Copysign(0, -1)), math.Float32frombits(0x7fffffff), false},
	} {
		if got := approxEqUlps(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: approxEqUlps(%v, %v) = %v, want %v", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
	if approxZeroUlps(-math.SmallestNonzeroFloat32) {
		t.Error("approxZeroUlps: a tiny negative counts as zero")
	}
}

func TestParsePaintOrder(t *testing.T) {
	F, S, M := orderFill, orderStroke, orderMarkers
	for text, want := range map[string][3]paintOrderKind{
		"":                    {F, S, M},
		"normal":              {F, S, M},
		"stroke":              {S, F, M},
		"markers":             {M, F, S},
		"fill markers":        {F, M, S},
		" stroke  markers ":   {S, M, F},
		"markers stroke fill": {M, S, F},
		"fill fill":           {F, S, M}, // a duplicate is the default
		"stroke bogus":        {F, S, M}, // so is an unknown name
		"stroke, fill":        {F, S, M}, // and trailing data
		"stroke normal":       {F, S, M},
	} {
		if got := parsePaintOrder(text); got != want {
			t.Errorf("parsePaintOrder(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestParseAngleStr(t *testing.T) {
	for text, want := range map[string]float64{
		"45": 45, "45deg": 45, "100grad": 90, "0.5turn": 180, " -30": -30,
		"3.141592653589793rad": 3.141592653589793 * (180 / math.Pi),
	} {
		if got, ok := parseAngleStr(text); !ok || math.Abs(got-want) > 1e-12 {
			t.Errorf("parseAngleStr(%q) = %v %v, want %v", text, got, ok, want)
		}
	}
	for _, text := range []string{"", "auto", "45 deg", "45degx", "deg"} {
		if got, ok := parseAngleStr(text); ok {
			t.Errorf("parseAngleStr(%q) = %v, want an error", text, got)
		}
	}
}
