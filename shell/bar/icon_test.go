package bar

import (
	"testing"
)

func TestLevelIndexFloor(t *testing.T) {
	// battery helpers.rs: floor(percent/100*n), clamped.
	for _, tc := range []struct {
		percent float64
		n       int
		want    int
	}{
		{0, 8, 0},
		{12.5, 8, 1},
		{50, 8, 4},
		{100, 8, 7},
		{100, 3, 2},
		{99.9, 3, 2},
		{0, 0, 0},
	} {
		if got := levelIndexFloor(tc.percent, tc.n); got != tc.want {
			t.Errorf("levelIndexFloor(%v, %d) = %d, want %d", tc.percent, tc.n, got, tc.want)
		}
	}
}

func TestLevelIndexSpan(t *testing.T) {
	// volume helpers.rs: 0% takes the first, then floor((p-1)/step).
	// The Rust tests pin 15% -> icons[0], 50% -> icons[1], 100% ->
	// icons[2] with n=3.
	for _, tc := range []struct {
		percent int
		n       int
		want    int
	}{
		{0, 3, 0},
		{1, 3, 0},
		{15, 3, 0},
		{33, 3, 0},
		{34, 3, 0},
		{50, 3, 1},
		{67, 3, 1},
		{68, 3, 2},
		{100, 3, 2},
		{100, 2, 1},
		{5, 0, 0},
	} {
		if got := levelIndexSpan(tc.percent, tc.n); got != tc.want {
			t.Errorf("levelIndexSpan(%d, %d) = %d, want %d", tc.percent, tc.n, got, tc.want)
		}
	}
}
