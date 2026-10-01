package config

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

// The layer tree is the format-agnostic value model every config file
// parses into, like the Rust loader's toml::Value: YAML and TOML files
// both become it, so imports, merging, and the layers never care which
// format a file was. Parsed values are:
//
//	string | int64 | float64 | bool | datetime | []any | map[string]any
//
// Trees produced by encoding typed values additionally hold *table (an
// insertion-ordered table, the Rust preserve_order map), float32
// (serde's f32, kept so pretty output prints 0.35 rather than its
// widened f64), and uint64 above the int64 range.

// datetime is a TOML date-time literal carried verbatim; no config key
// takes one, so it only ever reaches an "invalid type" diagnostic.
type datetime string

// table is an insertion-ordered string-keyed table.
type table struct {
	keys []string
	vals map[string]any
}

func newTable() *table { return &table{vals: map[string]any{}} }

func (t *table) set(key string, v any) {
	if _, ok := t.vals[key]; !ok {
		t.keys = append(t.keys, key)
	}
	t.vals[key] = v
}

func (t *table) get(key string) (any, bool) {
	v, ok := t.vals[key]
	return v, ok
}

func (t *table) delete(key string) {
	if _, ok := t.vals[key]; !ok {
		return
	}
	delete(t.vals, key)
	for i, k := range t.keys {
		if k == key {
			t.keys = append(t.keys[:i], t.keys[i+1:]...)
			return
		}
	}
}

func (t *table) len() int { return len(t.keys) }

// tableEntries returns a table-like value's entries in iteration order:
// insertion order for *table, sorted keys for a parsed map.
func tableEntries(v any) (keys []string, get func(string) any, ok bool) {
	switch t := v.(type) {
	case *table:
		return t.keys, func(k string) any { return t.vals[k] }, true
	case map[string]any:
		keys = make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys, func(k string) any { return t[k] }, true
	}
	return nil, nil, false
}

// lookup reads one key of a table-like value.
func lookup(v any, key string) (any, bool) {
	switch t := v.(type) {
	case *table:
		return t.get(key)
	case map[string]any:
		val, ok := t[key]
		return val, ok
	}
	return nil, false
}

func isTable(v any) bool {
	switch v.(type) {
	case *table, map[string]any:
		return true
	}
	return false
}

// isYAMLPath reports whether path is parsed as YAML (.yaml/.yml);
// anything else is TOML (loading/mod.rs is_yaml_path).
func isYAMLPath(path string) bool {
	switch filepath.Ext(path) {
	case ".yaml", ".yml":
		return true
	}
	return false
}

// parseDocument parses one config file into the layer tree, picking the
// format from the extension.
func parseDocument(data []byte, path string) (any, error) {
	if isYAMLPath(path) {
		return parseYAML(data)
	}
	return parseTOML(data)
}

func parseTOML(data []byte) (any, error) {
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, err
	}
	return normalizeTOML(doc), nil
}

// normalizeTOML maps BurntSushi's decoded shapes onto the tree kinds.
func normalizeTOML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			t[k] = normalizeTOML(child)
		}
		return t
	case []map[string]any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = normalizeTOML(child)
		}
		return out
	case []any:
		for i, child := range t {
			t[i] = normalizeTOML(child)
		}
		return t
	case time.Time:
		return datetime(t.Format(time.RFC3339Nano))
	case string, int64, float64, bool:
		return t
	}
	return datetime(fmt.Sprint(v))
}

// parseYAML reads a YAML document into the tree the way serde_yaml
// deserializes into toml::Value: mappings become tables (keys taken as
// strings), sequences arrays, and scalars resolve by the YAML core
// schema. TOML has no null, so a null anywhere — including an empty
// document — is the same error serde reports.
func parseYAML(data []byte) (any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, errors.New("invalid type: unit value, expected any valid TOML value")
	}
	return yamlValue(doc.Content[0])
}

func yamlValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.AliasNode:
		return yamlValue(n.Alias)
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			for keyNode.Kind == yaml.AliasNode {
				keyNode = keyNode.Alias
			}
			if keyNode.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: invalid type: map key must be a string", keyNode.Line)
			}
			key := keyNode.Value
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("line %d: duplicate key: `%s`", keyNode.Line, key)
			}
			val, err := yamlValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			val, err := yamlValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return out, nil
	case yaml.ScalarNode:
		return yamlScalar(n)
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

func yamlScalar(n *yaml.Node) (any, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, fmt.Errorf("line %d: invalid type: unit value, expected any valid TOML value", n.Line)
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, err
		}
		return b, nil
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return i, nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return nil, fmt.Errorf("line %d: u64 value was too large", n.Line)
		}
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		return f, nil
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		return f, nil
	}
	return n.Value, nil
}

// mergeTrees overlays one tree on another: tables merge key by key,
// anything else is replaced by the overlay (merging.rs).
func mergeTrees(base, overlay any) any {
	baseTable, baseOK := base.(map[string]any)
	overTable, overOK := overlay.(map[string]any)
	if !baseOK || !overOK {
		return overlay
	}
	merged := make(map[string]any, len(baseTable)+len(overTable))
	maps.Copy(merged, overTable)
	for k, baseValue := range baseTable {
		if overValue, ok := merged[k]; ok {
			merged[k] = mergeTrees(baseValue, overValue)
		} else {
			merged[k] = baseValue
		}
	}
	return merged
}

// mergeAll folds the imports in order, then the importing file on top
// (merge_toml_configs).
func mergeAll(imports []any, main any) any {
	var acc any = map[string]any{}
	for _, imp := range imports {
		acc = mergeTrees(acc, imp)
	}
	return mergeTrees(acc, main)
}

// InvalidFieldError is the Rust Error::InvalidConfigField: a path
// segment that does not resolve.
type InvalidFieldError struct {
	Field     string
	Component string
	Reason    InvalidFieldReason
}

// InvalidFieldReason says why a path segment failed.
type InvalidFieldReason int

// Reasons.
const (
	FieldNotFound InvalidFieldReason = iota
	FieldEmptyPath
	FieldParentNotTable
)

func (r InvalidFieldReason) String() string {
	switch r {
	case FieldEmptyPath:
		return "empty path"
	case FieldParentNotTable:
		return "parent is not a table"
	}
	return "field not found"
}

func (e *InvalidFieldError) Error() string {
	return fmt.Sprintf("invalid config field '%s' in %s: %s", e.Field, e.Component, e.Reason)
}

// insertPath sets value at a dot-separated path, creating intermediate
// tables (toml_path.rs insert). An empty path inserts the "" key, like
// the Rust split on '.'.
func insertPath(root map[string]any, path string, value any) error {
	segments := strings.Split(path, ".")
	final := segments[len(segments)-1]
	current := root
	for _, seg := range segments[:len(segments)-1] {
		child, ok := current[seg]
		if !ok {
			next := map[string]any{}
			current[seg] = next
			current = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return &InvalidFieldError{Field: seg, Component: path, Reason: FieldParentNotTable}
		}
		current = next
	}
	current[final] = value
	return nil
}

// widenFloats converts every float32 in an encoded tree to float64,
// the way toml::Value::try_from turns an f32 into an f64 (0.35f32 is
// then 0.3499999940395355). The CLI's get/set output and runtime.toml
// see values through that conversion; `config default` does not.
func widenFloats(v any) any {
	switch t := v.(type) {
	case float32:
		return float64(t)
	case *table:
		out := newTable()
		for _, k := range t.keys {
			out.set(k, widenFloats(t.vals[k]))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = widenFloats(child)
		}
		return out
	}
	return v
}

// toPlain converts an encoded tree into the parsed shape (maps without
// order, float64), so an encoded value can be re-applied as a layer.
func toPlain(v any) any {
	switch t := v.(type) {
	case float32:
		return float64(t)
	case uint64:
		if t <= math.MaxInt64 {
			return int64(t)
		}
		return float64(t)
	case *table:
		out := make(map[string]any, len(t.keys))
		for _, k := range t.keys {
			out[k] = toPlain(t.vals[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = toPlain(child)
		}
		return out
	}
	return v
}

// debugValue renders a tree value the way Rust's Debug prints a
// toml::Value, used for scalar values in diagnostics.
func debugValue(v any) string {
	switch t := v.(type) {
	case string:
		return "String(" + rustDebugString(t) + ")"
	case int64:
		return "Integer(" + strconv.FormatInt(t, 10) + ")"
	case uint64:
		return "Integer(" + strconv.FormatUint(t, 10) + ")"
	case float64:
		return "Float(" + rustFloatDebug(t, 64) + ")"
	case float32:
		return "Float(" + rustFloatDebug(float64(t), 32) + ")"
	case bool:
		return "Boolean(" + strconv.FormatBool(t) + ")"
	case datetime:
		return "Datetime(" + string(t) + ")"
	case []any:
		parts := make([]string, len(t))
		for i, child := range t {
			parts[i] = debugValue(child)
		}
		return "Array([" + strings.Join(parts, ", ") + "])"
	}
	keys, get, ok := tableEntries(v)
	if !ok {
		return fmt.Sprint(v)
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = rustDebugString(k) + ": " + debugValue(get(k))
	}
	return "Table({" + strings.Join(parts, ", ") + "})"
}

// rustDebugString quotes s like Rust's str Debug.
func rustDebugString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0:
			b.WriteString(`\0`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// rustFloat formats a float like Rust's Display: the shortest
// round-tripping decimal for its width, never in exponent form.
func rustFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return strconv.FormatFloat(f, 'f', -1, bits)
}

// rustFloatDebug is Rust's float Debug: Display plus a ".0" on
// integral values.
func rustFloatDebug(f float64, bits int) string {
	s := rustFloat(f, bits)
	if !math.IsNaN(f) && !math.IsInf(f, 0) && !strings.ContainsRune(s, '.') {
		s += ".0"
	}
	return s
}

// hasPath reports whether the dot path names a value in tree.
func hasPath(tree any, path string) bool {
	v := tree
	for seg := range strings.SplitSeq(path, ".") {
		next, ok := lookup(v, seg)
		if !ok {
			return false
		}
		v = next
	}
	return true
}
