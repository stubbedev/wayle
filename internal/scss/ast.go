package scss

// interp is text with #{} interpolations: each part is either literal
// text or an expression whose value is spliced in unquoted.
type interp []ipart

type ipart struct {
	lit string
	e   expr
}

// ibuilder accumulates an interp, merging adjacent literal text.
type ibuilder struct {
	parts interp
	lit   []byte
}

func (b *ibuilder) byte(c byte)   { b.lit = append(b.lit, c) }
func (b *ibuilder) str(s string)  { b.lit = append(b.lit, s...) }
func (b *ibuilder) interp(e expr) { b.flush(); b.parts = append(b.parts, ipart{e: e}) }
func (b *ibuilder) done() interp  { b.flush(); return b.parts }

func (b *ibuilder) flush() {
	if len(b.lit) > 0 {
		b.parts = append(b.parts, ipart{lit: string(b.lit)})
		b.lit = b.lit[:0]
	}
}

// literal returns the interp's text when it holds no interpolation.
func (in interp) literal() (string, bool) {
	switch len(in) {
	case 0:
		return "", true
	case 1:
		return in[0].lit, in[0].e == nil
	}
	return "", false
}

// Statements.

type stmt interface{ stmtPos() pos }

func (p pos) stmtPos() pos { return p }

// styleRule is `selector { body }`.
type styleRule struct {
	pos
	sel  interp
	body []stmt
}

// decl is `name: value;`, optionally with a nested-property body. A
// custom property (and every declaration in plain CSS) keeps its value
// as raw text with only interpolation evaluated.
type decl struct {
	pos
	name  interp
	value expr
	raw   interp
	isRaw bool
	body  []stmt
}

// varDecl is `$name: value [!default] [!global];`, or `ns.$name: value;`
// assigning a module's variable.
type varDecl struct {
	pos
	ns, name string
	value    expr
	guarded  bool
	global   bool
}

// atRule is an at-rule passed through to CSS: @supports, @font-face,
// @layer, @page, @define-color, and any other the compiler does not
// interpret. bodyless ones end in `;`.
type atRule struct {
	pos
	name     string
	prelude  interp
	body     []stmt
	bodyless bool
	// supports is @supports: like @media it does not take declarations
	// outside a style rule.
	supports bool
}

// mediaRule is @media; nested ones merge their queries like Sass.
type mediaRule struct {
	pos
	query interp
	body  []stmt
}

// keyframesRule is @keyframes (and vendor-prefixed spellings): its
// children are keyframe selectors, never nested under a parent.
type keyframesRule struct {
	pos
	name    string
	prelude interp
	body    []stmt
}

// importRule is @import with one or more targets.
type importRule struct {
	pos
	targets []importTarget
	// modifiers is a trailing media or supports query, which makes
	// every target a plain CSS import.
	modifiers interp
}

type importTarget struct {
	pos
	url string
	// raw is the target as written, for a plain CSS import.
	raw   string
	plain bool
}

// useRule is `@use "url" [as ns|*] [with (...)]`.
type useRule struct {
	pos
	url    string
	ns     string
	config []configVar
}

type configVar struct {
	pos
	name  string
	value expr
}

// mixinRule is `@mixin name(params) { body }`.
type mixinRule struct {
	pos
	name   string
	params []param
	body   []stmt
}

type param struct {
	name string
	def  expr
}

// includeRule is `@include [ns.]name(args) [{ content }]`.
type includeRule struct {
	pos
	ns, name   string
	args       []arg
	content    []stmt
	hasContent bool
}

// contentRule is @content inside a mixin body.
type contentRule struct{ pos }

// Expressions.

type expr interface{ exprPos() pos }

func (p pos) exprPos() pos { return p }

type numLit struct {
	pos
	v    float64
	unit string
}

// strLit is a quoted string or an unquoted identifier, either with
// interpolation. A quoted string's literal text is stored escaped for
// double quotes.
type strLit struct {
	pos
	quoted bool
	parts  interp
}

type colorLit struct {
	pos
	c color
}

type boolLit struct {
	pos
	v bool
}

type nullLit struct{ pos }

type varRef struct {
	pos
	ns, name string
}

type unaryExpr struct {
	pos
	op string
	x  expr
}

// binaryExpr is an operation; slash marks a `/` between two literal
// numbers, which Sass keeps as a separator unless the result is used
// arithmetically.
type binaryExpr struct {
	pos
	op    string
	l, r  expr
	slash bool
}

type parenExpr struct {
	pos
	x expr
}

type listExpr struct {
	pos
	items     []expr
	sep       byte
	bracketed bool
}

type callExpr struct {
	pos
	ns, name string
	args     []arg
}

// arg is a call argument; name is set for a keyword argument.
type arg struct {
	name string
	x    expr
}

// rawExpr is text with interpolation that evaluates to an unquoted
// string: a special url(), a unicode-range, !important.
type rawExpr struct {
	pos
	parts interp
}
