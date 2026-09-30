package scss

import "strings"

// scanTop walks s calling visit for each byte outside strings,
// brackets, and parentheses (depth 0).
func scanTop(s string, visit func(i int)) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			i = skipString(s, i) - 1
		case '\\':
			i++
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		default:
			if depth == 0 {
				visit(i)
			}
		}
	}
}

// splitList splits a comma-separated list (selectors, media queries)
// at the commas outside strings, brackets, and parentheses.
func splitList(s string) []string {
	var parts []string
	start := 0
	scanTop(s, func(i int) {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	})
	return append(parts, s[start:])
}

// resolveSelectors resolves a rule's selector list against its parent
// selectors like Sass: `&` is replaced (each occurrence independently,
// as a cartesian product), otherwise the parent is prepended as a
// descendant. Top level (parents nil) may not use `&`. The list is in
// grass's order: the children's resolutions interleaved.
func resolveSelectors(at pos, parents []string, text string) []string {
	var perChild [][]string
	for _, complex := range splitList(text) {
		complex = strings.TrimSpace(complex)
		if complex == "" {
			at.fail("expected selector")
		}
		scanTop(complex, func(i int) {
			if complex[i] == '%' {
				at.unsupported("placeholder selectors (" + complex + ")")
			}
		})
		if parents == nil {
			if strings.Contains(complex, "&") {
				at.fail(`top-level selectors may not contain the parent selector "&"`)
			}
			perChild = append(perChild, []string{normalizeSelector(complex)})
			continue
		}
		perChild = append(perChild, resolveComplex(at, parents, complex))
	}
	var out []string
	for i := 0; ; i++ {
		more := false
		for _, rs := range perChild {
			if i < len(rs) {
				out = append(out, rs[i])
				more = true
			}
		}
		if !more {
			return out
		}
	}
}

func resolveComplex(at pos, parents []string, complex string) []string {
	nested := strings.Contains(complex, "&")
	complex = resolveInArguments(at, parents, complex)
	var segments []string
	start := 0
	scanTop(complex, func(i int) {
		if complex[i] == '&' {
			segments = append(segments, complex[start:i])
			start = i + 1
		}
	})
	segments = append(segments, complex[start:])
	if len(segments) == 1 {
		if nested {
			// Only inside pseudo-class arguments: `:not(&)`.
			return []string{normalizeSelector(complex)}
		}
		out := make([]string, len(parents))
		for i, p := range parents {
			out[i] = normalizeSelector(p + " " + complex)
		}
		return out
	}
	results := []string{segments[0]}
	for _, seg := range segments[1:] {
		next := make([]string, 0, len(results)*len(parents))
		for _, r := range results {
			for _, p := range parents {
				next = append(next, r+p+seg)
			}
		}
		results = next
	}
	for i, r := range results {
		results[i] = normalizeSelector(r)
	}
	return results
}

// resolveInArguments resolves `&` inside parenthesized arguments
// (`:not(&)`, `:is(& .x)`) against the whole parent list.
func resolveInArguments(at pos, parents []string, complex string) string {
	var b strings.Builder
	depth, open := 0, 0
	for i := 0; i < len(complex); i++ {
		c := complex[i]
		switch c {
		case '"', '\'':
			end := skipString(complex, i)
			if depth == 0 {
				b.WriteString(complex[i:end])
			}
			i = end - 1
			continue
		case '(':
			depth++
			if depth == 1 {
				open = i + 1
				b.WriteByte(c)
				continue
			}
		case ')':
			depth--
			if depth == 0 {
				inner := complex[open:i]
				if strings.Contains(inner, "&") {
					var resolved []string
					for _, part := range splitList(inner) {
						resolved = append(resolved, resolveComplex(at, parents, strings.TrimSpace(part))...)
					}
					inner = strings.Join(resolved, ", ")
				}
				b.WriteString(inner)
				b.WriteByte(c)
				continue
			}
		}
		if depth == 0 {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// normalizeSelector collapses whitespace and spaces combinators
// (`a>b` becomes `a > b`) outside arguments and strings.
func normalizeSelector(s string) string {
	s = collapseSpace(s)
	var b strings.Builder
	last := 0
	scanTop(s, func(i int) {
		if c := s[i]; c == '>' || c == '+' || c == '~' {
			b.WriteString(strings.TrimRight(s[last:i], " "))
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteByte(c)
			b.WriteByte(' ')
			last = i + 1
			for last < len(s) && s[last] == ' ' {
				last++
			}
		}
	})
	b.WriteString(s[last:])
	return strings.TrimSpace(b.String())
}
