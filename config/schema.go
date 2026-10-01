package config

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/version"
)

// JSON Schema generation by reflection over the Config tree, producing
// what schemars 1.x derives from the Rust types
// (crates/wayle-config/src/infrastructure/schema.rs): the same $defs
// names (Array_of_T, Nullable_T, Map_of_T, the primitive names), the
// same inline-vs-$ref choices, per-property defaults taken from each
// type's default instance, and descriptions from the doc comments
// (schemadoc_gen.go). A section added to Config is covered by
// construction; schema_test.go holds the output against the Rust
// schema in schema/.

// Schema is one JSON Schema object.
type Schema = map[string]any

// schemaProvider is implemented by leaf types with a hand-written
// schema (the Rust types with a manual JsonSchema impl). Such types
// are defined under their name, deduplicated by name only.
type schemaProvider interface {
	configSchema(g *schemaGen) Schema
}

// schemaNamer overrides a type's def name when the Go name differs
// from the Rust one.
type schemaNamer interface {
	configSchemaName() string
}

// noStructDefault marks a value struct without #[serde(default)]: its
// properties carry no defaults unless tagged `default`, and plain
// fields are required unless optional.
type noStructDefault interface {
	noStructDefault()
}

var (
	schemaProviderType  = reflect.TypeFor[schemaProvider]()
	schemaNamerType     = reflect.TypeFor[schemaNamer]()
	noStructDefaultType = reflect.TypeFor[noStructDefault]()
)

// implements reports whether values of the non-pointer type t (or
// their address) implement iface; an Option (*T) never does itself.
func implements(t, iface reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return false
	}
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// zeroOf returns an addressable zero value of t.
func zeroOf(t reflect.Type) reflect.Value { return reflect.New(t).Elem() }

// schemaName is the schemars schema_name of t.
func schemaName(t reflect.Type) string {
	if implements(t, schemaNamerType) {
		n, _ := reflect.TypeAssert[schemaNamer](zeroOf(t).Addr())
		return n.configSchemaName()
	}
	if isNamedType(t) {
		return t.Name()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "Nullable_" + schemaName(t.Elem())
	case reflect.Slice:
		return "Array_of_" + schemaName(t.Elem())
	case reflect.Map:
		return "Map_of_" + schemaName(t.Elem())
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	}
	return primitiveFormat(t.Kind())
}

// primitiveFormat is schemars' name (and "format") of a numeric kind.
func primitiveFormat(k reflect.Kind) string {
	switch k {
	case reflect.Int8:
		return "int8"
	case reflect.Int16:
		return "int16"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Int:
		return "int"
	case reflect.Uint8:
		return "uint8"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Uint:
		return "uint"
	case reflect.Float32:
		return "float"
	case reflect.Float64:
		return "double"
	}
	return k.String()
}

// isNamedType reports whether plain uses of t are a $ref to a def
// (structs, enums, hand-written schemas) rather than inlined.
func isNamedType(t reflect.Type) bool {
	if implements(t, schemaProviderType) || enumVariants(t) != nil {
		return true
	}
	return t.Kind() == reflect.Struct
}

type schemaGen struct {
	defs  map[string]Schema
	ids   map[string]string
	names map[string]bool
	// defaults maps each container type to its default instance, from
	// the walk of Defaults().
	defaults map[reflect.Type]reflect.Value
}

// GenerateSchema returns the JSON Schema of the whole config, compact
// with sorted keys (the `wayle config schema --stdout` output).
func GenerateSchema() string {
	return marshalJSON(generateSchemaTree())
}

func generateSchemaTree() Schema {
	g := &schemaGen{
		defs:     map[string]Schema{},
		ids:      map[string]string{},
		names:    map[string]bool{},
		defaults: map[reflect.Type]reflect.Value{},
	}
	root := reflect.ValueOf(Defaults()).Elem()
	collectContainerDefaults(root, g.defaults)
	schema := g.structSchema(root.Type())
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = schemaName(root.Type())
	schema["$id"] = "wayle-config-" + version.Version
	if len(g.defs) > 0 {
		defs := Schema{}
		for k, v := range g.defs {
			defs[k] = v
		}
		schema["$defs"] = defs
	}
	return schema
}

// collectContainerDefaults records the default instance of every
// container type reachable from v.
func collectContainerDefaults(v reflect.Value, out map[reflect.Type]reflect.Value) {
	t := v.Type()
	if _, seen := out[t]; seen {
		return
	}
	out[t] = v
	for _, f := range fieldsOf(t) {
		if isContainer(f.typ) {
			collectContainerDefaults(v.FieldByIndex(f.index), out)
		}
	}
}

// define returns the $ref to the def with the given identity,
// generating it on first use. Names collide only between distinct
// identities (a derived type and a ConfigProperty of it), which get
// schemars' numeric suffix in first-come order.
func (g *schemaGen) define(id, name string, build func() Schema) Schema {
	if existing, ok := g.ids[id]; ok {
		return Schema{"$ref": "#/$defs/" + existing}
	}
	final := name
	for n := 2; g.names[final]; n++ {
		final = name + strconv.Itoa(n)
	}
	g.ids[id] = final
	g.names[final] = true
	g.defs[final] = Schema{}
	g.defs[final] = build()
	return Schema{"$ref": "#/$defs/" + final}
}

// propertyRef is the schema of a ConfigProperty<T> field: always a
// def named after T, keyed by that name.
func (g *schemaGen) propertyRef(t reflect.Type) Schema {
	name := schemaName(t)
	return g.define("prop:"+name, name, func() Schema { return g.plainSchema(t) })
}

// subschema is a plain use of t: primitives, options, arrays, and maps
// inline; named types as a $ref.
func (g *schemaGen) subschema(t reflect.Type) Schema {
	if !isNamedType(t) {
		return g.plainSchema(t)
	}
	id := "derived:" + t.PkgPath() + "." + t.Name()
	if implements(t, schemaProviderType) {
		id = "prop:" + schemaName(t)
	}
	return g.define(id, schemaName(t), func() Schema { return g.plainSchema(t) })
}

// plainSchema is T::json_schema.
func (g *schemaGen) plainSchema(t reflect.Type) Schema {
	if implements(t, schemaProviderType) {
		p, _ := reflect.TypeAssert[schemaProvider](zeroOf(t).Addr())
		return p.configSchema(g)
	}
	if variants := enumVariants(t); variants != nil {
		return g.enumSchema(t, variants)
	}
	switch t.Kind() {
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.String:
		return Schema{"type": "string"}
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		return Schema{"type": "integer", "format": primitiveFormat(t.Kind())}
	case reflect.Uint8:
		return Schema{"type": "integer", "format": "uint8", "minimum": int64(0), "maximum": int64(math.MaxUint8)}
	case reflect.Uint16:
		return Schema{"type": "integer", "format": "uint16", "minimum": int64(0), "maximum": int64(math.MaxUint16)}
	case reflect.Uint32, reflect.Uint64, reflect.Uint:
		return Schema{"type": "integer", "format": primitiveFormat(t.Kind()), "minimum": int64(0)}
	case reflect.Float32, reflect.Float64:
		return Schema{"type": "number", "format": primitiveFormat(t.Kind())}
	case reflect.Pointer:
		return nullable(g.subschema(t.Elem()))
	case reflect.Slice:
		return Schema{"type": "array", "items": g.subschema(t.Elem())}
	case reflect.Map:
		return Schema{"type": "object", "additionalProperties": g.subschema(t.Elem())}
	case reflect.Struct:
		return g.structSchema(t)
	}
	panic(fmt.Sprintf("config: no schema for %s", t))
}

// nullable is schemars' Option<T>: a $ref becomes anyOf [ref, null],
// an inline type gains "null".
func nullable(s Schema) Schema {
	if _, isRef := s["$ref"]; isRef {
		return Schema{"anyOf": []any{s, Schema{"type": "null"}}}
	}
	out := Schema{}
	maps.Copy(out, s)
	switch typ := s["type"].(type) {
	case string:
		out["type"] = []any{typ, "null"}
	case []any:
		out["type"] = append(append([]any(nil), typ...), "null")
	default:
		return Schema{"anyOf": []any{s, Schema{"type": "null"}}}
	}
	return out
}

func (g *schemaGen) enumSchema(t reflect.Type, variants []string) Schema {
	oneOf := make([]any, len(variants))
	for i, v := range variants {
		entry := Schema{"const": v, "type": "string"}
		if doc := schemaDocs[t.Name()+"="+v]; doc != "" {
			entry["description"] = doc
		}
		oneOf[i] = entry
	}
	s := Schema{"oneOf": oneOf}
	if doc := schemaDocs[t.Name()]; doc != "" {
		s["description"] = doc
	}
	return s
}

func (g *schemaGen) structSchema(t reflect.Type) Schema {
	_, container := g.defaults[t]
	structDefault := container || !implements(t, noStructDefaultType)
	var def reflect.Value
	if container {
		if d, ok := g.defaults[t]; ok {
			def = d
		}
	}
	if !def.IsValid() {
		def = zeroOf(t)
		if d, ok := reflect.TypeAssert[defaulter](def.Addr()); ok {
			d.setDefaults()
		}
	}
	props := Schema{}
	var required []any
	for _, f := range fieldsOf(t) {
		var prop Schema
		switch {
		case f.nolayer || !container:
			prop = copySchema(g.subschema(f.typ))
		case isContainer(f.typ):
			prop = copySchema(g.subschema(f.typ))
		default:
			prop = copySchema(g.propertyRef(f.typ))
		}
		hasDefault := structDefault || f.hasDefault
		if hasDefault {
			prop["default"] = jsonValue(def.FieldByIndex(f.index))
		}
		if doc := schemaDocs[f.owner.Name()+"."+f.goName]; doc != "" {
			prop["description"] = doc
		}
		if !container && !structDefault && f.required {
			required = append(required, f.key)
		}
		props[f.key] = prop
	}
	s := Schema{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	if doc := typeDoc(t); doc != "" {
		s["description"] = doc
	}
	return s
}

// schemaDescriber supplies a type description gofmt would mangle as a
// doc comment (indented lines inside a top-level comment are rewritten
// as a code block).
type schemaDescriber interface {
	configDescription() string
}

var schemaDescriberType = reflect.TypeFor[schemaDescriber]()

func typeDoc(t reflect.Type) string {
	if implements(t, schemaDescriberType) {
		d, _ := reflect.TypeAssert[schemaDescriber](zeroOf(t).Addr())
		return d.configDescription()
	}
	return schemaDocs[t.Name()]
}

func copySchema(s Schema) Schema {
	out := make(Schema, len(s)+2)
	maps.Copy(out, s)
	return out
}

// jsonValue serializes a typed value the way serde_json::to_value
// does for schemars defaults: None is null, f32 widens to f64.
func jsonValue(v reflect.Value) any {
	return toJSONTree(encodeValueMode(v, true))
}

func toJSONTree(v any) any {
	switch t := v.(type) {
	case absent:
		return nil
	case float32:
		return float64(t)
	case *table:
		out := make(map[string]any, len(t.keys))
		for _, k := range t.keys {
			out[k] = toJSONTree(t.vals[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = toJSONTree(child)
		}
		return out
	}
	return v
}

// marshalJSON writes compact JSON with sorted keys and serde_json's
// number forms (a float always carries a fraction or exponent).
func marshalJSON(v any) string {
	var b strings.Builder
	writeJSON(&b, v)
	return b.String()
}

func writeJSON(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case string:
		writeJSONString(b, t)
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case uint64:
		b.WriteString(strconv.FormatUint(t, 10))
	case int:
		b.WriteString(strconv.Itoa(t))
	case float64:
		b.WriteString(jsonFloat(t))
	case []any:
		b.WriteByte('[')
		for i, elem := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, elem)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			writeJSON(b, t[k])
		}
		b.WriteByte('}')
	default:
		panic(fmt.Sprintf("config: no JSON form for %T", v))
	}
}

// jsonFloat formats like serde_json (ryu): shortest round-trip digits,
// integral values with ".0", exponent form outside 1e-5..1e16.
func jsonFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	abs := math.Abs(f)
	if abs != 0 && (abs < 1e-5 || abs >= 1e16) {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		mantissa, exp, _ := strings.Cut(s, "e")
		exp = strings.TrimPrefix(exp, "+")
		if strings.HasPrefix(exp, "-") {
			exp = "-" + strings.TrimLeft(exp[1:], "0")
		} else {
			exp = strings.TrimLeft(exp, "0")
		}
		return mantissa + "e" + exp
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsRune(s, '.') {
		s += ".0"
	}
	return s
}

func writeJSONString(b *strings.Builder, s string) {
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
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
