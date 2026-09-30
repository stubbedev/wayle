// Package strftime formats wayle's strftime-style format strings.
// The clock and other time-bearing modules take user formats in
// strftime syntax; Go layouts are never user-facing.
package strftime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Layout is a compiled strftime format: validated once at load, then
// formatted per tick without re-parsing.
type Layout struct {
	src string
}

// Compile validates a strftime format. Unsupported or dangling
// specifiers are errors — a bad format fails at load instead of
// rendering garbage at runtime.
func Compile(format string) (*Layout, error) {
	for _, spec := range specs(format) {
		_, inLayouts := layouts[spec]
		_, inCustom := custom[spec]
		if !inLayouts && !inCustom {
			return nil, fmt.Errorf("strftime: unsupported specifier %q in %q", "%"+string(spec), format)
		}
	}
	return &Layout{src: format}, nil
}

// String returns the source format.
func (l *Layout) String() string { return l.src }

// Format renders t through the compiled format. Literal text passes
// through unchanged; "%%" renders a single percent sign. A chrono
// padding flag between the percent and the specifier re-pads numeric
// fields: "-" drops the padding (%-d is "5"), "_" pads with spaces,
// "0" with zeros; on text fields the flag changes nothing, as in
// chrono.
func (l *Layout) Format(t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(l.src); {
		c := l.src[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		flag, spec, n := directive(l.src[i:])
		if n == 0 {
			// A dangling percent (or percent-flag) at the end.
			b.WriteString(l.src[i:])
			break
		}
		i += n
		if spec == '%' {
			b.WriteByte('%')
			continue
		}
		b.WriteString(pad(render(spec, t), spec, flag))
	}
	return b.String()
}

// render formats one validated specifier.
func render(spec byte, t time.Time) string {
	if goLayout, ok := layouts[spec]; ok {
		return t.Format(goLayout)
	}
	return custom[spec](t)
}

// directive splits "%[flag]spec" at the head of s: the flag byte (0
// when absent), the specifier, and the bytes consumed (0 when s ends
// before a specifier).
func directive(s string) (flag, spec byte, n int) {
	i := 1
	if i < len(s) && isFlag(s[i]) {
		flag = s[i]
		i++
	}
	if i >= len(s) {
		return 0, 0, 0
	}
	return flag, s[i], i + 1
}

// isFlag reports whether c is a chrono padding flag.
func isFlag(c byte) bool { return c == '-' || c == '_' || c == '0' }

// numericWidth is the padded width of the numeric specifiers the
// padding flags act on.
var numericWidth = map[byte]int{
	'd': 2, 'e': 2, 'm': 2, 'y': 2,
	'H': 2, 'I': 2, 'M': 2, 'S': 2,
	'j': 3,
}

// pad applies a padding flag to one rendered field.
func pad(field string, spec, flag byte) string {
	width, numeric := numericWidth[spec]
	if flag == 0 || !numeric {
		return field
	}
	digits := strings.TrimLeft(field, "0 ")
	if digits == "" {
		digits = "0"
	}
	switch flag {
	case '-':
		return digits
	case '_':
		return strings.Repeat(" ", max(0, width-len(digits))) + digits
	default: // '0'
		return strings.Repeat("0", max(0, width-len(digits))) + digits
	}
}

// specs lists the specifier bytes in a format, in order; padding flags
// are skipped.
func specs(format string) []byte {
	var out []byte
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		_, spec, n := directive(format[i:])
		if n == 0 {
			break
		}
		if spec != '%' {
			out = append(out, spec)
		}
		i += n
	}
	return out
}

// layouts maps specifiers with a direct Go time layout. Everything
// without one lives in custom.
var layouts = map[byte]string{
	'Y': "2006",
	'y': "06",
	'm': "01",
	'd': "02",
	'e': "_2",
	'a': "Mon",
	'A': "Monday",
	'b': "Jan",
	'B': "January",
	'H': "15",
	'I': "03",
	'M': "04",
	'S': "05",
	'p': "PM",
	'Z': "MST",
	'z': "-0700",
}

// custom maps specifiers needing arithmetic beyond a Go layout.
var custom = map[byte]func(time.Time) string{
	'j': func(t time.Time) string { return fmt.Sprintf("%03d", t.YearDay()) },
	'Z': func(t time.Time) string { return t.Format("MST") },
	'u': func(t time.Time) string {
		if wd := int(t.Weekday()); wd == 0 {
			return "7"
		} else {
			return strconv.Itoa(wd)
		}
	},
	'w': func(t time.Time) string { return strconv.Itoa(int(t.Weekday())) },
	'F': func(t time.Time) string { return t.Format("2006-01-02") },
	'T': func(t time.Time) string { return t.Format("15:04:05") },
	's': func(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) },
}
