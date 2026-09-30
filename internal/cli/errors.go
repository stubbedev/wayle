package cli

import (
	"strconv"
	"strings"
	"unicode"
)

// errKind is the clap ErrorKind subset the tree can raise.
type errKind int

const (
	kindDisplayHelp errKind = iota
	kindDisplayHelpOnMissing
	kindDisplayVersion
	kindUnknownArgument
	kindInvalidSubcommand
	kindInvalidValue
	kindValueValidation
	kindMissingRequired
	kindArgumentConflict
	kindTooManyValues
	kindMissingSubcommand
)

// Error is a parse outcome that ends the run before a handler: help
// or version output (exit 0), or a usage error (exit 2). It renders
// as clap's RichFormatter does.
type Error struct {
	kind errKind
	// message is the pre-rendered help or version text.
	message string

	invalidArg     string
	invalidValue   string
	validValues    []string
	invalidSub     string
	suggestedSubs  []string
	suggestedArg   string
	suggestedValue string
	tips           []string // styled "tip:" suggestions
	usage          string   // styled "Usage: ..." line, "" for none
	required       []string
	source         string // the value parser's message
	helpFlag       string // the "try '--help'" hint, "" for none
}

// Error renders without styling so an *Error reads as a plain error.
func (e *Error) Error() string { return strip(e.render()) }

// ExitCode is clap's: 0 for help/version, 2 for everything else.
func (e *Error) ExitCode() int {
	if e.kind == kindDisplayHelp || e.kind == kindDisplayVersion {
		return 0
	}
	return 2
}

// toStdout reports whether the output belongs on stdout.
func (e *Error) toStdout() bool { return e.ExitCode() == 0 }

// helpFlagFor is clap's get_help_flag.
func helpFlagFor(c *cmd) string {
	switch {
	case !c.helpFlagOff:
		return "--help"
	case len(c.subs) > 0 && !c.helpSubOff:
		return "help"
	default:
		return ""
	}
}

func newError(kind errKind, c *cmd) *Error {
	return &Error{kind: kind, helpFlag: helpFlagFor(c)}
}

func unknownArgumentError(c *cmd, argument, suggestion string, trailingTip bool, usage string) *Error {
	e := newError(kindUnknownArgument, c)
	e.invalidArg = argument
	e.usage = usage
	if trailingTip {
		e.tips = append(e.tips, "to pass '"+styleInvalid.wrap(argument)+"' as a value, use '"+styleValid.wrap("-- "+argument)+"'")
	}
	e.suggestedArg = suggestion
	return e
}

func invalidSubcommandError(c *cmd, sub string, suggestions []string, usage string) *Error {
	e := newError(kindInvalidSubcommand, c)
	e.invalidSub = sub
	e.suggestedSubs = suggestions
	e.usage = usage
	return e
}

func invalidValueError(c *cmd, value string, valid []string, argument string) *Error {
	e := newError(kindInvalidValue, c)
	e.invalidArg = argument
	e.invalidValue = value
	e.validValues = valid
	if s := didYouMean(value, valid); len(s) > 0 {
		e.suggestedValue = s[len(s)-1]
	}
	return e
}

// valueValidationError has no command yet; the parser attaches it.
func valueValidationError(argument, value, source string) *Error {
	return &Error{kind: kindValueValidation, invalidArg: argument, invalidValue: value, source: source}
}

func missingRequiredError(c *cmd, required []string, usage string) *Error {
	e := newError(kindMissingRequired, c)
	e.required = required
	e.usage = usage
	return e
}

func conflictSelfError(c *cmd, argument, usage string) *Error {
	e := newError(kindArgumentConflict, c)
	e.invalidArg = argument
	e.usage = usage
	return e
}

func tooManyValuesError(c *cmd, value, argument, usage string) *Error {
	e := newError(kindTooManyValues, c)
	e.invalidArg = argument
	e.invalidValue = value
	e.usage = usage
	return e
}

func missingSubcommandError(c *cmd, usage string) *Error {
	e := newError(kindMissingSubcommand, c)
	e.invalidSub = c.bin
	e.validValues = c.subNames()
	e.usage = usage
	return e
}

func displayError(kind errKind, message string) *Error {
	return &Error{kind: kind, message: message}
}

// render is RichFormatter::format_error.
func (e *Error) render() string {
	if e.kind == kindDisplayHelp || e.kind == kindDisplayHelpOnMissing || e.kind == kindDisplayVersion {
		return e.message
	}
	var b strings.Builder
	b.WriteString(styleError.wrap("error:"))
	b.WriteString(" ")
	b.WriteString(e.dynamicContext())

	suggested := false
	tip := func(text string) {
		b.WriteString("\n")
		if !suggested {
			b.WriteString("\n")
			suggested = true
		}
		b.WriteString("  " + styleValid.wrap("tip:") + " " + text)
	}
	if len(e.suggestedSubs) > 0 {
		quoted := make([]string, len(e.suggestedSubs))
		for i, s := range e.suggestedSubs {
			quoted[i] = "'" + styleValid.wrap(s) + "'"
		}
		if len(quoted) == 1 {
			tip("a similar subcommand exists: " + quoted[0])
		} else {
			tip("some similar subcommands exist: " + strings.Join(quoted, ", "))
		}
	}
	if e.suggestedArg != "" {
		tip("a similar argument exists: '" + styleValid.wrap(e.suggestedArg) + "'")
	}
	if e.suggestedValue != "" {
		tip("a similar value exists: '" + styleValid.wrap(e.suggestedValue) + "'")
	}
	if len(e.tips) > 0 {
		if !suggested {
			b.WriteString("\n")
		}
		for _, t := range e.tips {
			b.WriteString("\n  " + styleValid.wrap("tip:") + " " + t)
		}
	}
	if e.usage != "" {
		b.WriteString("\n\n")
		b.WriteString(e.usage)
	}
	if e.helpFlag != "" {
		b.WriteString("\n\nFor more information, try '" + styleLiteral.wrap(e.helpFlag) + "'.\n")
	} else {
		b.WriteString("\n")
	}
	return b.String()
}

func (e *Error) dynamicContext() string {
	switch e.kind {
	case kindArgumentConflict:
		return "the argument '" + styleInvalid.wrap(e.invalidArg) + "' cannot be used multiple times"
	case kindInvalidValue:
		var b strings.Builder
		if e.invalidValue == "" {
			b.WriteString("a value is required for '" + styleInvalid.wrap(e.invalidArg) + "' but none was supplied")
		} else {
			b.WriteString("invalid value '" + styleInvalid.wrap(e.invalidValue) + "' for '" + styleLiteral.wrap(e.invalidArg) + "'")
		}
		writeValuesList(&b, "possible values", e.validValues)
		return b.String()
	case kindMissingSubcommand:
		var b strings.Builder
		b.WriteString("'" + styleInvalid.wrap(e.invalidSub) + "' requires a subcommand but one was not provided")
		writeValuesList(&b, "subcommands", e.validValues)
		return b.String()
	case kindInvalidSubcommand:
		return "unrecognized subcommand '" + styleInvalid.wrap(e.invalidSub) + "'"
	case kindMissingRequired:
		var b strings.Builder
		b.WriteString("the following required arguments were not provided:")
		for _, r := range e.required {
			b.WriteString("\n  " + styleValid.wrap(r))
		}
		return b.String()
	case kindTooManyValues:
		return "unexpected value '" + styleInvalid.wrap(e.invalidValue) + "' for '" + styleLiteral.wrap(e.invalidArg) + "' found; no more were expected"
	case kindValueValidation:
		return "invalid value '" + styleInvalid.wrap(e.invalidValue) + "' for '" + styleLiteral.wrap(e.invalidArg) + "': " + e.source
	case kindUnknownArgument:
		return "unexpected argument '" + styleInvalid.wrap(e.invalidArg) + "' found"
	default:
		return ""
	}
}

// writeValuesList is format.rs's write_values_list.
func writeValuesList(b *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		return
	}
	b.WriteString("\n  [" + name + ": ")
	for i, v := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(styleValid.wrap(quoteIfSpaced(v)))
	}
	b.WriteString("]")
}

// quoteIfSpaced is clap's Escape: a value containing whitespace is
// shown Debug-quoted.
func quoteIfSpaced(v string) string {
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return strconv.Quote(v)
	}
	return v
}
