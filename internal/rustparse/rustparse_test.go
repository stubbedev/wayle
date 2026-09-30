package rustparse

import (
	"errors"
	"math"
	"testing"
)

func TestInt(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "42": 42, "+7": 7, "-3": -3, "-2147483648": math.MinInt32} {
		if got, err := Int(in, 32); err != nil || got != want {
			t.Errorf("%q -> %d %v", in, got, err)
		}
	}
	for in, want := range map[string]error{
		"":            ErrIntEmpty,
		"abc":         ErrIntDigit,
		"-":           ErrIntDigit,
		"+-1":         ErrIntDigit,
		"1_000":       ErrIntDigit,
		" 1":          ErrIntDigit,
		"0x10":        ErrIntDigit,
		"2147483648":  ErrIntHigh,
		"-2147483649": ErrIntLow,
	} {
		if _, err := Int(in, 32); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", in, err, want)
		}
	}
}

func TestUint(t *testing.T) {
	if got, err := Uint("+5", 32); err != nil || got != 5 {
		t.Errorf("+5 -> %d %v", got, err)
	}
	if got, err := Uint("4294967295", 32); err != nil || got != math.MaxUint32 {
		t.Errorf("max -> %d %v", got, err)
	}
	for in, want := range map[string]error{
		"-1":         ErrIntDigit,
		"4294967296": ErrIntHigh,
		"":           ErrIntEmpty,
		"1.5":        ErrIntDigit,
	} {
		if _, err := Uint(in, 32); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", in, err, want)
		}
	}
}

func TestFloat(t *testing.T) {
	for in, want := range map[string]float64{"1.5": 1.5, "+2": 2, ".5": 0.5, "3.": 3, "1e3": 1000, "-0": 0} {
		if got, err := Float(in); err != nil || got != want {
			t.Errorf("%q -> %v %v", in, got, err)
		}
	}
	if got, err := Float("inf"); err != nil || !math.IsInf(got, 1) {
		t.Errorf("inf -> %v %v", got, err)
	}
	if got, err := Float("1e999"); err != nil || !math.IsInf(got, 1) {
		t.Errorf("overflow saturates like Rust: %v %v", got, err)
	}
	for in, want := range map[string]error{
		"":      ErrFloatEmpty,
		"abc":   ErrFloat,
		"0x10":  ErrFloat,
		"1_0":   ErrFloat,
		".":     ErrFloat,
		"1e":    ErrFloat,
		" 1.0":  ErrFloat,
		"0x1p3": ErrFloat,
	} {
		if _, err := Float(in); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", in, err, want)
		}
	}
}
