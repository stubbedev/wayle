package scss

import (
	"fmt"
	"sort"
	"strings"
)

// source is one stylesheet's text. A .css file is plain CSS: Sass
// syntax in it is an error, as in grass.
type source struct {
	path       string
	text       string
	plain      bool
	lineStarts []int
}

func newSource(path, text string) *source {
	s := &source{path: path, text: text, plain: strings.HasSuffix(path, ".css")}
	s.lineStarts = []int{0}
	for i := range len(text) {
		if text[i] == '\n' {
			s.lineStarts = append(s.lineStarts, i+1)
		}
	}
	return s
}

// line is the 1-based line of byte offset off.
func (s *source) line(off int) int {
	return sort.SearchInts(s.lineStarts, off+1)
}

// pos is where a syntax node starts; every failure is reported at one.
type pos struct {
	src *source
	off int
}

// fail aborts the compile with msg at p.
func (p pos) fail(format string, args ...any) {
	panic(&Error{File: p.src.path, Line: p.src.line(p.off), Msg: fmt.Sprintf(format, args...)})
}

// unsupported aborts the compile because what is outside the subset.
func (p pos) unsupported(what string) {
	panic(&Error{
		File:        p.src.path,
		Line:        p.src.line(p.off),
		Msg:         "unsupported SCSS feature: " + what,
		unsupported: true,
	})
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// isNameStart reports whether c can start a CSS identifier (after any
// leading hyphens).
func isNameStart(c byte) bool { return isLetter(c) || c == '_' || c >= 0x80 || c == '\\' }

// isName reports whether c can continue a CSS identifier.
func isName(c byte) bool { return isNameStart(c) || isDigit(c) || c == '-' }

// normName folds Sass's `_`/`-` equivalence in variable, mixin, and
// argument names.
func normName(name string) string { return strings.ReplaceAll(name, "_", "-") }

// collapseSpace trims s and collapses each whitespace run outside
// quoted strings to one space.
func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			b.WriteByte(c)
			switch c {
			case '\\':
				if i+1 < len(s) {
					i++
					b.WriteByte(s[i])
				}
			case quote:
				quote = 0
			}
			continue
		}
		if isSpace(c) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		if c == '"' || c == '\'' {
			quote = c
		}
		b.WriteByte(c)
	}
	return b.String()
}
