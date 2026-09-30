package launcher

import (
	"strings"

	"github.com/stubbedev/wayle/internal/shlex"
)

// Lookup resolves a template placeholder; false (or an empty value)
// means absent.
type Lookup func(key string) (string, bool)

// Render fills a rofi-style template (template.rs): {key} is replaced
// by its value, and a [...] block is emitted only when every {key}
// inside it resolved non-empty - rofi's PATTERN semantics for
// drun-display-format, window-format, ssh-command, and combi. An
// unknown key renders empty.
func Render(template string, lookup Lookup) string {
	var out strings.Builder
	runes := []rune(template)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '[':
			var block strings.Builder
			depth := 1
			for i++; i < len(runes); i++ {
				c := runes[i]
				if c == '[' {
					depth++
				} else if c == ']' {
					depth--
					if depth == 0 {
						break
					}
				}
				block.WriteRune(c)
			}
			if rendered, filled := renderBlock(block.String(), lookup); filled {
				out.WriteString(rendered)
			}
		case '{':
			key, next := collectKey(runes, i+1)
			i = next
			if v, ok := lookup(key); ok {
				out.WriteString(v)
			}
		default:
			out.WriteRune(runes[i])
		}
	}
	return out.String()
}

// RenderArgv renders a template as an argv: shell-split first, then
// each argument rendered on its own.
//
// The order is deliberately not rofi's. rofi substitutes into the
// command string and shell-parses the result, so a value holding a
// space becomes several arguments and a value holding a quote breaks
// the parse (rofi 2.0.0 runs nothing at all for a row named "it's
// here"). Splitting first means one placeholder is exactly one
// argument, whatever is in it, and a value cannot smuggle in more.
//
// Empty when the template does not shell-parse: half a command is
// worse than none.
func RenderArgv(template string, lookup Lookup) []string {
	words, ok := shlex.Split(template)
	if !ok {
		return nil
	}
	argv := make([]string, len(words))
	for i, w := range words {
		argv[i] = Render(w, lookup)
	}
	return argv
}

// renderBlock renders an optional block and reports whether every
// placeholder in it resolved to a non-empty value.
func renderBlock(block string, lookup Lookup) (string, bool) {
	var out strings.Builder
	filled := true
	runes := []rune(block)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '{' {
			out.WriteRune(runes[i])
			continue
		}
		key, next := collectKey(runes, i+1)
		i = next
		if v, ok := lookup(key); ok && v != "" {
			out.WriteString(v)
		} else {
			filled = false
		}
	}
	return out.String(), filled
}

// collectKey reads a placeholder name starting at runes[from] and
// returns it with the index of its closing brace (or the last index
// when the brace never closes).
func collectKey(runes []rune, from int) (string, int) {
	var key strings.Builder
	i := from
	for ; i < len(runes); i++ {
		if runes[i] == '}' {
			return key.String(), i
		}
		key.WriteRune(runes[i])
	}
	return key.String(), i
}

// Values is a Lookup over a fixed map.
func Values(m map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}
