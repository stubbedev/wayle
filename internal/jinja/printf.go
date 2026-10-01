package jinja

import (
	"math"
	"strconv"
	"strings"
)

// printf is the format filter: minijinja's printf_style formatter
// (format_utils.rs). Conversions d i o x X e E f F g G s, flags
// "#0- +", width, precision, a length modifier, "%(key)s" against a
// mapping argument, and "%%".
func printf(format string, args []any) (string, error) {
	var b strings.Builder
	argIndex := 0
	for i := 0; i < len(format); {
		c := format[i]
		if c != '%' {
			j := strings.IndexByte(format[i:], '%')
			if j < 0 {
				b.WriteString(format[i:])
				break
			}
			b.WriteString(format[i : i+j])
			i += j
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i += 2
			continue
		}
		location := i
		i++
		key, hasKey := "", false
		if i < len(format) && format[i] == '(' {
			end := strings.IndexByte(format[i:], ')')
			if end < 0 {
				return "", errorf("unterminated mapping key in format spec at offset %d", location)
			}
			key, hasKey = format[i+1:i+end], true
			i += end + 1
		}
		spec, n, err := parseSpec(format[i:], location)
		if err != nil {
			return "", err
		}
		i += n
		var arg any
		switch {
		case hasKey:
			if len(args) == 0 {
				return "", errorf("missing an argument for format spec at offset '%d'", location)
			}
			m, ok := normalize(args[0]).(map[string]any)
			if !ok {
				return "", errorf("format argument must be a mapping")
			}
			v, ok := m[key]
			if !ok || isUndefined(v) {
				return "", errorf("missing an argument for format spec at offset '%d'", location)
			}
			arg = v
		case argIndex < len(args):
			arg = args[argIndex]
			argIndex++
		default:
			return "", errorf("missing an argument for format spec at offset '%d'", location)
		}
		out, err := spec.format(arg)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

type fmtSpec struct {
	left, sign, space, alt, zero bool
	width                        int // -1 when absent
	precision                    int // -1 when absent
	ty                           byte
	location                     int
}

func parseSpec(s string, location int) (fmtSpec, int, error) {
	spec := fmtSpec{width: -1, precision: -1, location: location}
	i := 0
flags:
	for i < len(s) {
		switch s[i] {
		case '#':
			spec.alt = true
		case '0':
			spec.zero = true
		case '-':
			spec.left = true
		case ' ':
			spec.space = true
		case '+':
			spec.sign = true
		default:
			break flags
		}
		i++
	}
	if spec.sign {
		spec.space = false
	}
	if spec.left {
		spec.zero = false
	}
	number := func() int {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return -1
		}
		n, _ := strconv.Atoi(s[start:i])
		return n
	}
	spec.width = number()
	if spec.zero && spec.width < 0 {
		spec.zero = false
		spec.width = 0
	}
	if i < len(s) && s[i] == '.' {
		i++
		spec.precision = number()
	}
	if i < len(s) && strings.IndexByte("hlL", s[i]) >= 0 {
		i++
	}
	if i >= len(s) {
		return spec, 0, errorf("incomplete format spec at offset %d; missing conversion type", location)
	}
	switch s[i] {
	case 'd', 'i':
		spec.ty = 'd'
	case 'e', 'E', 'f', 'F', 'g', 'G', 'o', 'x', 'X', 's':
		spec.ty = s[i]
	default:
		return spec, 0, errorf("invalid conversion type '%c' in format spec at offset %d", s[i], location)
	}
	return spec, i + 1, nil
}

// prec is the precision, 6 when absent.
func (s fmtSpec) prec() int {
	if s.precision < 0 {
		return 6
	}
	return s.precision
}

// format dispatches like FormatSpec::format: bools, then integers,
// then floats, then everything else as its display.
func (s fmtSpec) format(v any) (string, error) {
	switch x := normalize(v).(type) {
	case bool:
		return s.formatBool(x)
	case int64:
		neg := x < 0
		mag := uint64(x)
		if neg {
			mag = uint64(-(x + 1)) + 1
		}
		return s.formatInteger(mag, neg)
	case float64:
		return s.formatFloat(x)
	}
	return s.formatStr(display(v))
}

func (s fmtSpec) formatBool(v bool) (string, error) {
	asInteger := s.left || s.sign || s.alt || s.zero || s.width >= 0 || s.precision >= 0
	if s.ty == 's' {
		return s.pad(strconv.FormatBool(v), false), nil
	}
	if !asInteger && s.ty == 0 {
		return s.pad(strconv.FormatBool(v), true), nil
	}
	n := uint64(0)
	if v {
		n = 1
	}
	return s.formatInteger(n, false)
}

func (s fmtSpec) formatStr(text string) (string, error) {
	if s.ty != 's' {
		return "", errorf("invalid format spec at offset %d; 'string' cannot be formatted in %s", s.location, typeDescription(s.ty))
	}
	if s.precision >= 0 && s.precision < len(text) {
		cut := s.precision
		for cut > 0 && !utf8Start(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return s.pad(text, false), nil
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

func typeDescription(ty byte) string {
	switch ty {
	case 'd':
		return "decimal format ('d')"
	case 'o':
		return "octal format ('o')"
	case 'x':
		return "hex format ('x')"
	case 'X':
		return "hex format ('X')"
	case 'e', 'E':
		return "scientific notation ('" + string(ty) + "')"
	case 'f', 'F':
		return "fixed-point notation ('" + string(ty) + "')"
	case 'g', 'G':
		return "general format ('" + string(ty) + "')"
	}
	return "string format ('s')"
}

// mantissaExp is mantissa_and_exp: "{val:.p$e}" split at the e.
func mantissaExp(v float64, precision int) (string, int) {
	e := strconv.FormatFloat(v, 'e', precision, 64)
	i := strings.LastIndexByte(e, 'e')
	exp, _ := strconv.Atoi(e[i+1:])
	return e[:i], exp
}

func (s fmtSpec) fixDecimalPoint(num string) string {
	if s.precision == 0 && s.alt {
		num += "."
	}
	return num
}

func (s fmtSpec) removeInsignificants(num string) string {
	if !s.alt && strings.Contains(num, ".") {
		return strings.TrimRight(strings.TrimRight(num, "0"), ".")
	}
	return num
}

func expSuffix(exp int, upper bool) string {
	e := "e"
	if upper {
		e = "E"
	}
	sign := "+"
	if exp < 0 {
		sign, exp = "-", -exp
	}
	digits := strconv.Itoa(exp)
	if len(digits) < 2 {
		digits = "0" + digits
	}
	return e + sign + digits
}

// general is number_in_general_format.
func (s fmtSpec) general(v float64, isInt bool, upper bool) string {
	precision := s.prec()
	if precision == 0 {
		precision = 1
	}
	mant, exp := mantissaExp(v, precision-1)
	if exp >= -4 && exp < precision {
		places := precision - 1 - exp
		num := strconv.FormatFloat(v, 'f', places, 64)
		if isInt {
			// Rust ignores a precision on an integer's Display.
			num = strconv.FormatFloat(v, 'f', 0, 64)
		}
		return s.removeInsignificants(num)
	}
	return s.removeInsignificants(mant) + expSuffix(exp, upper)
}

func (s fmtSpec) formatInteger(v uint64, negative bool) (string, error) {
	sign := ""
	switch {
	case negative:
		sign = "-"
	case s.sign:
		sign = "+"
	case s.space:
		sign = " "
	}
	var number string
	switch s.ty {
	case 'o':
		number = strconv.FormatUint(v, 8)
	case 'x':
		number = strconv.FormatUint(v, 16)
	case 'X':
		number = strings.ToUpper(strconv.FormatUint(v, 16))
	case 'd', 0:
		number = strconv.FormatUint(v, 10)
	case 's':
		if negative {
			sign = "-"
		} else {
			sign = ""
		}
		number = strconv.FormatUint(v, 10)
	case 'e', 'E':
		mant, exp := mantissaExp(float64(v), s.prec())
		number = s.fixDecimalPoint(mant) + expSuffix(exp, s.ty == 'E')
	case 'f', 'F':
		prec := s.prec()
		number = strconv.FormatUint(v, 10)
		if prec != 0 {
			number += "." + strings.Repeat("0", prec)
		}
		number = s.fixDecimalPoint(number)
	case 'g', 'G':
		number = s.general(float64(v), true, s.ty == 'G')
	}
	return s.formatNumber(number, sign), nil
}

func (s fmtSpec) formatFloat(v float64) (string, error) {
	sign := ""
	switch {
	case math.Signbit(v):
		sign = "-"
	case s.sign && s.ty != 's':
		sign = "+"
	case s.space:
		sign = " "
	}
	upper := s.ty == 'E' || s.ty == 'F' || s.ty == 'G'
	special := func() (string, bool) {
		switch {
		case math.IsNaN(v):
			if upper {
				return s.formatNumber("NAN", ""), true
			}
			return s.formatNumber("nan", ""), true
		case math.IsInf(v, 0):
			if upper {
				return s.formatNumber("INF", sign), true
			}
			return s.formatNumber("inf", sign), true
		}
		return "", false
	}
	abs := math.Abs(v)
	switch s.ty {
	case 's', 0:
		if math.IsNaN(v) {
			return s.formatNumber("nan", ""), nil
		}
		if math.IsInf(v, 0) {
			return s.formatNumber("inf", sign), nil
		}
		if v == 0 {
			return s.formatNumber("0", sign), nil
		}
		num := s.general(abs, false, false)
		if !strings.ContainsAny(num, ".eE") {
			num += ".0"
		}
		return s.formatNumber(num, sign), nil
	case 'e', 'E':
		if out, ok := special(); ok {
			return out, nil
		}
		mant, exp := mantissaExp(abs, s.prec())
		return s.formatNumber(s.fixDecimalPoint(mant)+expSuffix(exp, upper), sign), nil
	case 'f', 'F':
		if out, ok := special(); ok {
			return out, nil
		}
		num := strconv.FormatFloat(abs, 'f', s.prec(), 64)
		return s.formatNumber(s.fixDecimalPoint(num), sign), nil
	case 'g', 'G':
		if out, ok := special(); ok {
			return out, nil
		}
		if v == 0 {
			return s.formatNumber("0", sign), nil
		}
		return s.formatNumber(s.general(abs, false, upper), sign), nil
	}
	return "", errorf("invalid format spec at offset %d; 'float' cannot be formatted in %s", s.location, typeDescription(s.ty))
}

// formatNumber applies the radix prefix and zero or space padding.
func (s fmtSpec) formatNumber(number, sign string) string {
	radix := ""
	if s.alt {
		switch s.ty {
		case 'o':
			radix = "0o"
		case 'x':
			radix = "0x"
		case 'X':
			radix = "0X"
		}
	}
	if s.zero {
		cur := len(sign) + len(radix) + len(number)
		if cur < s.width {
			return sign + radix + strings.Repeat("0", s.width-cur) + number
		}
		return sign + radix + number
	}
	return s.pad(sign+radix+number, false)
}

// pad is apply_padding: right-aligned unless "-" (or a default left
// alignment) asks for left; widths count bytes, as in Rust.
func (s fmtSpec) pad(text string, defaultLeft bool) string {
	if s.width < 0 || len(text) >= s.width {
		return text
	}
	fill := strings.Repeat(" ", s.width-len(text))
	if s.left || defaultLeft {
		return text + fill
	}
	return fill + text
}
