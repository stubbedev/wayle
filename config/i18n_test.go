package config

import (
	"reflect"
	"testing"

	"github.com/stubbedev/wayle/i18n"
)

// leafPaths lists every leaf field path under t with the struct type
// holding it.
func leafPaths(t reflect.Type, prefix string, visit func(path string, holder reflect.Type)) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for _, f := range fieldsOf(t) {
		path := f.key
		if prefix != "" {
			path = prefix + "." + f.key
		}
		if isContainer(f.typ) {
			leafPaths(f.typ, path, visit)
			continue
		}
		visit(path, t)
	}
}

// Every field of a prefixed struct has its label in the settings FTL,
// as wayle-config's all_i18n_keys_are_populated pins in Rust.
func TestEveryLabeledFieldHasItsFTLKey(t *testing.T) {
	loader := i18n.Settings()
	labeled := 0
	leafPaths(typeOf[Config](), "", func(path string, holder reflect.Type) {
		key, ok := I18nKey(path)
		_, prefixed := i18nPrefixes[schemaName(holder)]
		_, f, _ := fieldAtPath(typeOf[Config](), path)
		if prefixed && !f.noI18n && !ok {
			t.Errorf("%s: no key though %s has a prefix", path, schemaName(holder))
		}
		if !ok {
			return
		}
		labeled++
		if !loader.Has(key) {
			t.Errorf("%s: %s is not in the settings FTL", path, key)
		}
	})
	if labeled < 300 {
		t.Errorf("only %d labeled fields; the walk missed the schema", labeled)
	}
}

func TestI18nKey(t *testing.T) {
	for path, want := range map[string]string{
		"osd.enabled":                 "settings-osd-enabled",
		"modules.battery.level-icons": "settings-modules-battery-level-icons",
		"bar.location":                "settings-bar-location",
	} {
		if got, ok := I18nKey(path); !ok || got != want {
			t.Errorf("I18nKey(%s) = %q %v, want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"osd", "modules.battery", "nope", "osd.enabled.deeper", "styling.palette_base_theme", ""} {
		if got, ok := I18nKey(path); ok {
			t.Errorf("I18nKey(%s) = %q, want none", path, got)
		}
	}
}
