package jinja

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind int

const (
	tText tokKind = iota
	tVarStart
	tVarEnd
	tBlockStart
	tBlockEnd
	tIdent
	tString
	tInt
	tFloat
	tOp
	tEOF
)

type token struct {
	kind tokKind
	text string // identifier, operator, raw text, or the decoded string
	num  int64
	flt  float64
	pos  int
}

// lex splits source into text and tag tokens. Whitespace control is
// resolved here: a "-" at a tag's inner edge trims the neighboring
// text's whitespace. The source loses one trailing newline
// (keep_trailing_newline off).
func lex(src string) ([]token, error) {
	if strings.HasSuffix(src, "\r\n") {
		src = src[:len(src)-2]
	} else if strings.HasSuffix(src, "\n") {
		src = src[:len(src)-1]
	}
	var toks []token
	trimNext := false
	pos := 0
	emitText := func(text string, trimRight bool, at int) {
		if trimNext {
			text = strings.TrimLeftFunc(text, unicode.IsSpace)
			trimNext = false
		}
		if trimRight {
			text = strings.TrimRightFunc(text, unicode.IsSpace)
		}
		if text != "" {
			toks = append(toks, token{kind: tText, text: text, pos: at})
		}
	}
	for pos < len(src) {
		i := nextOpener(src[pos:])
		if i < 0 {
			emitText(src[pos:], false, pos)
			break
		}
		start := pos + i
		opener := src[start : start+2]
		trimLeft := start+2 < len(src) && src[start+2] == '-'
		emitText(src[pos:start], trimLeft, pos)
		inner := start + 2
		if trimLeft || (start+2 < len(src) && src[start+2] == '+') {
			inner++
		}
		switch opener {
		case "{#":
			end := strings.Index(src[inner:], "#}")
			if end < 0 {
				return nil, errorf("unexpected end of comment")
			}
			body := src[inner : inner+end]
			trimNext = strings.HasSuffix(body, "-")
			pos = inner + end + 2
			continue
		case "{%":
			if raw, after, ok, err := lexRaw(src, inner); ok || err != nil {
				if err != nil {
					return nil, err
				}
				toks = append(toks, token{kind: tText, text: raw.text, pos: inner})
				trimNext = raw.trimAfter
				pos = after
				continue
			}
		}
		closer, startKind, endKind := "}}", tVarStart, tVarEnd
		if opener == "{%" {
			closer, startKind, endKind = "%}", tBlockStart, tBlockEnd
		}
		toks = append(toks, token{kind: startKind, pos: start})
		p, err := lexExpr(src, inner, closer, &toks)
		if err != nil {
			return nil, err
		}
		trimNext = p > 0 && src[p-1] == '-'
		toks = append(toks, token{kind: endKind, pos: p})
		pos = p + 2
	}
	toks = append(toks, token{kind: tEOF, pos: len(src)})
	return toks, nil
}

type rawBlock struct {
	text      string
	trimAfter bool
}

// lexRaw reads a {% raw %}...{% endraw %} block starting after "{%".
func lexRaw(src string, inner int) (rawBlock, int, bool, error) {
	rest := strings.TrimLeft(src[inner:], " \t\r\n")
	if !strings.HasPrefix(rest, "raw") {
		return rawBlock{}, 0, false, nil
	}
	after := strings.TrimLeft(rest[3:], " \t\r\n")
	trimInner := strings.HasPrefix(after, "-%}")
	if !trimInner && !strings.HasPrefix(after, "%}") {
		return rawBlock{}, 0, false, nil
	}
	bodyStart := len(src) - len(after) + 2
	if trimInner {
		bodyStart++
	}
	idx := bodyStart
	for {
		k := strings.Index(src[idx:], "{%")
		if k < 0 {
			return rawBlock{}, 0, false, errorf("unexpected end of raw block")
		}
		tag := idx + k
		p := tag + 2
		trimBefore := p < len(src) && src[p] == '-'
		if trimBefore {
			p++
		}
		body := strings.TrimLeft(src[p:], " \t\r\n")
		if strings.HasPrefix(body, "endraw") {
			tail := strings.TrimLeft(body[6:], " \t\r\n")
			trimAfter := strings.HasPrefix(tail, "-%}")
			if trimAfter || strings.HasPrefix(tail, "%}") {
				text := src[bodyStart:tag]
				if trimInner {
					text = strings.TrimLeftFunc(text, unicode.IsSpace)
				}
				if trimBefore {
					text = strings.TrimRightFunc(text, unicode.IsSpace)
				}
				end := len(src) - len(tail) + 2
				if trimAfter {
					end++
				}
				return rawBlock{text: text, trimAfter: trimAfter}, end, true, nil
			}
		}
		idx = tag + 2
	}
}

var operators = []string{"**", "//", "==", "!=", "<=", ">=", "+", "-", "*", "/", "%", "~", "(", ")", "[", "]", "{", "}", ",", ".", ":", "|", "=", "<", ">"}

// lexExpr tokenizes a tag's inside up to closer (optionally preceded by
// "-") and returns the closer's offset.
func lexExpr(src string, p int, closer string, toks *[]token) (int, error) {
	for {
		for p < len(src) && strings.ContainsRune(" \t\r\n", rune(src[p])) {
			p++
		}
		if p >= len(src) {
			return 0, errorf("unexpected end of template, expected %q", closer)
		}
		if strings.HasPrefix(src[p:], closer) {
			return p, nil
		}
		if src[p] == '-' && strings.HasPrefix(src[p+1:], closer) {
			return p + 1, nil
		}
		c := src[p]
		switch {
		case c == '"' || c == '\'':
			s, n, err := lexString(src[p:])
			if err != nil {
				return 0, err
			}
			*toks = append(*toks, token{kind: tString, text: s, pos: p})
			p += n
		case c >= '0' && c <= '9':
			tok, n, err := lexNumber(src[p:])
			if err != nil {
				return 0, err
			}
			tok.pos = p
			*toks = append(*toks, tok)
			p += n
		case c == '_' || unicode.IsLetter(rune(c)) || c >= utf8.RuneSelf:
			q := p
			for q < len(src) {
				r, size := utf8.DecodeRuneInString(src[q:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				q += size
			}
			if q == p {
				return 0, errorf("unexpected character %q", src[p])
			}
			*toks = append(*toks, token{kind: tIdent, text: src[p:q], pos: p})
			p = q
		default:
			matched := false
			for _, op := range operators {
				if strings.HasPrefix(src[p:], op) {
					*toks = append(*toks, token{kind: tOp, text: op, pos: p})
					p += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return 0, errorf("unexpected character %q", src[p])
			}
		}
	}
}

// lexString decodes a quoted string literal with backslash escapes.
func lexString(s string) (string, int, error) {
	quote := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote:
			return b.String(), i + 1, nil
		case c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case '\\', '"', '\'', '/':
				b.WriteByte(s[i])
			case 'u':
				if i+4 >= len(s) {
					return "", 0, errorf("bad string escape")
				}
				n, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return "", 0, errorf("bad string escape")
				}
				b.WriteRune(rune(n))
				i += 4
			default:
				return "", 0, errorf("bad string escape \\%c", s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errorf("unexpected end of string")
}

// lexNumber reads an integer or float literal ("_" separators allowed).
func lexNumber(s string) (token, int, error) {
	i, isFloat := 0, false
	digits := func() {
		for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
			i++
		}
	}
	digits()
	if i+1 < len(s) && s[i] == '.' && s[i+1] >= '0' && s[i+1] <= '9' {
		isFloat = true
		i++
		digits()
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && s[j] >= '0' && s[j] <= '9' {
			isFloat = true
			i = j
			digits()
		}
	}
	text := strings.ReplaceAll(s[:i], "_", "")
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return token{}, 0, errorf("invalid float %q", text)
		}
		return token{kind: tFloat, flt: f}, i, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(text, 64)
		if ferr != nil {
			return token{}, 0, errorf("invalid integer %q", text)
		}
		return token{kind: tFloat, flt: f}, i, nil
	}
	return token{kind: tInt, num: n}, i, nil
}

// nextOpener is the offset of the first "{{", "{%", or "{#" in s, -1
// without one.
func nextOpener(s string) int {
	best := -1
	for _, o := range []string{"{{", "{%", "{#"} {
		if i := strings.Index(s, o); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}
