package cli

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
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
	v, msg := parseRustI64(raw)
	if msg != "" {
		return nil, valueValidationError(a.String(), raw, msg)
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
	v, msg := parseRustF64(raw)
	if msg != "" {
		return nil, valueValidationError(a.String(), raw, msg)
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

// parseRustI64 is str::parse::<i64>: an optional sign, ASCII digits,
// and Rust's ParseIntError messages.
func parseRustI64(s string) (int64, string) {
	if s == "" {
		return 0, "cannot parse integer from empty string"
	}
	digits := s
	if digits[0] == '+' || digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" {
		return 0, "invalid digit found in string"
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, "invalid digit found in string"
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		if strings.HasPrefix(s, "-") {
			return 0, "number too small to fit in target type"
		}
		return 0, "number too large to fit in target type"
	}
	if err != nil {
		return 0, "invalid digit found in string"
	}
	return v, ""
}

// rustFloat is f64::from_str's grammar: decimal digits with an optional
// fraction and exponent, or inf/infinity/nan in any case, all with an
// optional sign. Unlike strconv it takes no hex floats or underscores.
var rustFloat = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|(?i:inf|infinity|nan))$`)

func parseRustF64(s string) (float64, string) {
	if s == "" {
		return 0, "cannot parse float from empty string"
	}
	if !rustFloat.MatchString(s) {
		return 0, "invalid float literal"
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, "invalid float literal"
	}
	return v, "" // out of range is ±inf or 0, as in Rust
}
