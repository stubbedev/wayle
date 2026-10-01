// Package pango reads Pango markup - what rofi rows, drun's display
// format, and script messages are written in - and translates it into
// gelm's rich-label subset. Weight, style, and hex foreground colors
// carry over; size, underline, fonts, and the other Pango attributes
// parse and render as plain text, since gelm's labels have no run
// sizes or decorations.
package pango

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// frame is one open tag and what it turned on.
type frame struct {
	name  string
	close string
}

// Translate converts Pango markup into gelm markup (b, i, span
// color). It reports false for markup Pango itself would reject: a
// stray or mismatched tag, an unknown element, a broken entity.
func Translate(markup string) (string, bool) {
	var out strings.Builder
	if !walk(markup, &out, true) {
		return "", false
	}
	return out.String(), true
}

// PlainText strips the markup to its text, as pango_parse_markup
// returns it; false when the markup does not parse.
func PlainText(markup string) (string, bool) {
	var out strings.Builder
	if !walk(markup, &out, false) {
		return "", false
	}
	return out.String(), true
}

// Escape is g_markup_escape_text: the five XML specials as entities.
func Escape(text string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&#39;", "\"", "&quot;")
	return r.Replace(text)
}

// escapeGelm writes text in gelm's allowlisted entities.
func escapeGelm(out *strings.Builder, text string) {
	for _, r := range text {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '"':
			out.WriteString("&quot;")
		case '\'':
			out.WriteString("&apos;")
		default:
			out.WriteRune(r)
		}
	}
}

var knownTags = map[string]bool{
	"markup": true, "span": true, "b": true, "big": true, "i": true, "s": true,
	"sub": true, "sup": true, "small": true, "tt": true, "u": true,
}

func walk(src string, out *strings.Builder, rich bool) bool {
	var stack []frame
	for i := 0; i < len(src); {
		switch src[i] {
		case '<':
			end := strings.IndexByte(src[i:], '>')
			if end < 0 {
				return false
			}
			tag := src[i+1 : i+end]
			i += end + 1
			if name, ok := strings.CutPrefix(tag, "/"); ok {
				name = strings.TrimSpace(name)
				if len(stack) == 0 || stack[len(stack)-1].name != name {
					return false
				}
				if rich {
					out.WriteString(stack[len(stack)-1].close)
				}
				stack = stack[:len(stack)-1]
				continue
			}
			selfClosing := strings.HasSuffix(tag, "/")
			tag = strings.TrimSuffix(tag, "/")
			name, attrs, _ := strings.Cut(strings.TrimSpace(tag), " ")
			if !knownTags[name] {
				return false
			}
			open, closing, ok := translateTag(name, attrs)
			if !ok {
				return false
			}
			if selfClosing {
				continue
			}
			if rich {
				out.WriteString(open)
			}
			stack = append(stack, frame{name: name, close: closing})
		case '&':
			end := strings.IndexByte(src[i:], ';')
			if end < 0 {
				return false
			}
			r, ok := entity(src[i+1 : i+end])
			if !ok {
				return false
			}
			i += end + 1
			if rich {
				escapeGelm(out, string(r))
			} else {
				out.WriteRune(r)
			}
		default:
			r, size := utf8.DecodeRuneInString(src[i:])
			i += size
			if rich {
				escapeGelm(out, string(r))
			} else {
				out.WriteRune(r)
			}
		}
	}
	return len(stack) == 0
}

// entity decodes an XML entity body (amp, #39, #x27).
func entity(body string) (rune, bool) {
	switch body {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	num, ok := strings.CutPrefix(body, "#")
	if !ok {
		return 0, false
	}
	base := 10
	if hex, ok := strings.CutPrefix(num, "x"); ok {
		num, base = hex, 16
	}
	v, err := strconv.ParseUint(num, base, 32)
	if err != nil || !utf8.ValidRune(rune(v)) {
		return 0, false
	}
	return rune(v), true
}

// translateTag returns the gelm open and close for a Pango element.
func translateTag(name, attrs string) (string, string, bool) {
	switch name {
	case "b":
		return "<b>", "</b>", true
	case "i":
		return "<i>", "</i>", true
	case "span":
		return spanTag(attrs)
	}
	// markup, big, small, s, sub, sup, tt, u: the text stays, the
	// styling has no gelm counterpart.
	return "", "", strings.TrimSpace(attrs) == ""
}

// spanTag maps a span's attributes: bold weights open <b>, italic and
// oblique styles <i>, a hex foreground a colored span. Every other
// attribute parses and is dropped.
func spanTag(attrs string) (string, string, bool) {
	values, ok := parseAttrs(attrs)
	if !ok {
		return "", "", false
	}
	var open, closing string
	if c, ok := firstOf(values, "foreground", "fgcolor", "color"); ok && validHex(c) {
		open += `<span color="` + c + `">`
		closing = "</span>" + closing
	}
	if w, ok := firstOf(values, "weight", "font_weight", "font-weight"); ok && boldWeight(w) {
		open += "<b>"
		closing = "</b>" + closing
	}
	if s, ok := firstOf(values, "style", "font_style", "font-style"); ok && (s == "italic" || s == "oblique") {
		open += "<i>"
		closing = "</i>" + closing
	}
	return open, closing, true
}

func firstOf(m map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	return "", false
}

func boldWeight(w string) bool {
	switch w {
	case "semibold", "bold", "ultrabold", "heavy", "ultraheavy":
		return true
	}
	n, err := strconv.Atoi(w)
	return err == nil && n >= 600
}

func validHex(c string) bool {
	h, ok := strings.CutPrefix(c, "#")
	if !ok || (len(h) != 3 && len(h) != 6 && len(h) != 8) {
		return false
	}
	_, err := strconv.ParseUint(h, 16, 64)
	return err == nil
}

// parseAttrs reads name="value" / name='value' pairs.
func parseAttrs(s string) (map[string]string, bool) {
	out := map[string]string{}
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return out, true
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, false
		}
		name := strings.TrimSpace(s[:eq])
		rest := strings.TrimSpace(s[eq+1:])
		if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
			return nil, false
		}
		end := strings.IndexByte(rest[1:], rest[0])
		if end < 0 {
			return nil, false
		}
		value, ok := PlainText(rest[1 : 1+end])
		if !ok {
			return nil, false
		}
		out[name] = value
		s = rest[end+2:]
	}
}
