package modes

import (
	"context"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/stubbedev/wayle/service/launcher"
)

// Calc is the calculator mode (calc.rs): the query is an expression and
// the first row is its result. rofi needs rofi-calc, a plugin, and qalc
// for this; here it is a mode like any other. Enter copies the result,
// Shift+Enter puts it back in the input so the next expression builds
// on it. The evaluator is deliberately small - arithmetic, parentheses,
// a handful of functions and constants - and does no units; qalc
// through a script mode is still the answer for those.
type Calc struct {
	// result is the last successfully evaluated result, as shown.
	result *string
}

// NewCalc builds the mode.
func NewCalc() *Calc { return &Calc{} }

// Name is "calc".
func (*Calc) Name() string { return "calc" }

func (c *Calc) state() launcher.ModeState {
	var items []launcher.Item
	if c.result != nil {
		r := *c.result
		// PERMANENT: the result is not a row the query searches for but
		// the answer to it; matching "3" against "1+2" would hide it the
		// moment it appeared.
		items = []launcher.Item{{Display: "= " + r, MatchText: r, Info: &r, Flags: launcher.FlagPermanent}}
	}
	return launcher.ModeState{Items: items, Prompt: "calc"}
}

// Load shows the current result, if any.
func (c *Calc) Load(context.Context) launcher.ModeState { return c.state() }

// Query evaluates the query; the list changes only when the answer
// does, so an equivalent spelling does not fight the selection.
func (c *Calc) Query(query string) (launcher.ModeState, bool) {
	var result *string
	if v, ok := Eval(query); ok {
		s := FormatResult(v)
		result = &s
	}
	if (result == nil) == (c.result == nil) && (result == nil || *result == *c.result) {
		return launcher.ModeState{}, false
	}
	c.result = result
	return c.state(), true
}

// Activate copies the result; the alternate accept continues with it.
func (c *Calc) Activate(_ context.Context, _ launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	if c.result == nil {
		return launcher.ActionNothing{}
	}
	if _, alt := kind.(launcher.ActivateAlt); alt {
		return launcher.ActionSetInput{Text: *c.result}
	}
	return launcher.ActionCopy{Text: *c.result}
}

// AllowsCustom is false: typed text is never itself the answer.
func (*Calc) AllowsCustom() bool { return false }

// FormatResult writes a result the way a person would: no trailing
// zeros, no ".0" on a whole number.
func FormatResult(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 10, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// Eval evaluates an expression, false when it is not one. Incomplete
// input is not an error worth reporting: half of what is typed here is
// a half-finished expression, so a failure shows no row. Infinite and
// NaN results are not answers either.
func Eval(input string) (float64, bool) {
	p := &calcParser{rest: strings.TrimSpace(input)}
	v, ok := p.expression()
	if !ok {
		return 0, false
	}
	p.skipSpaces()
	if p.rest != "" || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

// calcParser is a recursive-descent parser over what is left.
type calcParser struct{ rest string }

func (p *calcParser) skipSpaces() { p.rest = strings.TrimLeftFunc(p.rest, unicode.IsSpace) }

func (p *calcParser) eat(tok byte) bool {
	p.skipSpaces()
	if p.rest != "" && p.rest[0] == tok {
		p.rest = p.rest[1:]
		return true
	}
	return false
}

func (p *calcParser) peek() (byte, bool) {
	p.skipSpaces()
	if p.rest == "" {
		return 0, false
	}
	return p.rest[0], true
}

// expression := term (('+' | '-') term)*
func (p *calcParser) expression() (float64, bool) {
	v, ok := p.term()
	for ok {
		c, _ := p.peek()
		switch c {
		case '+':
			p.rest = p.rest[1:]
			var t float64
			t, ok = p.term()
			v += t
		case '-':
			p.rest = p.rest[1:]
			var t float64
			t, ok = p.term()
			v -= t
		default:
			return v, true
		}
	}
	return 0, false
}

// term := power (('*' | '/' | '%') power)*
func (p *calcParser) term() (float64, bool) {
	v, ok := p.power()
	for ok {
		c, _ := p.peek()
		var t float64
		switch c {
		case '*':
			p.rest = p.rest[1:]
			t, ok = p.power()
			v *= t
		case '/':
			p.rest = p.rest[1:]
			t, ok = p.power()
			v /= t
		case '%':
			p.rest = p.rest[1:]
			t, ok = p.power()
			v = math.Mod(v, t)
		default:
			return v, true
		}
	}
	return 0, false
}

// power := unary ('^' power)? - right associative, as in maths.
func (p *calcParser) power() (float64, bool) {
	base, ok := p.unary()
	if !ok {
		return 0, false
	}
	if p.eat('^') {
		exp, ok := p.power()
		if !ok {
			return 0, false
		}
		return math.Pow(base, exp), true
	}
	return base, true
}

// unary := ('-' | '+') unary | atom
func (p *calcParser) unary() (float64, bool) {
	c, ok := p.peek()
	if !ok {
		return 0, false
	}
	switch c {
	case '-':
		p.rest = p.rest[1:]
		v, ok := p.unary()
		return -v, ok
	case '+':
		p.rest = p.rest[1:]
		return p.unary()
	}
	return p.atom()
}

// atom := number | '(' expression ')' | name ['(' expression ')']
func (p *calcParser) atom() (float64, bool) {
	if p.eat('(') {
		v, ok := p.expression()
		if !ok || !p.eat(')') {
			return 0, false
		}
		return v, true
	}
	c, ok := p.peek()
	switch {
	case !ok:
		return 0, false
	case c >= '0' && c <= '9' || c == '.':
		return p.number()
	case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		return p.named()
	}
	return 0, false
}

func (p *calcParser) number() (float64, bool) {
	p.skipSpaces()
	end := strings.IndexFunc(p.rest, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if end < 0 {
		end = len(p.rest)
	}
	digits := p.rest[:end]
	p.rest = p.rest[end:]
	v, err := strconv.ParseFloat(digits, 64)
	return v, err == nil
}

// named is a constant, or a function applied to a parenthesized
// argument.
func (p *calcParser) named() (float64, bool) {
	p.skipSpaces()
	end := strings.IndexFunc(p.rest, func(r rune) bool { return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') })
	if end < 0 {
		end = len(p.rest)
	}
	name := p.rest[:end]
	p.rest = p.rest[end:]
	switch name {
	case "pi":
		return math.Pi, true
	case "e":
		return math.E, true
	}
	if !p.eat('(') {
		return 0, false
	}
	arg, ok := p.expression()
	if !ok || !p.eat(')') {
		return 0, false
	}
	switch name {
	case "sqrt":
		return math.Sqrt(arg), true
	case "abs":
		return math.Abs(arg), true
	case "floor":
		return math.Floor(arg), true
	case "ceil":
		return math.Ceil(arg), true
	case "round":
		return math.Round(arg), true
	case "ln":
		return math.Log(arg), true
	case "log":
		return math.Log10(arg), true
	case "sin":
		return math.Sin(arg), true
	case "cos":
		return math.Cos(arg), true
	case "tan":
		return math.Tan(arg), true
	}
	return 0, false
}
