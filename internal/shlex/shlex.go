// Package shlex splits and quotes command lines the way the Rust shlex
// crate (1.3) does, byte for byte: the launcher's hooks, templates, and
// `-format q` output have to agree with what the Rust shell produced.
//
// Splitting is POSIX-shell word splitting without expansion: single and
// double quotes, backslash escapes, and `#` comments at a word start.
// An unbalanced quote or a trailing backslash is an error, never half a
// command.
package shlex

import (
	"errors"
	"strings"
)

// ErrNul reports a string that cannot be quoted because it holds a NUL
// byte (shlex::QuoteError::Nul).
var ErrNul = errors.New("cannot shell-quote string containing nul byte")

// Split splits in into words, or reports false when it does not parse
// (an unbalanced quote, a dangling backslash).
func Split(in string) ([]string, bool) {
	l := lexer{in: in}
	words := []string{}
	for {
		word, ok := l.next()
		if !ok {
			break
		}
		words = append(words, word)
	}
	if l.failed {
		return nil, false
	}
	return words, true
}

// lexer walks the input once (shlex::bytes::Shlex).
type lexer struct {
	in     string
	pos    int
	failed bool
}

func (l *lexer) char() (byte, bool) {
	if l.pos >= len(l.in) {
		return 0, false
	}
	c := l.in[l.pos]
	l.pos++
	return c, true
}

// next skips blanks and comments, then reads one word.
func (l *lexer) next() (string, bool) {
	c, ok := l.char()
	if !ok {
		return "", false
	}
	for {
		switch c {
		case ' ', '\t', '\n':
		case '#':
			for {
				c2, ok := l.char()
				if !ok || c2 == '\n' {
					break
				}
			}
		default:
			return l.word(c)
		}
		if c, ok = l.char(); !ok {
			return "", false
		}
	}
}

func (l *lexer) word(c byte) (string, bool) {
	var sb strings.Builder
	for {
		switch c {
		case '"':
			if !l.double(&sb) {
				l.failed = true
				return "", false
			}
		case '\'':
			if !l.single(&sb) {
				l.failed = true
				return "", false
			}
		case '\\':
			c2, ok := l.char()
			if !ok {
				l.failed = true
				return "", false
			}
			if c2 != '\n' {
				sb.WriteByte(c2)
			}
		case ' ', '\t', '\n':
			return sb.String(), true
		default:
			sb.WriteByte(c)
		}
		var ok bool
		if c, ok = l.char(); !ok {
			return sb.String(), true
		}
	}
}

func (l *lexer) double(sb *strings.Builder) bool {
	for {
		c, ok := l.char()
		if !ok {
			return false
		}
		switch c {
		case '\\':
			c3, ok := l.char()
			if !ok {
				return false
			}
			switch c3 {
			case '$', '`', '"', '\\':
				sb.WriteByte(c3)
			case '\n':
			default:
				sb.WriteByte('\\')
				sb.WriteByte(c3)
			}
		case '"':
			return true
		default:
			sb.WriteByte(c)
		}
	}
}

func (l *lexer) single(sb *strings.Builder) bool {
	for {
		c, ok := l.char()
		if !ok {
			return false
		}
		if c == '\'' {
			return true
		}
		sb.WriteByte(c)
	}
}

// Quote returns in quoted for a POSIX shell (shlex::try_quote): bare
// when every byte is safe, otherwise in single- or double-quoted chunks.
// A NUL byte cannot be quoted and is ErrNul.
func Quote(in string) (string, error) {
	if in == "" {
		return "''", nil
	}
	if strings.IndexByte(in, 0) >= 0 {
		return "", ErrNul
	}
	var out strings.Builder
	rest := in
	for rest != "" {
		n, strategy := quotingStrategy(rest)
		if n == len(rest) && strategy == unquoted && out.Len() == 0 {
			return in, nil
		}
		appendChunk(&out, rest[:n], strategy)
		rest = rest[n:]
	}
	return out.String(), nil
}

// MustQuote is Quote with the input returned unchanged when it cannot be
// quoted - the `.unwrap_or_else(|_| text.clone())` the Rust callers use.
func MustQuote(in string) string {
	q, err := Quote(in)
	if err != nil {
		return in
	}
	return q
}

type strategy uint8

const (
	unquoted strategy = iota
	singleQuoted
	doubleQuoted
)

func unquotedOK(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return true
	}
	switch c {
	case '+', '-', '.', '/', ':', '@', ']', '_':
		return true
	}
	return false
}

func singleQuotedOK(c byte) bool { return c != '\'' && c != '^' && c != '\\' }

func doubleQuotedOK(c byte) bool { return c != '`' && c != '$' && c != '!' && c != '^' }

// quotingStrategy finds the longest prefix one strategy can quote.
func quotingStrategy(in string) (int, strategy) {
	const (
		okUnquoted = 1 << iota
		okSingle
		okDouble
	)
	prev := okSingle | okDouble | okUnquoted
	i := 0
	if in[0] == '^' {
		prev = okSingle
		i = 1
	}
	for i < len(in) {
		c := in[i]
		cur := prev
		if c >= 0x80 {
			cur &^= okUnquoted
		} else {
			if !unquotedOK(c) {
				cur &^= okUnquoted
			}
			if !singleQuotedOK(c) {
				cur &^= okSingle
			}
			if !doubleQuotedOK(c) {
				cur &^= okDouble
			}
		}
		if cur == 0 {
			break
		}
		prev = cur
		i++
	}
	switch {
	case prev&okUnquoted != 0:
		return i, unquoted
	case prev&okSingle != 0:
		return i, singleQuoted
	default:
		return i, doubleQuoted
	}
}

func appendChunk(out *strings.Builder, chunk string, s strategy) {
	switch s {
	case unquoted:
		out.WriteString(chunk)
	case singleQuoted:
		out.WriteByte('\'')
		out.WriteString(chunk)
		out.WriteByte('\'')
	case doubleQuoted:
		out.WriteByte('"')
		for i := range len(chunk) {
			switch chunk[i] {
			case '$', '`', '"', '\\':
				out.WriteByte('\\')
			}
			out.WriteByte(chunk[i])
		}
		out.WriteByte('"')
	}
}
