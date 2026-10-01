// Package configdocs renders the config reference pages (`wayle config
// docs`, wayle/src/docs): one VitePress page per documented schema, the
// shared types page, and the index, from config.DocSections.
package configdocs

import "strings"

// rehostRustdoc is rehost_rustdoc: ATX headings shift to sit below
// enclosingDepth (never shallower than one below it, never deeper than
// six); fenced code passes through.
func rehostRustdoc(description string, enclosingDepth int) string {
	const maxDepth = 6
	shift := max(enclosingDepth-1, 0)
	minDepth := min(enclosingDepth+1, maxDepth)
	var b strings.Builder
	inFence := false
	for _, line := range lines(description) {
		if isFenceLine(line) {
			inFence = !inFence
			b.WriteString(line + "\n")
			continue
		}
		if inFence {
			b.WriteString(line + "\n")
			continue
		}
		if depth, body, ok := parseATXHeading(line); ok {
			n := min(max(depth+shift, minDepth), maxDepth)
			b.WriteString(strings.Repeat("#", n) + body + "\n")
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// isFenceLine is is_fence_line.
func isFenceLine(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// parseATXHeading is parse_atx_heading: the depth and the body from the
// space after the hashes; bare hashes, seven or more, or no space are
// no heading.
func parseATXHeading(line string) (int, string, bool) {
	t := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || !strings.HasPrefix(t[n:], " ") {
		return 0, "", false
	}
	return n, t[n:], true
}

// lines is str::lines: split on \n, a trailing \r dropped, no final
// empty line for a trailing newline.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	for i, l := range out {
		out[i] = strings.TrimSuffix(l, "\r")
	}
	return out
}
