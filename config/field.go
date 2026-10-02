package config

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// FieldKind is the control a leaf's type calls for: what the settings
// GUI edits it with when a page names only its path.
type FieldKind int

// The kinds.
const (
	// FieldOther needs an editor of its own (lists, maps, structs).
	FieldOther FieldKind = iota
	// FieldBool is a switch.
	FieldBool
	// FieldEnum picks one of Variants.
	FieldEnum
	// FieldInt is a whole number in [Min, Max].
	FieldInt
	// FieldFloat is a number in [Min, Max].
	FieldFloat
	// FieldText is edited as the string it encodes to (String, and the
	// types serializing as one: Size, a monitor target, ...).
	FieldText
)

// spinMax is the largest whole number a float64 spin keeps exact
// (U64_SPIN_MAX in the Rust number editor).
const spinMax = 1 << 53

// FieldMeta describes the leaf at a path for an editor.
type FieldMeta struct {
	Kind FieldKind
	// Type is the Rust schema name of the leaf's type (OsdPosition,
	// uint32, ...).
	Type string
	// Variants are an enum's values in schema order.
	Variants []string
	// Min and Max bound a number: the type's own range, narrowed by a
	// validated newtype's (Percentage 0-100, ScaleFactor 0.25-3).
	Min, Max float64
	// Optional is an Option<T> leaf, which may be unset.
	Optional bool
	// Elem describes a list's items (nil for anything else).
	Elem *FieldMeta
}

// Field describes the leaf at a dot path, or a field inside a value
// struct leaf (animations.osd.enter); false for a path naming no field
// or a container.
func Field(path string) (FieldMeta, bool) {
	leaf, sub, ok := LeafPath(path)
	if !ok {
		return FieldMeta{}, false
	}
	_, f, ok := fieldAtPath(typeOf[Config](), leaf)
	if !ok || isContainer(f.typ) {
		return FieldMeta{}, false
	}
	if sub != "" {
		t := f.typ
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if f, ok = valueFieldAt(t, strings.Split(sub, ".")); !ok {
			return FieldMeta{}, false
		}
	}
	return describe(f.typ), true
}

// MetaOf describes type T as Field would a field of it: for the
// fields of a list's items, which have no path of their own.
func MetaOf[T any]() FieldMeta { return describe(typeOf[T]()) }

// describe is the FieldMeta of a field type.
func describe(t reflect.Type) FieldMeta {
	m := FieldMeta{}
	if t.Kind() == reflect.Pointer {
		m.Optional, t = true, t.Elem()
	}
	m.Type = schemaName(t)
	if t.Kind() == reflect.Slice && !implements(t, schemaProviderType) {
		elem := describe(t.Elem())
		m.Elem = &elem
	}
	switch {
	case enumVariants(t) != nil:
		m.Kind, m.Variants = FieldEnum, enumVariants(t)
	case t.Kind() == reflect.Bool:
		m.Kind = FieldBool
	case isInt(t.Kind()):
		m.Kind = FieldInt
		m.Min, m.Max = intRange(t)
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		m.Kind = FieldFloat
		m.Min, m.Max = schemaRange(t, -spinMax, spinMax)
	case encodesAsString(t):
		m.Kind = FieldText
	}
	return m
}

func isInt(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

// intRange is the kind's range, capped where a spin stays exact, then
// narrowed by the type's schema.
func intRange(t reflect.Type) (lo, hi float64) {
	switch t.Kind() {
	case reflect.Uint8:
		lo, hi = 0, math.MaxUint8
	case reflect.Uint16:
		lo, hi = 0, math.MaxUint16
	case reflect.Uint32:
		lo, hi = 0, math.MaxUint32
	case reflect.Uint, reflect.Uint64:
		lo, hi = 0, spinMax
	case reflect.Int8:
		lo, hi = math.MinInt8, math.MaxInt8
	case reflect.Int16:
		lo, hi = math.MinInt16, math.MaxInt16
	case reflect.Int32:
		lo, hi = math.MinInt32, math.MaxInt32
	default:
		lo, hi = -spinMax, spinMax
	}
	return schemaRange(t, lo, hi)
}

// schemaRange narrows [lo, hi] to a validated newtype's minimum and
// maximum.
func schemaRange(t reflect.Type, lo, hi float64) (float64, float64) {
	if !implements(t, schemaProviderType) {
		return lo, hi
	}
	s := newSchemaGen().plainSchema(t)
	if v, ok := asFloat(s["minimum"]); ok {
		lo = max(lo, v)
	}
	if v, ok := asFloat(s["maximum"]); ok {
		hi = min(hi, v)
	}
	return lo, hi
}

// encodesAsString reports whether t's schema is a plain string: every
// value serializes as one.
func encodesAsString(t reflect.Type) bool {
	return newSchemaGen().plainSchema(t)["type"] == "string"
}

// EnumLabelKey is the Fluent key labeling one enum value
// (EnumVariant::fluent_key): enum-<type>-<variant>, kebab-cased from
// the Rust names. Serde's kebab-case rename makes the value the
// variant's kebab name, except for the variants enumIdents lists. The
// key may be missing from the bundle; the Rust GUI then shows the raw
// value.
func EnumLabelKey(enumType, value string) string {
	variant := value
	if ident, ok := enumIdents[enumType+"."+value]; ok {
		variant = ident
	}
	return "enum-" + pascalToKebab(enumType) + "-" + variant
}

// enumIdents are the variants whose serde value is not the kebab form
// of the Rust identifier.
var enumIdents = map[string]string{
	"SessionAction.log-out": "logout",
	"TimeFormat.12h":        "twelve-hour",
	"TimeFormat.24h":        "twenty-four-hour",
}

// pascalToKebab is the derive's pascal_to_kebab: a hyphen before every
// uppercase letter but the first, all lowercased.
func pascalToKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// newSchemaGen is a generator for one type's schema outside the whole
// document.
func newSchemaGen() *schemaGen {
	return &schemaGen{defs: map[string]Schema{}, ids: map[string]string{}, names: map[string]bool{}, defaults: map[reflect.Type]reflect.Value{}}
}

// isValueStruct reports whether t is a struct decoded as one value
// whose fields still carry cfg tags (SurfaceAnimation): a leaf the
// layers set whole, with fields an editor can address.
func isValueStruct(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && (t.Implements(valueStructType) || reflect.PointerTo(t).Implements(valueStructType))
}

// LeafPath splits a dot path at the config leaf it lies in: leaf is
// the field the layers set and reset, sub the path inside that leaf's
// value struct ("" when the path names the leaf). ok is false for a
// path naming no field.
func LeafPath(path string) (leaf, sub string, ok bool) {
	t := typeOf[Config]()
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		_, f, found := fieldAtPath(t, seg)
		if !found {
			return "", "", false
		}
		t = f.typ
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if isContainer(t) {
			continue
		}
		if i == len(segs)-1 {
			return path, "", true
		}
		if !isValueStruct(t) {
			return "", "", false
		}
		if _, ok := valueFieldAt(t, segs[i+1:]); !ok {
			return "", "", false
		}
		return strings.Join(segs[:i+1], "."), strings.Join(segs[i+1:], "."), true
	}
	return "", "", false
}

// valueFieldAt finds the field a path names inside a value struct.
func valueFieldAt(t reflect.Type, segs []string) (fieldInfo, bool) {
	var f fieldInfo
	for _, seg := range segs {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if !isValueStruct(t) {
			return fieldInfo{}, false
		}
		found := false
		for _, cand := range fieldsOf(t) {
			if slices.Contains(cand.lookupKeys(), seg) {
				f, found = cand, true
				break
			}
		}
		if !found {
			return fieldInfo{}, false
		}
		t = f.typ
	}
	return f, true
}
