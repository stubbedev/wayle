package openconnect

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A reader for the handful of tiny XML documents the gateways speak
// (xml.rs). These responses are a flat <jnlp> of <argument> elements,
// a <challenge> of two fields, or a one-line error: pulling the text
// out of named elements is the entire requirement, so that is all this
// does. encoding/xml is deliberately not used: it refuses the
// not-quite-XML real gateways send (HTML wrappers, bare ampersands).
//
// Element names are matched ASCII-case-insensitively: openconnect
// compares them that way because real gateways send inputStr where the
// protocol notes say inputstr.

// xmlValues is the text of every element with this name, in document
// order. A self-closing or empty element contributes "" rather than
// being skipped: an <argument/> is a positional slot, and dropping it
// would shift every argument after it onto the wrong meaning.
func xmlValues(doc, tag string) []string {
	var found []string
	rest := doc
	for {
		body, remainder, ok := nextElement(rest, tag)
		if !ok {
			return found
		}
		found = append(found, decodeEntities(body))
		rest = remainder
	}
}

// xmlValue is the text of the first element with this name.
func xmlValue(doc, tag string) (string, bool) {
	body, _, ok := nextElement(doc, tag)
	if !ok {
		return "", false
	}
	return decodeEntities(body), true
}

// xmlNonEmpty is xmlValue with an empty element read as absent, the
// `.filter(|v| !v.is_empty())` the Rust applies at most call sites.
func xmlNonEmpty(doc, tag string) (string, bool) { return nonEmpty(xmlValue(doc, tag)) }

// nonEmpty filters a (value, ok) lookup to non-empty values.
func nonEmpty(value string, ok bool) (string, bool) { return value, ok && value != "" }

// rawElements is the whole of every <tag …>…</tag>, markup and
// attributes included. AnyConnect needs this: its forms are described by
// the attributes of <input> elements, and its <opaque> blob has to be
// echoed back to the gateway byte for byte, children and all.
func rawElements(doc, tag string) []string {
	var found []string
	rest := doc
	for {
		element, remainder, ok := nextRawElement(rest, tag)
		if !ok {
			return found
		}
		found = append(found, element)
		rest = remainder
	}
}

// rawElement is the whole of the first <tag …>…</tag>.
func rawElement(doc, tag string) (string, bool) {
	element, _, ok := nextRawElement(doc, tag)
	return element, ok
}

// xmlAttribute is the value of an attribute on an element's opening tag.
// Only the forms a gateway actually writes — name="value" and
// name='value'. An attribute with no quotes is not XML.
func xmlAttribute(element, name string) (string, bool) {
	rest, _, ok := strings.Cut(element, ">")
	if !ok {
		return "", false
	}
	for {
		at := strings.Index(rest, name)
		if at < 0 {
			return "", false
		}
		before, _ := utf8.DecodeLastRuneInString(rest[:at])
		after := rest[at+len(name):]
		afterTrimmed := strings.TrimLeftFunc(after, unicode.IsSpace)
		// name= and not some-other-name=: the character before has to be
		// whitespace, and the next non-space one an equals sign.
		if (at == 0 || unicode.IsSpace(before)) && strings.HasPrefix(afterTrimmed, "=") {
			value := strings.TrimLeftFunc(afterTrimmed[1:], unicode.IsSpace)
			if value == "" {
				return "", false
			}
			quote := value[0]
			if quote != '"' && quote != '\'' {
				return "", false
			}
			end := strings.IndexByte(value[1:], quote)
			if end < 0 {
				return "", false
			}
			return decodeEntities(value[1 : end+1]), true
		}
		rest = after
	}
}

// nextRawElement finds the next <tag …>…</tag>, returning the whole
// element and what follows it.
func nextRawElement(doc, tag string) (element, rest string, ok bool) {
	start, ok := elementStart(doc, tag)
	if !ok {
		return "", "", false
	}
	_, rest, ok = nextElement(doc[start:], tag)
	if !ok {
		return "", "", false
	}
	return doc[start : len(doc)-len(rest)], rest, true
}

// elementName reads the tag name that starts at doc[after:], returning
// its end offset.
func elementName(doc string, after int) (int, bool) {
	offset := strings.IndexFunc(doc[after:], func(c rune) bool {
		return c == '>' || c == '/' || isASCIISpace(c)
	})
	if offset < 0 {
		return 0, false
	}
	return after + offset, true
}

// elementStart is the offset of the < that opens the next element with
// this name.
func elementStart(doc, tag string) (int, bool) {
	cursor := 0
	for {
		open := strings.IndexByte(doc[cursor:], '<')
		if open < 0 {
			return 0, false
		}
		open += cursor
		after := open + 1
		nameEnd, ok := elementName(doc, after)
		if !ok {
			return 0, false
		}
		if asciiEqualFold(doc[after:nameEnd], tag) {
			return open, true
		}
		cursor = after
	}
}

// nextElement finds the next <tag …>…</tag>, returning its raw body and
// what follows.
func nextElement(doc, tag string) (body, rest string, ok bool) {
	cursor := 0
	for {
		open := strings.IndexByte(doc[cursor:], '<')
		if open < 0 {
			return "", "", false
		}
		open += cursor
		after := open + 1
		nameEnd, ok := elementName(doc, after)
		if !ok {
			return "", "", false
		}
		name := doc[after:nameEnd]
		if !asciiEqualFold(name, tag) {
			cursor = after
			continue
		}

		tagEnd := strings.IndexByte(doc[nameEnd:], '>')
		if tagEnd < 0 {
			return "", "", false
		}
		tagEnd += nameEnd
		// <argument/> — an empty positional slot, not an absent one.
		if strings.HasSuffix(doc[:tagEnd], "/") {
			return "", doc[tagEnd+1:], true
		}

		bodyStart := tagEnd + 1
		closing := "</" + name + ">"
		bodyEnd := strings.Index(asciiLower(doc[bodyStart:]), asciiLower(closing))
		if bodyEnd < 0 {
			return "", "", false
		}
		bodyEnd += bodyStart
		return doc[bodyStart:bodyEnd], doc[bodyEnd+len(closing):], true
	}
}

// decodeEntities resolves the XML entities a gateway actually emits. A
// 2FA prompt is free text written by an administrator, so it is the one
// field that reliably contains them; a bare & survives as written.
func decodeEntities(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "&") {
		return raw
	}

	var out strings.Builder
	out.Grow(len(raw))
	rest := raw
	for {
		start := strings.IndexByte(rest, '&')
		if start < 0 {
			break
		}
		out.WriteString(rest[:start])
		tail := rest[start:]
		end := strings.IndexByte(tail, ';')
		if end < 0 || end > 10 {
			out.WriteByte('&')
			rest = tail[1:]
			continue
		}
		switch entity := tail[1:end]; entity {
		case "amp":
			out.WriteByte('&')
		case "lt":
			out.WriteByte('<')
		case "gt":
			out.WriteByte('>')
		case "quot":
			out.WriteByte('"')
		case "apos":
			out.WriteByte('\'')
		default:
			character, ok := numericEntity(entity)
			if !ok {
				out.WriteByte('&')
				rest = tail[1:]
				continue
			}
			out.WriteRune(character)
		}
		rest = tail[end+1:]
	}
	out.WriteString(rest)
	return out.String()
}

// numericEntity reads &#65; and &#x41; (the leading # already part of
// entity), refusing anything that is not a Unicode scalar value.
func numericEntity(entity string) (rune, bool) {
	digits, ok := strings.CutPrefix(entity, "#")
	if !ok {
		return 0, false
	}
	base := 10
	if hex, isHex := cutAnyPrefix(digits, "x", "X"); isHex {
		digits, base = hex, 16
	}
	// Rust's u32 parsing takes a leading plus sign; so does this.
	digits = strings.TrimPrefix(digits, "+")
	if digits == "" || strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		return 0, false
	}
	code, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return 0, false
	}
	character := rune(code)
	if !utf8.ValidRune(character) {
		return 0, false
	}
	return character, true
}

func cutAnyPrefix(s string, prefixes ...string) (string, bool) {
	for _, prefix := range prefixes {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			return rest, true
		}
	}
	return s, false
}

// asciiLower lowercases ASCII letters only, so byte offsets into the
// result are offsets into the input (strings.ToLower can change a
// string's length).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// asciiEqualFold is Rust's eq_ignore_ascii_case.
func asciiEqualFold(a, b string) bool {
	return len(a) == len(b) && asciiLower(a) == asciiLower(b)
}

func isASCIISpace(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}
