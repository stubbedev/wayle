package config

import "github.com/stubbedev/wayle/strftime"

// StrftimeFormat is a chrono strftime format string compiled at load,
// so an unsupported specifier is a diagnostic for its field instead of
// garbage at render time. In the schema it is the plain String the
// Rust field holds.
type StrftimeFormat struct {
	src    string
	layout *strftime.Layout
}

// ParseStrftimeFormat compiles one format string.
func ParseStrftimeFormat(s string) (StrftimeFormat, error) {
	l, err := strftime.Compile(s)
	if err != nil {
		return StrftimeFormat{}, err
	}
	return StrftimeFormat{src: s, layout: l}, nil
}

// mustStrftime builds a schema-default format; a bad literal panics
// at init.
func mustStrftime(s string) StrftimeFormat {
	f, err := ParseStrftimeFormat(s)
	if err != nil {
		panic(err)
	}
	return f
}

// String is the format as written.
func (f StrftimeFormat) String() string { return f.src }

// Layout is the compiled format.
func (f StrftimeFormat) Layout() *strftime.Layout { return f.layout }

// UnmarshalConfig implements Unmarshaler.
func (f *StrftimeFormat) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	parsed, err := ParseStrftimeFormat(s)
	if err != nil {
		return err
	}
	*f = parsed
	return nil
}

// MarshalConfig implements Marshaler.
func (f StrftimeFormat) MarshalConfig() any { return f.src }

// configSchemaName keeps the Rust def name: the field is a String.
func (StrftimeFormat) configSchemaName() string { return "string" }

func (StrftimeFormat) configSchema(*schemaGen) Schema { return Schema{"type": "string"} }
