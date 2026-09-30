package cli

import (
	"strconv"

	"github.com/stubbedev/wayle/internal/rustparse"
)

// ValueParser turns one raw command-line value into its typed form
// (clap's value_parser!), so a handler never re-validates a string.
type ValueParser interface {
	parse(c *cmd, a *arg, raw string) (any, *Error)
	possibleValues() []PossibleValue
}

// PossibleValue is one accepted value of an enumerated arg; Help shows
// in --help's "Possible values:" list.
type PossibleValue struct {
	Name string
	Help string
}

type stringParser struct{}

func (stringParser) parse(_ *cmd, _ *arg, raw string) (any, *Error) { return raw, nil }
func (stringParser) possibleValues() []PossibleValue                { return nil }

// String accepts any value (value_parser!(String)).
var String ValueParser = stringParser{}

type pathParser struct{}

func (pathParser) parse(c *cmd, a *arg, raw string) (any, *Error) {
	if raw == "" {
		return nil, invalidValueError(c, "", nil, a.String())
	}
	return raw, nil
}
func (pathParser) possibleValues() []PossibleValue { return nil }

// Path accepts any non-empty value (value_parser!(PathBuf)); an empty
// one is "a value is required".
var Path ValueParser = pathParser{}

type u32Parser struct{}

func (u32Parser) parse(_ *cmd, a *arg, raw string) (any, *Error) {
	v, err := rustparse.Int(raw, 64)
	if err != nil {
		return nil, valueValidationError(a.String(), raw, err.Error())
	}
	if v < 0 || v > 1<<32-1 {
		return nil, valueValidationError(a.String(), raw, strconv.FormatInt(v, 10)+" is not in 0..=4294967295")
	}
	return uint32(v), nil
}
func (u32Parser) possibleValues() []PossibleValue { return nil }

// U32 parses a u32 the way clap's RangedI64ValueParser<u32> does: an
// i64 parse (Rust's messages) and then the range check.
var U32 ValueParser = u32Parser{}

type f64Parser struct{}

func (f64Parser) parse(_ *cmd, a *arg, raw string) (any, *Error) {
	v, err := rustparse.Float(raw)
	if err != nil {
		return nil, valueValidationError(a.String(), raw, err.Error())
	}
	return v, nil
}
func (f64Parser) possibleValues() []PossibleValue { return nil }

// F64 parses an f64 with Rust's f64::from_str grammar and messages.
var F64 ValueParser = f64Parser{}

type enumParser struct{ values []PossibleValue }

func (e enumParser) parse(c *cmd, a *arg, raw string) (any, *Error) {
	names := make([]string, len(e.values))
	for i, pv := range e.values {
		if pv.Name == raw {
			return raw, nil
		}
		names[i] = pv.Name
	}
	return nil, invalidValueError(c, raw, names, a.String())
}
func (e enumParser) possibleValues() []PossibleValue { return e.values }

// Enum accepts exactly one of values (a clap ValueEnum); the parsed
// value is the matching Name.
func Enum(values ...PossibleValue) ValueParser { return enumParser{values: values} }
