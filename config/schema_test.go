package config

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config/internal/schemadoc"
)

// notPortedSections are the root keys of the Rust schema the Go config
// does not carry yet. The list only shrinks: TestSchemaCoversEveryRootSection
// fails when a section is ported without being removed here.
var notPortedSections = map[string]bool{}

func loadRustSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../schema/wayle-config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func loadGoSchema(t *testing.T) map[string]any {
	t.Helper()
	var schema map[string]any
	dec := json.NewDecoder(strings.NewReader(GenerateSchema()))
	dec.UseNumber()
	if err := dec.Decode(&schema); err != nil {
		t.Fatalf("generated schema is not JSON: %v", err)
	}
	return schema
}

// TestSchemaDefsMatchRust holds every $def the Go schema produces to
// the Rust schema in schema/: same names, types, defaults, and
// descriptions. The Rust file is the oracle for key completeness.
func TestSchemaDefsMatchRust(t *testing.T) {
	rust := loadRustSchema(t)["$defs"].(map[string]any)
	goDefs := loadGoSchema(t)["$defs"].(map[string]any)
	names := make([]string, 0, len(goDefs))
	for name := range goDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want, ok := rust[name]
		if !ok {
			t.Errorf("$defs/%s: not in the Rust schema", name)
			continue
		}
		diffJSON(t, "$defs/"+name, goDefs[name], want)
	}
}

// TestSchemaRootMatchesRust compares the root object and each ported
// section's property, and requires every unported section to be
// listed.
func TestSchemaRootMatchesRust(t *testing.T) {
	rust := loadRustSchema(t)
	goSchema := loadGoSchema(t)
	for _, key := range []string{"$schema", "$id", "title", "description", "type"} {
		diffJSON(t, key, goSchema[key], rust[key])
	}
	rustProps := rust["properties"].(map[string]any)
	goProps := goSchema["properties"].(map[string]any)
	for key, want := range rustProps {
		got, ok := goProps[key]
		switch {
		case !ok && !notPortedSections[key]:
			t.Errorf("root section %q is in the Rust schema but not in Go (port it or list it in notPortedSections)", key)
		case ok && notPortedSections[key]:
			t.Errorf("root section %q is ported; remove it from notPortedSections", key)
		case ok:
			diffJSON(t, "properties/"+key, got, want)
		}
	}
	for key := range goProps {
		if _, ok := rustProps[key]; !ok {
			t.Errorf("root section %q is not in the Rust schema", key)
		}
	}
}

// TestSchemaDiffCatchesMissingKeys proves the comparison is not
// vacuous: dropping one property from a def is reported.
func TestSchemaDiffCatchesMissingKeys(t *testing.T) {
	rust := loadRustSchema(t)["$defs"].(map[string]any)
	battery := rust["BatteryConfig"].(map[string]any)
	trimmed := map[string]any{}
	maps.Copy(trimmed, battery)
	props := map[string]any{}
	for k, v := range battery["properties"].(map[string]any) {
		if k != "format" {
			props[k] = v
		}
	}
	trimmed["properties"] = props
	rec := &recorder{}
	diffJSONInto(rec, "BatteryConfig", trimmed, battery)
	if len(rec.errs) == 0 {
		t.Fatal("a def missing the format property compared equal")
	}
}

type recorder struct{ errs []string }

func (r *recorder) Errorf(format string, args ...any) { r.errs = append(r.errs, format) }

type errorfer interface {
	Errorf(format string, args ...any)
}

func diffJSON(t *testing.T, path string, got, want any) {
	t.Helper()
	diffJSONInto(t, path, got, want)
}

func diffJSONInto(t errorfer, path string, got, want any) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: got %s, want an object", path, short(got))
			return
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok {
				t.Errorf("%s: missing key %q (want %s)", path, k, short(wv))
				continue
			}
			diffJSONInto(t, path+"/"+k, gv, wv)
		}
		for k, gv := range g {
			if _, ok := w[k]; !ok {
				t.Errorf("%s: extra key %q = %s", path, k, short(gv))
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: got %s, want %s", path, short(got), short(want))
			return
		}
		for i := range w {
			diffJSONInto(t, path+"/"+itoa(i), g[i], w[i])
		}
	default:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %s, want %s", path, short(got), short(want))
		}
	}
}

func short(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestSchemaDocsAreCurrent fails when schemadoc_gen.go is stale:
// run `go generate ./config`.
func TestSchemaDocsAreCurrent(t *testing.T) {
	want, err := schemadoc.Generate(".", "config")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(schemadoc.OutputName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schemadoc_gen.go is stale; run `go generate ./config`")
	}
}
