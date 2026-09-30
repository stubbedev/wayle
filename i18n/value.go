package i18n

import (
	"math"
	"strconv"
	"strings"
)

// valueKind is the closed set of fluent-bundle FluentValue variants the
// resolver produces (types/mod.rs); Custom has no FTL source.
type valueKind int

const (
	valueNone valueKind = iota
	valueString
	valueNumber
	valueError
)

// value is a FluentValue: a string, a number with its formatting
// options, or the error/none markers that write nothing.
type value struct {
	kind valueKind
	str  string
	num  number
}

// number is a FluentNumber with the one option fluent-bundle honors
// when formatting and matching: the minimum fraction digits, which an
// FTL number literal takes from its source spelling ("1.50" -> 2).
type number struct {
	v float64
	// minFD is the minimum fraction digit count, or -1 when unset (a
	// number passed from code).
	minFD int
}

func stringValue(s string) value  { return value{kind: valueString, str: s} }
func numberValue(n number) value  { return value{kind: valueNumber, num: n} }
func codeNumber(v float64) number { return number{v: v, minFD: -1} }

// tryNumber is FluentValue::try_number: a parseable number keeps its
// fraction digit count, anything else stays a string.
func tryNumber(raw string) value {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return stringValue(raw)
	}
	mfd := -1
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		mfd = len(raw) - dot - 1
	}
	return numberValue(number{v: f, minFD: mfd})
}

// rustFloat formats f64 the way Rust's Display does: the shortest
// round-tripping digits, never an exponent, "inf"/"NaN" for the
// non-finite values.
func rustFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// String is FluentNumber::as_string: the plain digits, padded with
// zeros up to the minimum fraction digits.
func (n number) String() string {
	s := rustFloat(n.v)
	if n.minFD < 0 {
		return s
	}
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		if missing := n.minFD - (len(s) - dot - 1); missing > 0 {
			s += strings.Repeat("0", missing)
		}
		return s
	}
	return s + "." + strings.Repeat("0", n.minFD)
}

// operands are the CLDR plural operands fluent-bundle derives from a
// number: i (integer digits) and v (visible fraction digit count) are
// all the carried rules read.
type operands struct {
	i uint64
	v int
}

// pluralOperands ports intl_pluralrules' TryFrom<&str> over the Rust
// Display string plus fluent-bundle's minimum-fraction-digits bump. It
// fails where Rust's does (an unparsable integer part).
func (n number) pluralOperands() (operands, bool) {
	s := strings.TrimPrefix(rustFloat(n.v), "-")
	var ops operands
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		i, err := strconv.ParseUint(s[:dot], 10, 64)
		if err != nil {
			return operands{}, false
		}
		ops = operands{i: i, v: len(s) - dot - 1}
	} else {
		abs, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return operands{}, false
		}
		ops = operands{i: saturatingUint(abs)}
	}
	if n.minFD > ops.v {
		ops.v = n.minFD
	}
	return ops, true
}

// saturatingUint is Rust's `f64 as u64`: NaN to 0, out of range
// clamped.
func saturatingUint(f float64) uint64 {
	switch {
	case math.IsNaN(f) || f <= 0:
		return 0
	case f >= math.MaxUint64:
		return math.MaxUint64
	}
	return uint64(f)
}

// write renders the value (FluentValue::write): errors and none write
// nothing.
func (v value) write(b *strings.Builder) {
	switch v.kind {
	case valueString:
		b.WriteString(v.str)
	case valueNumber:
		b.WriteString(v.num.String())
	case valueNone, valueError:
	}
}

// unescape ports fluent-syntax's unescape_unicode: \\ and \" map to
// themselves, \uXXXX and \UXXXXXX to the code point (U+FFFD when
// invalid), and any other escape, \{ included, to U+FFFD.
func unescape(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(raw) {
			b.WriteRune('\uFFFD')
			break
		}
		switch raw[i] {
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case 'u', 'U':
			length := 4
			if raw[i] == 'U' {
				length = 6
			}
			r := '\uFFFD'
			if i+1+length <= len(raw) {
				if cp, err := strconv.ParseUint(raw[i+1:i+1+length], 16, 32); err == nil && validRune(cp) {
					r = rune(cp)
				}
			}
			i += length
			b.WriteRune(r)
		default:
			b.WriteRune('\uFFFD')
		}
	}
	return b.String()
}

func validRune(cp uint64) bool {
	return cp <= 0x10FFFF && (cp < 0xD800 || cp > 0xDFFF)
}
