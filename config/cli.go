package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/version"
)

// Helpers behind `wayle config` (wayle/src/cli/config), kept here so
// the output forms live next to the tree they format.

// FormatGetValue renders a `wayle config get` result: scalars bare
// (strings unquoted, floats in Rust's f64 Display), tables as a pretty
// TOML document, arrays inline.
func FormatGetValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		return rustFloat(t, 64)
	case float32:
		return rustFloat(float64(t), 32)
	case bool:
		return strconv.FormatBool(t)
	case datetime:
		return string(t)
	}
	if s, err := tomlPretty(v); err == nil {
		return s
	}
	return TOMLInline(v)
}

// ParseCLIValue reads a `wayle config set` value as a TOML value
// ("true", "5", "[1, 2]", "{ a = 1 }", "\"quoted\""); anything that is
// not valid TOML is taken as a plain string.
func ParseCLIValue(raw string) any {
	tree, err := parseTOML([]byte("value = " + raw))
	if err != nil {
		return raw
	}
	if v, ok := lookup(tree, "value"); ok {
		return v
	}
	return raw
}

// DefaultTOML renders the default configuration as pretty TOML (the
// `wayle config default` output).
func DefaultTOML() string {
	out, _ := tomlPretty(encode(Defaults()))
	return out
}

// ExamplePath is config.toml.example in dir.
func ExamplePath(dir string) string { return filepath.Join(dir, "config.toml.example") }

// SchemaPath is schema.json in dir.
func SchemaPath(dir string) string { return filepath.Join(dir, "schema.json") }

// TombiPath is tombi.toml in dir.
func TombiPath(dir string) string { return filepath.Join(dir, "tombi.toml") }

// tombiConfig associates the generated schemas with the config files
// for Taplo/tombi editors (schema.rs TOMBI_CONFIG).
const tombiConfig = `toml-version = "v1.1.0"

[schema]
enabled = true

[[schemas]]
path = "./schema.json"
include = ["config.toml", "runtime.toml"]

[[schemas]]
path = "./themes/schema.json"
include = ["themes/*.toml"]
`

// EnsureSchemaCurrent writes schema.json when it is missing or from
// another version, and tombi.toml when it differs (ensure_schema_current).
func EnsureSchemaCurrent(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the user's config dir
		return err
	}
	schemaPath := SchemaPath(dir)
	existing, err := os.ReadFile(schemaPath) //nolint:gosec // schema.json in the config dir
	if err != nil || !strings.Contains(string(existing), "wayle-config-"+version.Version) {
		if err := os.WriteFile(schemaPath, []byte(GenerateSchema()), 0o644); err != nil { //nolint:gosec // an editor schema, not secret
			return err
		}
	}
	tombi := TombiPath(dir)
	current, err := os.ReadFile(tombi) //nolint:gosec // tombi.toml in the config dir
	if err == nil && string(current) == tombiConfig {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(tombi, []byte(tombiConfig), 0o644) //nolint:gosec // an editor config, not secret
}
