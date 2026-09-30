package scss

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// value is a SassScript value: number, sstring, color, list, boolean,
// nullValue, or (only inside calc()) calcValue.
type value interface{ isValue() }

// number is a Sass number with at most one unit. slash keeps the two
// operands of a `/` Sass leaves as a separator (`12px/1.5`).
type number struct {
	v     float64
	unit  string
	slash *[2]number
}

// sstring is a string; a quoted one's text is escaped for double quotes.
type sstring struct {
	text   string
	quoted bool
}

// color is an RGBA color with 0-255 channels and 0-1 alpha. text is
// the literal as written (`#FFF`, `red`), kept verbatim on output; a
// computed color has none.
type color struct {
	r, g, b, a float64
	text       string
}

type list struct {
	items     []value
	sep       byte
	bracketed bool
}

type boolean bool

type nullValue struct{}

// calcValue is an operation inside calc() that cannot be simplified
// (`100% - 10px`); it only ever prints inside a calculation.
type calcValue struct {
	op   string
	l, r value
}

func (number) isValue()    {}
func (sstring) isValue()   {}
func (color) isValue()     {}
func (list) isValue()      {}
func (boolean) isValue()   {}
func (nullValue) isValue() {}
func (calcValue) isValue() {}

// formatNumber prints a number like Sass: at most 10 decimal places,
// no trailing zeros, a leading zero, never -0.
func formatNumber(v float64) string {
	r := math.Round(v*1e10) / 1e10
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func (n number) css() string {
	if n.slash != nil {
		return n.slash[0].css() + "/" + n.slash[1].css()
	}
	return formatNumber(n.v) + n.unit
}

// css renders v as CSS output.
func css(v value) string {
	switch x := v.(type) {
	case number:
		return x.css()
	case sstring:
		if x.quoted {
			return `"` + x.text + `"`
		}
		return x.text
	case color:
		return x.css()
	case list:
		return x.css()
	case boolean:
		if x {
			return "true"
		}
		return "false"
	case nullValue:
		return ""
	case calcValue:
		return calcText(x)
	}
	return ""
}

func (c color) css() string {
	if c.text != "" {
		return c.text
	}
	r, g, b := channel(c.r), channel(c.g), channel(c.b)
	if c.a >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", r, g, b, formatNumber(c.a))
}

func channel(v float64) int {
	return int(math.Round(math.Max(0, math.Min(255, v))))
}

func (l list) css() string {
	var parts []string
	for _, item := range l.items {
		if s := css(item); s != "" {
			parts = append(parts, s)
		}
	}
	sep := " "
	switch l.sep {
	case ',':
		sep = ", "
	case '/':
		sep = "/"
	}
	s := strings.Join(parts, sep)
	if l.bracketed {
		return "[" + s + "]"
	}
	return s
}

// unquoted renders v as interpolation splices it: strings lose their
// quotes (and the escaping that implied).
func unquoted(v value) string {
	switch x := v.(type) {
	case sstring:
		if x.quoted {
			return unescapeQuoted(x.text)
		}
		return x.text
	case list:
		var parts []string
		for _, item := range x.items {
			if s := unquoted(item); s != "" {
				parts = append(parts, s)
			}
		}
		sep := " "
		switch x.sep {
		case ',':
			sep = ", "
		case '/':
			sep = "/"
		}
		s := strings.Join(parts, sep)
		if x.bracketed {
			return "[" + s + "]"
		}
		return s
	}
	return css(v)
}

// unescapeQuoted undoes the double-quote escaping of a quoted string's
// text: `\"` and `\\` become the bare characters; other escapes (such
// as an icon font's `\f101`) stay.
func unescapeQuoted(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escapeQuoted escapes text for splicing into a quoted string.
func escapeQuoted(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// truthy is Sass truthiness: everything but false and null.
func truthy(v value) bool {
	switch x := v.(type) {
	case boolean:
		return bool(x)
	case nullValue:
		return false
	}
	return true
}

// Units.

type unitInfo struct {
	group  string
	factor float64
}

// units are the convertible units, as factors of their group's base.
var units = map[string]unitInfo{
	"px": {"length", 1}, "in": {"length", 96}, "cm": {"length", 96 / 2.54},
	"mm": {"length", 96 / 25.4}, "q": {"length", 96 / 101.6}, "pt": {"length", 96.0 / 72},
	"pc":  {"length", 16},
	"deg": {"angle", 1}, "grad": {"angle", 0.9}, "rad": {"angle", 180 / math.Pi}, "turn": {"angle", 360},
	"s": {"time", 1000}, "ms": {"time", 1},
	"hz": {"frequency", 1}, "khz": {"frequency", 1000},
	"dppx": {"resolution", 1}, "dpi": {"resolution", 1.0 / 96}, "dpcm": {"resolution", 2.54 / 96},
}

// convert returns n's value in unit to, when the units are equal or
// convertible.
func convert(n number, to string) (float64, bool) {
	if n.unit == to {
		return n.v, true
	}
	from, ok1 := units[strings.ToLower(n.unit)]
	dest, ok2 := units[strings.ToLower(to)]
	if !ok1 || !ok2 || from.group != dest.group {
		return 0, false
	}
	return n.v * from.factor / dest.factor, true
}

// coerce puts a and b in one unit for addition-like operations: equal
// or convertible units convert to a's; a unitless side takes the
// other's unit unless strict (calc(), where `1 + 1px` is invalid).
func coerce(a, b number, strict bool) (av, bv float64, unit string, ok bool) {
	switch {
	case a.unit == b.unit:
		return a.v, b.v, a.unit, true
	case !strict && a.unit == "":
		return a.v, b.v, b.unit, true
	case !strict && b.unit == "":
		return a.v, b.v, a.unit, true
	}
	if bv, ok := convert(b, a.unit); ok {
		return a.v, bv, a.unit, true
	}
	return 0, 0, "", false
}

const epsilon = 1e-11

func fuzzyEq(a, b float64) bool { return math.Abs(a-b) < epsilon }

// equal is Sass's `==`.
func equal(a, b value) bool {
	switch x := a.(type) {
	case number:
		y, ok := b.(number)
		if !ok {
			return false
		}
		if x.unit == "" || y.unit == "" {
			return x.unit == y.unit && fuzzyEq(x.v, y.v)
		}
		yv, ok := convert(y, x.unit)
		return ok && fuzzyEq(x.v, yv)
	case sstring:
		y, ok := b.(sstring)
		return ok && unquoted(x) == unquoted(y)
	case color:
		y, ok := b.(color)
		return ok && channel(x.r) == channel(y.r) && channel(x.g) == channel(y.g) &&
			channel(x.b) == channel(y.b) && fuzzyEq(x.a, y.a)
	case list:
		y, ok := b.(list)
		if !ok || x.sep != y.sep && len(x.items) > 1 || x.bracketed != y.bracketed || len(x.items) != len(y.items) {
			return false
		}
		for i := range x.items {
			if !equal(x.items[i], y.items[i]) {
				return false
			}
		}
		return true
	case boolean:
		y, ok := b.(boolean)
		return ok && x == y
	case nullValue:
		_, ok := b.(nullValue)
		return ok
	}
	return false
}

// arith applies a numeric operator. strict is calc() mode: units must
// match exactly (after conversion) and no complex unit is formed; a
// false ok there means "leave symbolic". Outside calc, a non-ok result
// comes with the error message.
func arith(op string, a, b number, strict bool) (number, string, bool) {
	switch op {
	case "+", "-", "%":
		av, bv, unit, ok := coerce(a, b, strict)
		if !ok {
			return number{}, fmt.Sprintf("incompatible units %s and %s", unitName(b.unit), unitName(a.unit)), false
		}
		switch op {
		case "+":
			return number{v: av + bv, unit: unit}, "", true
		case "-":
			return number{v: av - bv, unit: unit}, "", true
		}
		if bv == 0 {
			return number{}, "modulo by zero", false
		}
		r := math.Mod(av, bv)
		if r != 0 && (r < 0) != (bv < 0) {
			r += bv
		}
		return number{v: r, unit: unit}, "", true
	case "*":
		if a.unit != "" && b.unit != "" {
			return number{}, fmt.Sprintf("%s*%s isn't a valid CSS value", a.css(), unitName(b.unit)), false
		}
		return number{v: a.v * b.v, unit: a.unit + b.unit}, "", true
	case "/":
		if b.v == 0 {
			return number{}, "division by zero", false
		}
		switch {
		case b.unit == "":
			return number{v: a.v / b.v, unit: a.unit}, "", true
		case a.unit == "":
			return number{}, fmt.Sprintf("%s/%s isn't a valid CSS value", formatNumber(a.v/b.v), b.unit), false
		}
		if bv, ok := convert(b, a.unit); ok {
			return number{v: a.v / bv}, "", true
		}
		return number{}, fmt.Sprintf("%s/%s isn't a valid CSS value", formatNumber(a.v/b.v)+a.unit, b.unit), false
	}
	return number{}, "unknown operator " + op, false
}

func unitName(u string) string {
	if u == "" {
		return "unitless"
	}
	return u
}

// compare orders two numbers for <, <=, >, >=.
func compare(a, b number) (int, bool) {
	av, bv, _, ok := coerce(a, b, false)
	if !ok {
		return 0, false
	}
	switch {
	case fuzzyEq(av, bv):
		return 0, true
	case av < bv:
		return -1, true
	}
	return 1, true
}

// calcText renders a calculation operand, parenthesizing operations
// that bind looser than their context.
func calcText(v value) string {
	c, ok := v.(calcValue)
	if !ok {
		return css(v)
	}
	if n, ok := c.r.(number); ok && n.v < 0 && (c.op == "+" || c.op == "-") {
		// `a - -1px` reads as `a + 1px`, as Sass prints it.
		c.op = map[string]string{"+": "-", "-": "+"}[c.op]
		c.r = number{v: -n.v, unit: n.unit}
	}
	l, r := calcText(c.l), calcText(c.r)
	if isSum(c.l) && (c.op == "*" || c.op == "/") {
		l = "(" + l + ")"
	}
	if lr, ok := c.r.(calcValue); ok {
		if isSum(c.r) && c.op != "+" || c.op == "/" && (lr.op == "*" || lr.op == "/") {
			r = "(" + r + ")"
		}
	}
	return l + " " + c.op + " " + r
}

func isSum(v value) bool {
	c, ok := v.(calcValue)
	return ok && (c.op == "+" || c.op == "-")
}
