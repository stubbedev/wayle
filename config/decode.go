package config

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
)

// Leaf decoding: one layer-tree value into one typed value, with
// serde's acceptance rules (an integer is a valid float, a float is
// never an integer, integers are range-checked) and serde's error
// wording, since that wording is what the Rust diagnostics print.

// defaulter is implemented by value structs whose missing keys take
// something other than the zero value (a serde struct with
// #[serde(default)] and a hand-written Default).
type defaulter interface {
	setDefaults()
}

// decodeLeaf decodes v into a fresh value of type t.
func decodeLeaf(t reflect.Type, v any) (reflect.Value, error) {
	out := reflect.New(t).Elem()
	if err := decodeInto(out, v); err != nil {
		return reflect.Value{}, err
	}
	return out, nil
}

// decodeInto decodes v into the addressable dst.
func decodeInto(dst reflect.Value, v any) error {
	t := dst.Type()
	if u, ok := reflect.TypeAssert[Unmarshaler](dst.Addr()); ok {
		return u.UnmarshalConfig(v)
	}
	if variants := enumVariants(t); variants != nil {
		s, err := decodeEnum(variants, v)
		if err != nil {
			return err
		}
		dst.SetString(s)
		return nil
	}
	switch t.Kind() {
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return invalidType(v, "a boolean")
		}
		dst.SetBool(b)
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return invalidType(v, "a string")
		}
		dst.SetString(s)
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		i, ok := v.(int64)
		if !ok {
			return invalidType(v, rustIntName(t))
		}
		if dst.OverflowInt(i) {
			return fmt.Errorf("invalid value: integer `%d`, expected %s", i, rustIntName(t))
		}
		dst.SetInt(i)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		i, ok := v.(int64)
		if !ok {
			return invalidType(v, rustIntName(t))
		}
		if i < 0 || dst.OverflowUint(uint64(i)) {
			return fmt.Errorf("invalid value: integer `%d`, expected %s", i, rustIntName(t))
		}
		dst.SetUint(uint64(i))
	case reflect.Float32, reflect.Float64:
		f, ok := asFloat(v)
		if !ok {
			return invalidType(v, rustIntName(t))
		}
		dst.SetFloat(f)
	case reflect.Pointer:
		// Option<T> from a present key is always Some.
		elem := reflect.New(t.Elem())
		if err := decodeInto(elem.Elem(), v); err != nil {
			return err
		}
		dst.Set(elem)
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return invalidType(v, "a sequence")
		}
		out := reflect.MakeSlice(t, len(arr), len(arr))
		for i, elem := range arr {
			if err := decodeInto(out.Index(i), elem); err != nil {
				return err
			}
		}
		dst.Set(out)
	case reflect.Map:
		keys, get, ok := tableEntries(v)
		if !ok {
			return invalidType(v, "a map")
		}
		out := reflect.MakeMapWithSize(t, len(keys))
		for _, k := range keys {
			elem := reflect.New(t.Elem()).Elem()
			if err := decodeInto(elem, get(k)); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(k).Convert(t.Key()), elem)
		}
		dst.Set(out)
	case reflect.Struct:
		return decodeValueStruct(dst, v)
	default:
		return fmt.Errorf("config: no decoder for %s", t)
	}
	return nil
}

// decodeValueStruct decodes a plain serde struct: defaults first,
// then each present key; unknown keys are ignored and a missing
// required key is serde's "missing field".
func decodeValueStruct(dst reflect.Value, v any) error {
	t := dst.Type()
	if !isTable(v) {
		return invalidType(v, "struct "+schemaName(t))
	}
	if d, ok := reflect.TypeAssert[defaulter](dst.Addr()); ok {
		d.setDefaults()
	}
	for _, f := range fieldsOf(t) {
		if f.nolayer {
			continue
		}
		found := false
		for _, key := range f.lookupKeys() {
			val, ok := lookup(v, key)
			if !ok {
				continue
			}
			found = true
			if err := decodeInto(dst.FieldByIndex(f.index), val); err != nil {
				return err
			}
			break
		}
		if !found && f.required {
			return fmt.Errorf("missing field `%s`", f.key)
		}
	}
	return nil
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// invalidType is serde's invalid_type error.
func invalidType(v any, expected string) error {
	return fmt.Errorf("invalid type: %s, expected %s", unexpected(v), expected)
}

// unexpected renders a tree value as serde's Unexpected.
func unexpected(v any) string {
	switch t := v.(type) {
	case bool:
		return "boolean `" + strconv.FormatBool(t) + "`"
	case int64:
		return "integer `" + strconv.FormatInt(t, 10) + "`"
	case float64:
		return "floating point `" + rustFloatDebug(t, 64) + "`"
	case string:
		return "string " + rustDebugString(t)
	case []any:
		return "sequence"
	}
	return "map"
}

// rustIntName is the Rust primitive a Go numeric kind stands for.
func rustIntName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int8:
		return "i8"
	case reflect.Int16:
		return "i16"
	case reflect.Int32:
		return "i32"
	case reflect.Int64:
		return "i64"
	case reflect.Int:
		return "isize"
	case reflect.Uint8:
		return "u8"
	case reflect.Uint16:
		return "u16"
	case reflect.Uint32:
		return "u32"
	case reflect.Uint64:
		return "u64"
	case reflect.Uint:
		return "usize"
	case reflect.Float32:
		return "f32"
	case reflect.Float64:
		return "f64"
	}
	return t.String()
}

// absent marks a value that serializes to nothing (None).
type absent struct{}

// encodeValue serializes a typed value into the encoded tree: structs
// as ordered tables in field order, nil pointers omitted (serde skips
// None in TOML), maps by sorted key.
func encodeValue(v reflect.Value) any { return encodeValueMode(v, false) }

// encodeValueMode is encodeValue; keepNone keeps None as absent{}
// entries (serde_json's null) instead of dropping them.
func encodeValueMode(v reflect.Value, keepNone bool) any {
	t := v.Type()
	if t.Kind() != reflect.Pointer && t.Implements(marshalerType) {
		m, _ := reflect.TypeAssert[Marshaler](v)
		return m.MarshalConfig()
	}
	if v.CanAddr() && reflect.PointerTo(t).Implements(marshalerType) {
		m, _ := reflect.TypeAssert[Marshaler](v.Addr())
		return m.MarshalConfig()
	}
	if enumVariants(t) != nil {
		return v.String()
	}
	switch t.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		return v.Int()
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		u := v.Uint()
		if u <= math.MaxInt64 {
			return int64(u)
		}
		return u
	case reflect.Float32:
		return float32(v.Float())
	case reflect.Float64:
		return v.Float()
	case reflect.Pointer:
		if v.IsNil() {
			return absent{}
		}
		return encodeValueMode(v.Elem(), keepNone)
	case reflect.Slice:
		out := make([]any, 0, v.Len())
		for i := range v.Len() {
			out = append(out, encodeValueMode(v.Index(i), keepNone))
		}
		return out
	case reflect.Map:
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		out := newTable()
		for _, k := range keys {
			elem := encodeValueMode(v.MapIndex(reflect.ValueOf(k).Convert(t.Key())), keepNone)
			if _, skip := elem.(absent); !skip || keepNone {
				out.set(k, elem)
			}
		}
		return out
	case reflect.Struct:
		out := newTable()
		for _, f := range fieldsOf(t) {
			elem := encodeValueMode(v.FieldByIndex(f.index), keepNone)
			if _, skip := elem.(absent); !skip || keepNone {
				out.set(f.key, elem)
			}
		}
		return out
	}
	panic(fmt.Sprintf("config: no encoder for %s", t))
}

// Encode serializes a config (or any schema value) into its TOML/YAML
// shape: the Rust Serialize output.
func encode(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	return encodeValue(rv)
}
