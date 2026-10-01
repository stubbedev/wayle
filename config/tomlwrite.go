package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// TOML output byte-compatible with the Rust side's toml 1.x
// serializer (toml/src/ser/document + toml_writer), so `wayle config
// default`, `get`, `set`, and runtime.toml read the same as before the
// port: key-values before sub-tables, implicit parent tables elided,
// arrays of tables as [[headers]], multi-line arrays in pretty mode,
// and toml_writer's string/key quoting choices.

// errUnsupportedDocument is toml's "unsupported type" for a document
// root that is not a table.
var errUnsupportedDocument = errors.New("unsupported rust type")

type docTable struct {
	key         []string
	body        strings.Builder
	hasChildren bool
	array       bool
}

type docWriter struct {
	tables []*docTable
}

func (w *docWriter) newTable(key []string) *docTable {
	t := &docTable{key: key}
	w.tables = append(w.tables, t)
	return t
}

// tomlPretty renders a table as a pretty TOML document
// (toml::to_string_pretty); a non-table root is an error, like
// serializing a bare value as a document.
func tomlPretty(v any) (string, error) {
	if !isTable(v) {
		return "", errUnsupportedDocument
	}
	w := &docWriter{}
	w.writeTable(w.newTable(nil), v)
	var out strings.Builder
	first := true
	for _, t := range w.tables {
		if !requiredTable(t) {
			continue
		}
		if !first {
			out.WriteByte('\n')
		}
		first = false
		if t.key != nil {
			open, closing := "[", "]"
			if t.array {
				open, closing = "[[", "]]"
			}
			out.WriteString(open + strings.Join(t.key, ".") + closing + "\n")
		}
		out.WriteString(t.body.String())
	}
	return out.String(), nil
}

func requiredTable(t *docTable) bool {
	if t.key == nil {
		return t.body.Len() > 0
	}
	return t.array || t.body.Len() > 0 || !t.hasChildren
}

type strategy int

const (
	strategyValue strategy = iota
	strategyTable
	strategyArrayOfTables
)

func strategyOf(v any) strategy {
	if isTable(v) {
		return strategyTable
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return strategyValue
	}
	for _, elem := range arr {
		if !isTable(elem) {
			return strategyValue
		}
	}
	return strategyArrayOfTables
}

func (w *docWriter) writeTable(t *docTable, v any) {
	keys, get, _ := tableEntries(v)
	for _, k := range keys {
		val := get(k)
		key := tomlKey(k)
		switch strategyOf(val) {
		case strategyValue:
			t.body.WriteString(key + " = " + tomlValue(val, true) + "\n")
		case strategyArrayOfTables:
			t.hasChildren = true
			elems, _ := val.([]any)
			for _, elem := range elems {
				child := w.newTable(childKey(t.key, key))
				child.array = true
				w.writeTable(child, elem)
			}
		case strategyTable:
			t.hasChildren = true
			w.writeTable(w.newTable(childKey(t.key, key)), val)
		}
	}
}

func childKey(parent []string, key string) []string {
	out := make([]string, 0, len(parent)+1)
	return append(append(out, parent...), key)
}

// TOMLInline renders a value on one line, like toml::Value's Display
// (the form `wayle config set` echoes).
func TOMLInline(v any) string { return tomlValue(v, false) }

// tomlValue renders one value; multiline puts arrays of two or more
// elements one per line (the pretty style).
func tomlValue(v any, multiline bool) string {
	switch t := v.(type) {
	case string:
		return tomlString(t)
	case bool:
		return strconv.FormatBool(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return tomlFloat(t, 64)
	case float32:
		return tomlFloat(float64(t), 32)
	case datetime:
		return string(t)
	case []any:
		return tomlArray(t, multiline)
	}
	if keys, get, ok := tableEntries(v); ok {
		if len(keys) == 0 {
			return "{}"
		}
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(" " + tomlKey(k) + " = " + tomlValue(get(k), multiline))
		}
		b.WriteString(" }")
		return b.String()
	}
	return fmt.Sprint(v)
}

func tomlArray(arr []any, multiline bool) string {
	multi := multiline && len(arr) >= 2
	var b strings.Builder
	b.WriteByte('[')
	for i, elem := range arr {
		if multi {
			b.WriteString("\n    ")
		} else if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tomlValue(elem, multiline))
		if multi {
			b.WriteByte(',')
		}
	}
	if multi && len(arr) > 0 {
		b.WriteByte('\n')
	}
	b.WriteByte(']')
	return b.String()
}

// tomlFloat is toml_writer's float form: Rust Display with a ".0" on
// integral values, lowercase nan/inf.
func tomlFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		if math.Signbit(f) {
			return "-nan"
		}
		return "nan"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	s := rustFloat(f, bits)
	if !math.IsInf(f, 0) && math.Mod(f, 1) == 0 {
		s += ".0"
	}
	return s
}

// stringMetrics are toml_writer's ValueMetrics.
type stringMetrics struct {
	maxSingleQuotes, maxDoubleQuotes int
	escapeCodes, escape, newline     bool
}

func measureString(s string) stringMetrics {
	var m stringMetrics
	single, double := 0, 0
	for i := range len(s) {
		c := s[i]
		if c == '\'' {
			single++
			m.maxSingleQuotes = max(m.maxSingleQuotes, single)
		} else {
			single = 0
		}
		if c == '"' {
			double++
			m.maxDoubleQuotes = max(m.maxDoubleQuotes, double)
		} else {
			double = 0
		}
		switch {
		case c == '\\':
			m.escape = true
		case c == '\t':
		case c == '\n':
			m.newline = true
		case c <= 0x1f || c == 0x7f:
			m.escapeCodes = true
		}
	}
	return m
}

type stringEncoding int

const (
	encBare stringEncoding = iota
	encLiteral
	encBasic
	encMLLiteral
	encMLBasic
)

// tomlString picks toml_writer's default encoding: basic when nothing
// needs escaping, else literal, else multi-line forms.
func tomlString(s string) string {
	m := measureString(s)
	var enc stringEncoding
	switch {
	case !m.escapeCodes && !m.escape && m.maxDoubleQuotes == 0 && !m.newline:
		enc = encBasic
	case !m.escapeCodes && m.maxSingleQuotes == 0 && !m.newline:
		enc = encLiteral
	case !m.escapeCodes && !m.escape && m.maxDoubleQuotes <= 2:
		enc = encMLBasic
	case !m.escapeCodes && m.maxSingleQuotes <= 2:
		enc = encMLLiteral
	case m.newline:
		enc = encMLBasic
	default:
		enc = encBasic
	}
	return writeEncoded(s, enc, m.newline)
}

// tomlKey quotes a key only when it is not a bare key.
func tomlKey(k string) string {
	bare := k != ""
	single, double, escapeCodes, escape := false, false, false, false
	for i := range len(k) {
		c := k[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			bare = false
		}
		switch {
		case c == '\'':
			single = true
		case c == '"':
			double = true
		case c == '\\':
			escape = true
		case c == '\t':
		case c <= 0x1f || c == 0x7f:
			escapeCodes = true
		}
	}
	switch {
	case bare:
		return k
	case !escapeCodes && !escape && !double:
		return writeEncoded(k, encBasic, false)
	case !escapeCodes && !single:
		return writeEncoded(k, encLiteral, false)
	}
	return writeEncoded(k, encBasic, false)
}

func writeEncoded(s string, enc stringEncoding, newline bool) string {
	var delim string
	escaped, ml := false, false
	switch enc {
	case encLiteral:
		delim = "'"
	case encBasic:
		delim, escaped = `"`, true
	case encMLLiteral:
		delim, ml = "'''", true
	case encMLBasic:
		delim, escaped, ml = `"""`, true, true
	}
	var b strings.Builder
	b.WriteString(delim)
	if newline && ml {
		b.WriteByte('\n')
	}
	if !escaped {
		b.WriteString(s)
		b.WriteString(delim)
		return b.String()
	}
	maxDouble := 0
	if ml {
		maxDouble = 2
	}
	seqDouble := 0
	for i := range len(s) {
		c := s[i]
		if c == '"' {
			seqDouble++
			if seqDouble > maxDouble {
				b.WriteString(`\"`)
				continue
			}
		} else {
			seqDouble = 0
		}
		switch {
		case c == 0x8:
			b.WriteString(`\b`)
		case c == 0x9:
			b.WriteString(`\t`)
		case c == 0xa && !ml:
			b.WriteString(`\n`)
		case c == 0xc:
			b.WriteString(`\f`)
		case c == 0xd:
			b.WriteString(`\r`)
		case c == '\\':
			b.WriteString(`\\`)
		case c != 0xa && (c <= 0x1f || c == 0x7f):
			fmt.Fprintf(&b, `\u%04X`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString(delim)
	return b.String()
}
