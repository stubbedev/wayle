package config

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/i18n"
)

func TestFieldPicksTheEditorFromTheType(t *testing.T) {
	for path, want := range map[string]FieldMeta{
		"osd.enabled":                         {Kind: FieldBool, Type: "boolean"},
		"osd.duration":                        {Kind: FieldInt, Type: "uint32", Min: 0, Max: math.MaxUint32},
		"bar.background-opacity":              {Kind: FieldInt, Type: "Percentage", Min: 0, Max: 100},
		"styling.scale":                       {Kind: FieldFloat, Type: "ScaleFactor", Min: 0.25, Max: 3},
		"osd.monitor":                         {Kind: FieldText, Type: "OsdMonitor"},
		"modules.weather.visual-crossing-key": {Kind: FieldText, Type: "string", Optional: true},
		"osd.presets":                         {Kind: FieldOther, Type: "Array_of_ToastPreset"},
	} {
		got, ok := Field(path)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("Field(%s) = %+v %v, want %+v", path, got, ok, want)
		}
	}
	pos, _ := Field("osd.position")
	if pos.Kind != FieldEnum || pos.Type != "OsdPosition" || !slices.Contains(pos.Variants, "top-left") || len(pos.Variants) != 8 {
		t.Errorf("osd.position = %+v, want the eight OsdPosition variants", pos)
	}
	// Size is a number or a string: it needs its own editor.
	if m, _ := Field("osd.margin"); m.Kind != FieldOther {
		t.Errorf("osd.margin kind %v, want FieldOther", m.Kind)
	}
	for _, path := range []string{"osd", "nope", "osd.enabled.deeper", ""} {
		if m, ok := Field(path); ok {
			t.Errorf("Field(%s) = %+v, want none", path, m)
		}
	}
}

// enumTypes visits every enum type reachable from the config, list
// items and map values included.
func enumTypes(visit func(t reflect.Type)) {
	seen := map[reflect.Type]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if seen[t] {
			return
		}
		seen[t] = true
		if enumVariants(t) != nil {
			visit(t)
			return
		}
		if t.Kind() == reflect.Struct && isContainer(t) && !implements(t, schemaProviderType) {
			for _, f := range fieldsOf(t) {
				walk(f.typ)
			}
		}
	}
	walk(typeOf[Config]())
}

// unlabeledEnums have no variant labels in Rust either: their options
// show as raw values.
var unlabeledEnums = []string{"RestartPolicy", "VpnShow"}

// Every enum value has its label in the settings FTL, as Rust's
// EnumVariants fluent keys do, and no override is dead.
func TestEveryEnumValueHasItsLabel(t *testing.T) {
	loader := i18n.Settings()
	used := map[string]bool{}
	enums := 0
	enumTypes(func(et reflect.Type) {
		enums++
		name := schemaName(et)
		unlabeled := slices.Contains(unlabeledEnums, name)
		for _, v := range enumVariants(et) {
			key := EnumLabelKey(name, v)
			if _, ok := enumIdents[name+"."+v]; ok {
				used[name+"."+v] = true
				if loader.Has("enum-" + pascalToKebab(name) + "-" + v) {
					t.Errorf("%s.%s: the override is not needed", name, v)
				}
			}
			if has := loader.Has(key); has == unlabeled {
				t.Errorf("%s.%s: %s in the FTL = %v, want %v", name, v, key, has, !unlabeled)
			}
		}
	})
	if enums < 50 {
		t.Errorf("only %d enums; the walk missed the schema", enums)
	}
	for k := range enumIdents {
		if !used[k] {
			t.Errorf("override %s names no variant", k)
		}
	}
}

func TestEnumLabelKey(t *testing.T) {
	for _, c := range [][3]string{
		{"OsdPosition", "top-left", "enum-osd-position-top-left"},
		{"TimeFormat", "12h", "enum-time-format-twelve-hour"},
		{"SessionAction", "log-out", "enum-session-action-logout"},
		{"SessionAction", "lock", "enum-session-action-lock"},
	} {
		if got := EnumLabelKey(c[0], c[1]); got != c[2] {
			t.Errorf("EnumLabelKey(%s, %s) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
