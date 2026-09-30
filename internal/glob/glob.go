// Package glob is the blocklist matcher: case-insensitive `*`
// wildcards, shared by the notification blocklist and the systray
// blacklist (wayle-core's glob.rs).
package glob

import "strings"

// Match reports whether name matches the wildcard pattern.
func Match(pattern, name string) bool {
	pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		idx := strings.Index(name[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	// The last segment must reach the end unless the pattern ends in *.
	if last := parts[len(parts)-1]; last != "" && !strings.HasSuffix(pattern, "*") {
		return strings.HasSuffix(name, last)
	}
	return true
}

// Wildcard is wayle-shell-core's glob::matches (the wildcard crate):
// case-sensitive, byte-wise, `*` for any run and `?` for exactly one
// byte, `\` escaping one of `*`, `?`, `\`. A malformed pattern (a
// dangling or needless escape) never matches, as the crate's
// constructor error does.
func Wildcard(pattern, text string) bool {
	type token struct {
		lit  byte
		kind byte // 0 literal, '*' any run, '?' one byte
	}
	tokens := make([]token, 0, len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '\\':
			if i+1 >= len(pattern) {
				return false
			}
			next := pattern[i+1]
			if next != '*' && next != '?' && next != '\\' {
				return false
			}
			tokens = append(tokens, token{lit: next})
			i++
		case '*', '?':
			tokens = append(tokens, token{kind: c})
		default:
			tokens = append(tokens, token{lit: c})
		}
	}
	// Greedy two-pointer match with backtracking to the last star.
	ti, pi := 0, 0
	star, mark := -1, 0
	for ti < len(text) {
		switch {
		case pi < len(tokens) && tokens[pi].kind == '*':
			star, mark = pi, ti
			pi++
		case pi < len(tokens) && (tokens[pi].kind == '?' || (tokens[pi].kind == 0 && tokens[pi].lit == text[ti])):
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(tokens) && tokens[pi].kind == '*' {
		pi++
	}
	return pi == len(tokens)
}

// FindWildcard is glob::find_match: the value of the first pattern
// that matches text, in the given order.
func FindWildcard(patterns []string, values []string, text string) (string, bool) {
	for i, pattern := range patterns {
		if Wildcard(pattern, text) {
			return values[i], true
		}
	}
	return "", false
}

// globToken is one compiled token of the glob crate's Pattern.
type globToken struct {
	kind   byte // 'c' char, '?' any char, '*' any sequence, '[' within, '!' except
	char   rune
	ranges [][2]rune
}

// compileGlob is glob::Pattern::new. A `***`, a `**` that is not a
// whole path component, or an unterminated `[` is a syntax error.
func compileGlob(pattern string) ([]globToken, bool) {
	chars := []rune(pattern)
	var tokens []globToken
	for i := 0; i < len(chars); {
		switch chars[i] {
		case '?':
			tokens = append(tokens, globToken{kind: '?'})
			i++
		case '*':
			start := i
			for i < len(chars) && chars[i] == '*' {
				i++
			}
			switch count := i - start; {
			case count > 2:
				return nil, false
			case count == 2:
				// ** must form a whole component: at the start or after
				// a separator, and at the end or before one.
				if start != 0 && chars[start-1] != '/' {
					return nil, false
				}
				switch {
				case i < len(chars) && chars[i] == '/':
					i++
				case i == len(chars):
				default:
					return nil, false
				}
				if n := len(tokens); n == 0 || tokens[n-1].kind != '*' {
					tokens = append(tokens, globToken{kind: '*'})
				}
			default:
				tokens = append(tokens, globToken{kind: '*'})
			}
		case '[':
			if i+4 <= len(chars) && chars[i+1] == '!' {
				if j := indexRune(chars[i+3:], ']'); j >= 0 {
					tokens = append(tokens, globToken{kind: '!', ranges: charSpecifiers(chars[i+2 : i+3+j])})
					i += j + 4
					continue
				}
			} else if i+3 <= len(chars) && chars[i+1] != '!' {
				if j := indexRune(chars[i+2:], ']'); j >= 0 {
					tokens = append(tokens, globToken{kind: '[', ranges: charSpecifiers(chars[i+1 : i+2+j])})
					i += j + 3
					continue
				}
			}
			return nil, false
		default:
			tokens = append(tokens, globToken{kind: 'c', char: chars[i]})
			i++
		}
	}
	return tokens, true
}

func indexRune(rs []rune, r rune) int {
	for i, c := range rs {
		if c == r {
			return i
		}
	}
	return -1
}

// charSpecifiers is parse_char_specifiers: a-b ranges and single
// characters (a single char is a one-rune range).
func charSpecifiers(s []rune) [][2]rune {
	var out [][2]rune
	for i := 0; i < len(s); {
		if i+3 <= len(s) && s[i+1] == '-' {
			out = append(out, [2]rune{s[i], s[i+2]})
			i += 3
			continue
		}
		out = append(out, [2]rune{s[i], s[i]})
		i++
	}
	return out
}

func inRanges(ranges [][2]rune, c rune) bool {
	for _, r := range ranges {
		if c >= r[0] && c <= r[1] {
			return true
		}
	}
	return false
}

// matchGlob is Pattern::matches with the default MatchOptions:
// case-sensitive, `*` crossing separators, no leading-dot rule.
func matchGlob(tokens []globToken, text []rune) bool {
	if len(tokens) == 0 {
		return len(text) == 0
	}
	t := tokens[0]
	if t.kind == '*' {
		for k := 0; k <= len(text); k++ {
			if matchGlob(tokens[1:], text[k:]) {
				return true
			}
		}
		return false
	}
	if len(text) == 0 {
		return false
	}
	c := text[0]
	var ok bool
	switch t.kind {
	case '?':
		ok = true
	case '[':
		ok = inRanges(t.ranges, c)
	case '!':
		ok = !inRanges(t.ranges, c)
	default:
		ok = c == t.char
	}
	return ok && matchGlob(tokens[1:], text[1:])
}

// Glob is the glob crate's Pattern::new(pattern).matches(text), as the
// hyprland workspace ignore list uses it: an invalid pattern never
// matches.
func Glob(pattern, text string) bool {
	tokens, ok := compileGlob(pattern)
	return ok && matchGlob(tokens, []rune(text))
}

// Fold is shell-core icons.rs's matches_glob: the text lowercased,
// equal to the (already lowercased) pattern, or matching it as a glob.
func Fold(text, pattern string) bool {
	lower := strings.ToLower(text)
	return lower == pattern || Glob(pattern, lower)
}
