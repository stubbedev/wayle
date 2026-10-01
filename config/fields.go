package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// The config schema is declared once, on the Go types, through `cfg`
// struct tags; loading, runtime overrides, `wayle config get/set/
// reset/default`, and the JSON Schema all walk the same metadata, so a
// key added to a struct is covered everywhere by construction. This is
// the Go counterpart of the #[wayle_config] derive
// (crates/wayle-derive/src/{wayle_config,derives,field_utils}.rs).
//
// Tag grammar: `cfg:"key[,option...]"`.
//
//	key               the canonical TOML/YAML key (serde rename)
//	alias=<k>         an extra key accepted on read (serde alias)
//	deprecated=<k>    a legacy key accepted on read with a warning
//	                  (#[wayle(deprecated_alias)])
//	inline            a Go-only grouping struct whose fields belong to
//	                  the parent table (no key of its own)
//	required          inside a value struct: a missing key is an error
//	                  (a serde field without #[serde(default)])
//	default           inside a value struct without #[serde(default)]:
//	                  this field has a field-level serde default
//	nolayer           serialized and in the schema, but never applied
//	                  from a layer (#[wayle(skip)] with serde, i.e.
//	                  `imports`)
//
// `cfg:"-"` marks an exported field that is not part of the schema.
// Every exported field of a schema struct carries one of the two; the
// schema tests enforce it.

// Leaf types are decoded wholesale: a layer replaces the whole value,
// the way a ConfigProperty<T> holds one T. Every non-struct type is a
// leaf; a struct is a leaf when it implements Unmarshaler or is marked
// as a value struct (a plain serde struct held by one ConfigProperty,
// like SurfaceAnimation or DropdownSize). Any other struct is a
// container whose fields are properties of their own.
type valueStruct interface {
	// configValue marks a struct decoded as one value.
	configValue()
}

// Unmarshaler is implemented by leaf types with their own decoding
// from the layer tree: string | int64 | float64 | bool | []any |
// map[string]any. The error text becomes the diagnostic's Error line,
// so it follows serde's wording where the Rust type is a serde type.
type Unmarshaler interface {
	UnmarshalConfig(v any) error
}

// Marshaler is implemented by leaf types that serialize to something
// other than their reflected shape (a string union, a newtype).
type Marshaler interface {
	MarshalConfig() any
}

// fieldInfo is one schema field of a struct, with inline groups
// flattened into their parent.
type fieldInfo struct {
	index      []int
	key        string
	aliases    []string
	deprecated []string
	required   bool
	hasDefault bool
	nolayer    bool
	typ        reflect.Type
	// goName is the Go field name, for docs lookup.
	goName string
	// owner is the struct type that declares the field (differs from
	// the walked type for inline groups).
	owner reflect.Type
}

// lookupKeys is the canonical key, then aliases, then deprecated
// aliases — serde_keys' order.
func (f *fieldInfo) lookupKeys() []string {
	keys := make([]string, 0, 1+len(f.aliases)+len(f.deprecated))
	keys = append(keys, f.key)
	keys = append(keys, f.aliases...)
	keys = append(keys, f.deprecated...)
	return keys
}

func (f *fieldInfo) isDeprecated(key string) bool {
	return slices.Contains(f.deprecated, key)
}

var (
	unmarshalerType = reflect.TypeFor[Unmarshaler]()
	marshalerType   = reflect.TypeFor[Marshaler]()
	valueStructType = reflect.TypeFor[valueStruct]()
)

var fieldCache sync.Map // reflect.Type -> []fieldInfo

// fieldsOf returns the schema fields of struct type t in declaration
// order. A malformed tag is a programmer error and panics on first use
// (the schema tests walk every type, so it never reaches a user).
func fieldsOf(t reflect.Type) []fieldInfo {
	if cached, ok := fieldCache.Load(t); ok {
		fields, _ := cached.([]fieldInfo)
		return fields
	}
	fields := collectFields(t, nil)
	fieldCache.Store(t, fields)
	return fields
}

func collectFields(t reflect.Type, prefix []int) []fieldInfo {
	var out []fieldInfo
	for i := range t.NumField() {
		sf := t.Field(i)
		tag, tagged := sf.Tag.Lookup("cfg")
		if !sf.IsExported() {
			if tagged {
				panic(fmt.Sprintf("config: %s.%s: cfg tag on an unexported field", t.Name(), sf.Name))
			}
			continue
		}
		if tag == "-" {
			continue
		}
		if !tagged {
			panic(fmt.Sprintf("config: %s.%s: exported field without a cfg tag", t.Name(), sf.Name))
		}
		index := append(append([]int(nil), prefix...), i)
		key, opts, _ := strings.Cut(tag, ",")
		info := fieldInfo{index: index, key: key, typ: sf.Type, goName: sf.Name, owner: t}
		inline := false
		for opt := range strings.SplitSeq(opts, ",") {
			switch name, value, _ := strings.Cut(opt, "="); name {
			case "":
			case "inline":
				inline = true
			case "required":
				info.required = true
			case "default":
				info.hasDefault = true
			case "nolayer":
				info.nolayer = true
			case "alias":
				info.aliases = append(info.aliases, value)
			case "deprecated":
				info.deprecated = append(info.deprecated, value)
			default:
				panic(fmt.Sprintf("config: %s.%s: unknown cfg option %q", t.Name(), sf.Name, name))
			}
		}
		if inline {
			if key != "" || sf.Type.Kind() != reflect.Struct {
				panic(fmt.Sprintf("config: %s.%s: inline needs an unnamed struct field", t.Name(), sf.Name))
			}
			out = append(out, collectFields(sf.Type, index)...)
			continue
		}
		if key == "" {
			panic(fmt.Sprintf("config: %s.%s: empty cfg key", t.Name(), sf.Name))
		}
		out = append(out, info)
	}
	return out
}

// isLeaf reports whether values of t are decoded wholesale.
func isLeaf(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return true
	}
	return reflect.PointerTo(t).Implements(unmarshalerType) ||
		t.Implements(valueStructType) || reflect.PointerTo(t).Implements(valueStructType)
}

// isContainer reports whether t is a config section: a struct whose
// fields are properties with their own layers.
func isContainer(t reflect.Type) bool { return !isLeaf(t) }
