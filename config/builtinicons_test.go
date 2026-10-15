package config

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// rustMappings parses a Rust BUILTIN_MAPPINGS table's ("pattern",
// "icon") pairs in order.
func rustMappings(t *testing.T, path string) []IconMapping {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "BUILTIN_MAPPINGS")
	end := strings.Index(body[start:], "];")
	if start < 0 || end < 0 {
		t.Fatalf("%s: no BUILTIN_MAPPINGS table", path)
	}
	pair := regexp.MustCompile(`\("([^"]+)",\s*"([^"]+)"\)`)
	var out []IconMapping
	for _, m := range pair.FindAllStringSubmatch(body[start:start+end], -1) {
		out = append(out, IconMapping{Pattern: m[1], Icon: m[2]})
	}
	return out
}

// The built-in icon tables are the Rust schema's, entry for entry and
// in order (first match wins).
func TestBuiltinIconTablesMatchRust(t *testing.T) {
	for _, tc := range []struct {
		path string
		got  []IconMapping
	}{
		{"../crates/wayle-config/src/schemas/modules/window_title/icons.rs", WindowTitleBuiltinIcons},
		{"../crates/wayle-config/src/schemas/modules/media/icons.rs", MediaBuiltinIcons},
	} {
		want := rustMappings(t, tc.path)
		if len(want) == 0 || !slices.Equal(tc.got, want) {
			t.Errorf("%s: Go table (%d) differs from Rust (%d)", tc.path, len(tc.got), len(want))
		}
	}
}
