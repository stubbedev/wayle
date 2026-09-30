package i18n

import "fmt"

// The FTL abstract syntax the runtime parser produces: the subset of
// fluent-syntax 0.12's ast that fluent-bundle resolves. Comments are
// dropped (the runtime parser skips them), junk is kept so tests can
// assert a resource parsed cleanly.

// resource is one parsed FTL source.
type resource struct {
	entries []entry
	// junk holds the source slices the parser could not read, in
	// order, each paired with the error that started it.
	junk []junkEntry
}

type junkEntry struct {
	content string
	err     *parseError
}

// entry is a message or a term.
type entry struct {
	term  bool
	id    string
	value *pattern // never nil for a term
	attrs []attribute
}

type attribute struct {
	id    string
	value *pattern
}

// pattern is a sequence of text and placeables.
type pattern struct {
	elements []element
}

// element is either literal text (expr nil) or a placeable.
type element struct {
	text string
	expr expression
}

// expression is an inline expression or a select expression.
type expression interface{ isExpression() }

// inlineExpr is the closed set of inline expressions.
type inlineExpr interface {
	expression
	isInline()
}

type (
	stringLiteral struct{ raw string } // escapes still in place
	numberLiteral struct{ raw string }
	messageRef    struct{ id, attr string } // attr "" when absent
	termRef       struct {
		id, attr string
		args     *callArgs // nil when the reference has no parentheses
	}
	variableRef struct{ id string }
	functionRef struct {
		id   string
		args callArgs
	}
	nestedPlaceable struct{ expr expression }
	selectExpr      struct {
		selector inlineExpr
		variants []variant
	}
)

type callArgs struct {
	positional []inlineExpr
	named      []namedArg
}

type namedArg struct {
	name  string
	value inlineExpr // a string or number literal
}

// variant is one [key] arm of a select expression; numeric keys keep
// their source spelling, which fixes their fraction digits.
type variant struct {
	key       string
	numberKey bool
	value     *pattern
	isDefault bool
}

func (stringLiteral) isExpression()   {}
func (numberLiteral) isExpression()   {}
func (messageRef) isExpression()      {}
func (termRef) isExpression()         {}
func (variableRef) isExpression()     {}
func (functionRef) isExpression()     {}
func (nestedPlaceable) isExpression() {}
func (selectExpr) isExpression()      {}

func (stringLiteral) isInline()   {}
func (numberLiteral) isInline()   {}
func (messageRef) isInline()      {}
func (termRef) isInline()         {}
func (variableRef) isInline()     {}
func (functionRef) isInline()     {}
func (nestedPlaceable) isInline() {}

// parseError is one fluent-syntax ErrorKind at a byte offset.
type parseError struct {
	kind string
	pos  int
}

func (e *parseError) Error() string { return fmt.Sprintf("ftl: %s at byte %d", e.kind, e.pos) }
