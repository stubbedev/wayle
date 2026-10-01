// Package jinja renders minijinja templates: the format-string engine
// behind every bar module's format key (wayle-shell-core's template.rs,
// minijinja 2 with its builtins and default settings).
//
// It covers what a format string can use: {{ }} expressions with the
// full operator set, attribute and item access, slicing, filters and
// tests, if/elif/else, for loops with the loop variable, set, raw
// blocks, comments, and whitespace control. Macros, includes, and
// template inheritance are not part of the Rust build (its minijinja
// has default features off) and are not here either.
//
// Values are Go data: nil (none), bool, the integer and float kinds,
// string, slices, and string-keyed maps. Undefined is its own value:
// it prints as nothing, is falsy, and iterates as empty, and looking
// an attribute up on it is an error, minijinja's lenient behavior.
package jinja

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// undefined is the value of a missing variable or attribute.
type undefined struct{}

// Undefined is the undefined value.
var Undefined any = undefined{}

func isUndefined(v any) bool { _, ok := v.(undefined); return ok }

// normalize maps Go values onto the template's kinds: every integer
// kind to int64, float32 to float64, typed slices and maps to []any and
// map[string]any. Anything else passes through and prints with %v.
func normalize(v any) any {
	switch x := v.(type) {
	case nil, undefined, bool, int64, float64, string, []any, map[string]any:
		return v
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		if x > math.MaxInt64 {
			return float64(x)
		}
		return int64(x)
	case float32:
		return float64(x)
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, s := range x {
			out[k] = s
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = normalize(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			out := make(map[string]any, rv.Len())
			for _, k := range rv.MapKeys() {
				out[k.String()] = normalize(rv.MapIndex(k).Interface())
			}
			return out
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return normalize(rv.Elem().Interface())
	}
	return v
}

// truthy is Value::is_true.
func truthy(v any) bool {
	switch x := normalize(v).(type) {
	case nil, undefined:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// display is Value's Display: what {{ v }} prints.
func display(v any) string {
	switch x := normalize(v).(type) {
	case undefined:
		return ""
	case nil:
		return "none"
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return formatFloat(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = repr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedKeys(x)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = strconv.Quote(k) + ": " + repr(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// formatFloat is the f64 display: Rust's shortest form, with ".0" on
// a whole number.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// repr is the Debug form inside sequences and maps: strings quoted.
func repr(v any) string {
	switch x := normalize(v).(type) {
	case string:
		return strconv.Quote(x)
	case undefined:
		return "undefined"
	}
	return display(v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// kind names a value's kind for error messages.
func kind(v any) string {
	switch normalize(v).(type) {
	case undefined:
		return "undefined"
	case nil:
		return "none"
	case bool:
		return "bool"
	case int64, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "sequence"
	case map[string]any:
		return "map"
	}
	return "plain object"
}

// iterate is try_iter: sequences item by item, maps by sorted key,
// strings by character, undefined as empty.
func iterate(v any) ([]any, error) {
	switch x := normalize(v).(type) {
	case undefined:
		return nil, nil
	case []any:
		return x, nil
	case map[string]any:
		keys := sortedKeys(x)
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = k
		}
		return out, nil
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out, nil
	}
	return nil, errorf("%s is not iterable", kind(v))
}

// length is Value::len.
func length(v any) (int, bool) {
	switch x := normalize(v).(type) {
	case string:
		return len([]rune(x)), true
	case []any:
		return len(x), true
	case map[string]any:
		return len(x), true
	}
	return 0, false
}

// equal is value equality: numbers compare across int and float.
func equal(a, b any) bool {
	a, b = normalize(a), normalize(b)
	if x, y, ok := numbers(a, b); ok {
		return x == y
	}
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if w, ok := y[k]; !ok || !equal(v, w) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// numbers reads two numeric values (bools count) as floats.
func numbers(a, b any) (float64, float64, bool) {
	x, ok1 := asFloat(a)
	y, ok2 := asFloat(b)
	return x, y, ok1 && ok2
}

func asFloat(v any) (float64, bool) {
	switch x := normalize(v).(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func asInt(v any) (int64, bool) {
	switch x := normalize(v).(type) {
	case int64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// kindRank is the ValueKind declaration order, which orders values of
// different kinds.
func kindRank(v any) int {
	switch v.(type) {
	case undefined:
		return 0
	case nil:
		return 1
	case bool:
		return 2
	case int64, float64:
		return 3
	case string:
		return 4
	case []any:
		return 6
	case map[string]any:
		return 7
	}
	return 9
}

// compare is Ord for Value: a total order, by kind first, then within
// the kind (numbers numerically, strings by bytes, sequences and maps
// element by element).
func compare(a, b any) int {
	a, b = normalize(a), normalize(b)
	if ra, rb := kindRank(a), kindRank(b); ra != rb {
		return cmpInt(ra, rb)
	}
	switch x := a.(type) {
	case undefined, nil:
		return 0
	case string:
		y, _ := b.(string)
		return strings.Compare(x, y)
	case []any:
		y, _ := b.([]any)
		for i := 0; i < len(x) && i < len(y); i++ {
			if c := compare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return cmpInt(len(x), len(y))
	case map[string]any:
		y, _ := b.(map[string]any)
		kx, ky := sortedKeys(x), sortedKeys(y)
		for i := 0; i < len(kx) && i < len(ky); i++ {
			if c := strings.Compare(kx[i], ky[i]); c != 0 {
				return c
			}
			if c := compare(x[kx[i]], y[ky[i]]); c != 0 {
				return c
			}
		}
		return cmpInt(len(kx), len(ky))
	}
	if ia, ok := intOperand(a); ok {
		if ib, ok := intOperand(b); ok {
			return cmpInt(ia, ib)
		}
	}
	fa, _ := asFloat(a)
	fb, _ := asFloat(b)
	switch {
	case fa < fb:
		return -1
	case fa > fb:
		return 1
	}
	return 0
}

func cmpInt[T int | int64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
