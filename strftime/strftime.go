// Package strftime formats wayle's strftime-style format strings.
// The clock and other time-bearing modules take user formats in
// strftime syntax; Go layouts are never user-facing.
package strftime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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
// through unchanged; "%%" renders a single percent sign.
func (l *Layout) Format(t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(l.src); {
		c := l.src[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(l.src) {
			b.WriteByte('%')
			break
		}
		spec := l.src[i+1]
		if spec == '%' {
			b.WriteByte('%')
			i += 2
			continue
		}
		if goLayout, ok := layouts[spec]; ok {
			b.WriteString(t.Format(goLayout))
			i += 1 + utf8.RuneLen(rune(spec))
			continue
		}
		b.WriteString(custom[spec](t))
		i += 2
	}
	return b.String()
}

// specs lists the specifier bytes in a format, in order.
func specs(format string) []byte {
	var out []byte
	for i := 0; i < len(format)-1; i++ {
		if format[i] == '%' {
			spec := format[i+1]
			if spec != '%' {
				out = append(out, spec)
			}
			i++
		}
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
