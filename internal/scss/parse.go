package scss

import "strings"

// parser is a recursive-descent parser over one source. Failures panic
// with an *Error (pos.fail), recovered at the package boundary.
type parser struct {
	src *source
	s   string
	i   int
	// mixinDepth counts enclosing @mixin bodies: @content is only
	// valid inside one.
	mixinDepth int
	// ruleDepth counts enclosing style rules: plain CSS may not nest.
	ruleDepth int
}

// parse parses a whole stylesheet.
func parse(src *source) []stmt {
	p := &parser{src: src, s: src.text}
	return p.stylesheet()
}

func (p *parser) here() pos { return pos{p.src, p.i} }

func (p *parser) at(n int) byte {
	if p.i+n < len(p.s) && p.i+n >= 0 {
		return p.s[p.i+n]
	}
	return 0
}

func (p *parser) peek() byte { return p.at(0) }

func (p *parser) eof() bool { return p.i >= len(p.s) }

func (p *parser) expect(c byte) {
	if p.peek() != c {
		p.here().fail("expected %q", string(c))
	}
	p.i++
}

// plainOnly rejects Sass syntax in a plain CSS source.
func (p *parser) plainOnly(what string) {
	if p.src.plain {
		p.here().fail("%s aren't allowed in plain CSS", what)
	}
}

// ws skips whitespace and comments, reporting whether any was skipped.
func (p *parser) ws() bool {
	start := p.i
	for !p.eof() {
		c := p.s[p.i]
		switch {
		case isSpace(c):
			p.i++
		case c == '/' && p.at(1) == '*':
			p.skipBlockComment()
		case c == '/' && p.at(1) == '/':
			p.plainOnly("silent comments")
			for !p.eof() && p.s[p.i] != '\n' {
				p.i++
			}
		default:
			return p.i > start
		}
	}
	return p.i > start
}

func (p *parser) skipBlockComment() {
	end := strings.Index(p.s[p.i+2:], "*/")
	if end < 0 {
		p.here().fail("unterminated comment")
	}
	p.i += end + 4
}

// keyword consumes word when it appears next as a whole identifier.
func (p *parser) keyword(word string) bool {
	if !strings.HasPrefix(p.s[p.i:], word) || isName(p.at(len(word))) {
		return false
	}
	p.i += len(word)
	return true
}

// ident reads a plain identifier (no interpolation); empty if none.
func (p *parser) ident() string {
	start := p.i
	for p.peek() == '-' {
		p.i++
	}
	if !isNameStart(p.peek()) {
		p.i = start
		return ""
	}
	for !p.eof() && isName(p.s[p.i]) {
		if p.s[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	return p.s[start:p.i]
}

// endStatement consumes a statement's `;`; a closing `}` or the end of
// input also ends it.
func (p *parser) endStatement() {
	p.ws()
	switch p.peek() {
	case ';':
		p.i++
	case '}', 0:
	default:
		p.here().fail(`expected ";"`)
	}
}

func (p *parser) stylesheet() []stmt {
	var out []stmt
	usesAllowed := true
	for {
		p.ws()
		if p.eof() {
			return out
		}
		switch p.peek() {
		case ';':
			p.i++
			continue
		case '}':
			p.here().fail(`unmatched "}"`)
		}
		st := p.statement(true, usesAllowed)
		if st == nil {
			continue
		}
		switch st.(type) {
		case *useRule, *varDecl:
		default:
			usesAllowed = false
		}
		out = append(out, st)
	}
}

// block parses `{ statements }`.
func (p *parser) block() []stmt {
	p.expect('{')
	var out []stmt
	for {
		p.ws()
		switch p.peek() {
		case 0:
			p.here().fail(`expected "}"`)
		case '}':
			p.i++
			return out
		case ';':
			p.i++
			continue
		}
		if st := p.statement(false, false); st != nil {
			out = append(out, st)
		}
	}
}

// statement parses one statement; nil is one that emits nothing
// (@charset).
func (p *parser) statement(top, usesAllowed bool) stmt {
	switch p.peek() {
	case '@':
		return p.atRule(usesAllowed)
	case '$':
		return p.varDecl("")
	}
	if ns, ok := p.namespacedVar(); ok {
		return p.varDecl(ns)
	}
	term, propColon := p.lookahead()
	if term == '{' && (top || !propColon) && !strings.HasPrefix(p.s[p.i:], "--") {
		return p.styleRule()
	}
	if top {
		p.here().fail("declarations may only be used within style rules")
	}
	return p.declaration(strings.HasPrefix(p.s[p.i:], "--"))
}

// namespacedVar recognizes `ns.$name:`, consuming `ns.` when found.
func (p *parser) namespacedVar() (string, bool) {
	start := p.i
	ns := p.ident()
	if ns != "" && p.peek() == '.' && p.at(1) == '$' {
		p.i++
		return ns, true
	}
	p.i = start
	return "", false
}

// lookahead scans (without consuming) to the first `{`, `;`, or `}`
// outside brackets, strings, and interpolation, telling a style rule
// from a declaration. propColon reports a first top-level `:` followed
// by whitespace or `{`: `font: {` is a nested property, `a:hover {` a
// selector.
func (p *parser) lookahead() (term byte, propColon bool) {
	depth := 0
	colon := false
	for j := p.i; j < len(p.s); {
		c := p.s[j]
		next := byte(0)
		if j+1 < len(p.s) {
			next = p.s[j+1]
		}
		switch {
		case c == '"' || c == '\'':
			j = skipString(p.s, j)
		case c == '\\':
			j += 2
		case c == '#' && next == '{':
			j = skipInterp(p.s, j)
		case c == '/' && next == '*':
			end := strings.Index(p.s[j+2:], "*/")
			if end < 0 {
				return 0, propColon
			}
			j += end + 4
		case c == '/' && next == '/' && depth == 0 && !p.src.plain:
			for j < len(p.s) && p.s[j] != '\n' {
				j++
			}
		case c == '(' || c == '[':
			depth++
			j++
		case c == ')' || c == ']':
			depth--
			j++
		case depth <= 0 && (c == '{' || c == ';' || c == '}'):
			return c, propColon
		case depth <= 0 && c == ':' && !colon:
			colon = true
			propColon = isSpace(next) || next == '{'
			j++
		default:
			j++
		}
	}
	return 0, propColon
}

// skipString returns the offset just past the quoted string at s[i].
func skipString(s string, i int) int {
	q := s[i]
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case q, '\n':
			return i + 1
		}
	}
	return i
}

// skipInterp returns the offset just past the #{...} at s[i].
func skipInterp(s string, i int) int {
	depth := 0
	for i += 2; i < len(s); {
		switch s[i] {
		case '"', '\'':
			i = skipString(s, i)
			continue
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i + 1
			}
			depth--
		}
		i++
	}
	return i
}

func (p *parser) styleRule() stmt {
	at := p.here()
	sel, _ := p.interpUntil("{")
	p.ruleDepth++
	if p.src.plain && p.ruleDepth > 1 {
		at.fail("nested style rules aren't allowed in plain CSS")
	}
	body := p.block()
	p.ruleDepth--
	return &styleRule{pos: at, sel: sel, body: body}
}

// interpolation parses `#{expr}` at p.i.
func (p *parser) interpolation() expr {
	p.plainOnly("interpolation")
	p.i += 2
	p.ws()
	e := p.expression()
	p.ws()
	p.expect('}')
	return e
}

// interpUntil reads text with interpolation up to the first byte of
// stops outside brackets and strings, returning it and the stop byte
// (0 at the end of input). Comments become a space; the evaluator
// collapses whitespace.
func (p *parser) interpUntil(stops string) (interp, byte) {
	var b ibuilder
	depth := 0
	for !p.eof() {
		c := p.s[p.i]
		switch {
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			return b.done(), c
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		case c == '"' || c == '\'':
			p.copyString(&b)
		case c == '\\':
			b.byte(c)
			p.i++
			if !p.eof() {
				b.byte(p.s[p.i])
				p.i++
			}
		case c == '/' && (p.at(1) == '*' || (p.at(1) == '/' && depth == 0)):
			p.ws()
			b.byte(' ')
		default:
			switch c {
			case '(', '[':
				depth++
			case ')', ']':
				depth--
			}
			b.byte(c)
			p.i++
		}
	}
	return b.done(), 0
}

// copyString copies a quoted string verbatim into b, evaluating any
// interpolation inside it.
func (p *parser) copyString(b *ibuilder) {
	q := p.s[p.i]
	b.byte(q)
	p.i++
	for {
		if p.eof() || p.s[p.i] == '\n' {
			p.here().fail("unterminated string")
		}
		c := p.s[p.i]
		switch {
		case c == q:
			b.byte(c)
			p.i++
			return
		case c == '\\' && p.i+1 < len(p.s):
			b.str(p.s[p.i : p.i+2])
			p.i += 2
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		default:
			b.byte(c)
			p.i++
		}
	}
}

// declaration parses `name: value;`, a custom property, or a nested
// property block.
func (p *parser) declaration(custom bool) stmt {
	at := p.here()
	name, stop := p.interpUntil(":;{}")
	if stop != ':' {
		p.here().fail(`expected ":"`)
	}
	p.i++
	d := &decl{pos: at, name: name}
	if custom || p.src.plain {
		d.isRaw = true
		d.raw = p.rawValue()
		p.endStatement()
		return d
	}
	p.ws()
	if p.peek() != '{' {
		d.value = p.expression()
		p.ws()
	}
	if p.peek() == '{' {
		d.body = p.block()
		return d
	}
	p.endStatement()
	return d
}

// rawValue reads a custom property's (or plain CSS) value verbatim up
// to the `;` or `}` that ends it, balancing brackets and braces.
func (p *parser) rawValue() interp {
	var b ibuilder
	depth := 0
	for !p.eof() {
		c := p.s[p.i]
		switch {
		case depth == 0 && (c == ';' || c == '}'):
			return b.done()
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		case c == '"' || c == '\'':
			p.copyString(&b)
		case c == '\\' && p.i+1 < len(p.s):
			b.str(p.s[p.i : p.i+2])
			p.i += 2
		case c == '$' && p.src.plain:
			p.plainOnly("Sass variables")
		default:
			switch c {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				depth--
			}
			b.byte(c)
			p.i++
		}
	}
	return b.done()
}

func (p *parser) varDecl(ns string) stmt {
	p.plainOnly("Sass variables")
	at := p.here()
	p.i++ // $
	name := p.ident()
	if name == "" {
		p.here().fail("expected variable name")
	}
	p.ws()
	p.expect(':')
	p.ws()
	v := &varDecl{pos: at, ns: ns, name: normName(name)}
	v.value = p.expression()
	for {
		p.ws()
		if p.peek() != '!' {
			break
		}
		p.i++
		switch flag := p.ident(); flag {
		case "default":
			v.guarded = true
		case "global":
			v.global = true
		default:
			p.here().fail("invalid flag name %q", flag)
		}
	}
	p.endStatement()
	return v
}

// unsupportedAtRules are Sass at-rules outside the subset.
var unsupportedAtRules = map[string]bool{
	"if": true, "else": true, "each": true, "for": true, "while": true,
	"function": true, "return": true, "extend": true, "at-root": true,
	"debug": true, "warn": true, "error": true, "forward": true,
}

// sassAtRules are the at-rules plain CSS may not use.
var sassAtRules = map[string]bool{"use": true, "mixin": true, "include": true, "content": true}

func (p *parser) atRule(usesAllowed bool) stmt {
	at := p.here()
	p.i++ // @
	name := p.ident()
	if name == "" {
		p.here().fail("expected at-rule name")
	}
	if p.src.plain && (sassAtRules[name] || unsupportedAtRules[name]) {
		at.fail("@%s isn't allowed in plain CSS", name)
	}
	if unsupportedAtRules[name] {
		at.unsupported("@" + name)
	}
	p.ws()
	switch name {
	case "import":
		return p.importRule(at)
	case "use":
		if !usesAllowed {
			at.fail("@use rules must be written before any other rules")
		}
		return p.useRule(at)
	case "mixin":
		return p.mixinRule(at)
	case "include":
		return p.includeRule(at)
	case "content":
		if p.mixinDepth == 0 {
			at.fail("@content is only allowed within mixin declarations")
		}
		if p.peek() == '(' || p.keyword("using") {
			at.unsupported("@content with arguments")
		}
		p.endStatement()
		return &contentRule{pos: at}
	case "charset":
		p.interpUntil(";{}")
		p.endStatement()
		return nil
	case "media":
		q := p.condition()
		return &mediaRule{pos: at, query: q, body: p.block()}
	case "supports":
		q := p.condition()
		return &atRule{pos: at, name: name, prelude: q, body: p.block(), supports: true}
	}
	prelude, stop := p.interpUntil("{;}")
	if name == "keyframes" || (strings.HasPrefix(name, "-") && strings.HasSuffix(name, "-keyframes")) {
		if stop != '{' {
			p.here().fail(`expected "{"`)
		}
		return &keyframesRule{pos: at, name: name, prelude: prelude, body: p.block()}
	}
	if stop == '{' {
		return &atRule{pos: at, name: name, prelude: prelude, body: p.block()}
	}
	p.endStatement()
	return &atRule{pos: at, name: name, prelude: prelude, bodyless: true}
}

// condition reads a @media or @supports prelude. A parenthesized
// `(feature: value)` has its value parsed as SassScript, as in Sass;
// everything else is text with interpolation.
func (p *parser) condition() interp {
	var b ibuilder
	for {
		c := p.peek()
		switch {
		case c == 0 || c == ';' || c == '}':
			p.here().fail(`expected "{"`)
		case c == '{':
			return b.done()
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		case c == '"' || c == '\'':
			p.copyString(&b)
		case c == '/' && (p.at(1) == '*' || p.at(1) == '/'):
			p.ws()
			b.byte(' ')
		case c == '$':
			p.plainOnly("Sass variables")
			p.here().unsupported("a Sass variable in a query outside (feature: value); interpolate it with #{}")
		case c == '(':
			p.i++
			b.byte('(')
			if !p.src.plain {
				p.feature(&b)
			}
		default:
			b.byte(c)
			p.i++
		}
	}
}

// feature parses `name: value)` right after a query's `(`, when that is
// what follows; otherwise it leaves the input alone.
func (p *parser) feature(b *ibuilder) {
	start := p.i
	p.ws()
	var name ibuilder
	for {
		switch c := p.peek(); {
		case c == '#' && p.at(1) == '{':
			name.interp(p.interpolation())
			continue
		case isName(c):
			name.byte(c)
			p.i++
			continue
		}
		break
	}
	nameParts := name.done()
	p.ws()
	if len(nameParts) == 0 || p.peek() != ':' {
		p.i = start
		return
	}
	p.i++
	p.ws()
	value := p.expression()
	p.ws()
	p.expect(')')
	for _, part := range nameParts {
		if part.e != nil {
			b.interp(part.e)
		} else {
			b.str(part.lit)
		}
	}
	b.str(": ")
	b.interp(value)
	b.byte(')')
}

// quotedURL reads an @import/@use URL: a quoted string without
// interpolation.
func (p *parser) quotedURL() (url, raw string) {
	at := p.here()
	q := p.peek()
	if q != '"' && q != '\'' {
		at.fail("expected string")
	}
	start := p.i
	p.i = skipString(p.s, p.i)
	raw = p.s[start:p.i]
	if strings.Contains(raw, "#{") {
		at.unsupported("interpolation in an import URL")
	}
	if len(raw) < 2 || raw[len(raw)-1] != q {
		at.fail("unterminated string")
	}
	return raw[1 : len(raw)-1], raw
}

func (p *parser) importRule(at pos) stmt {
	rule := &importRule{pos: at}
	for {
		tat := p.here()
		var t importTarget
		if strings.HasPrefix(p.s[p.i:], "url(") {
			start := p.i
			depth := 0
			for !p.eof() {
				c := p.s[p.i]
				p.i++
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			t = importTarget{pos: tat, raw: p.s[start:p.i], plain: true}
		} else {
			url, raw := p.quotedURL()
			t = importTarget{pos: tat, url: url, raw: raw, plain: p.src.plain || isPlainImport(url)}
		}
		rule.targets = append(rule.targets, t)
		p.ws()
		if p.peek() != ',' {
			break
		}
		p.i++
		p.ws()
	}
	mods, _ := p.interpUntil(";}")
	if s, ok := mods.literal(); !ok || strings.TrimSpace(s) != "" {
		rule.modifiers = mods
		for i := range rule.targets {
			rule.targets[i].plain = true
		}
	}
	p.endStatement()
	return rule
}

// isPlainImport reports an @import Sass leaves to CSS: a .css file or
// a remote URL.
func isPlainImport(url string) bool {
	return strings.HasSuffix(url, ".css") || strings.HasPrefix(url, "http://") ||
		strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "//")
}

func (p *parser) useRule(at pos) stmt {
	url, _ := p.quotedURL()
	rule := &useRule{pos: at, url: url}
	p.ws()
	if p.keyword("as") {
		p.ws()
		if p.peek() == '*' {
			p.i++
			rule.ns = "*"
		} else if rule.ns = p.ident(); rule.ns == "" {
			p.here().fail("expected namespace")
		}
		p.ws()
	}
	if p.keyword("with") {
		p.ws()
		p.expect('(')
		for {
			p.ws()
			if p.peek() == ')' {
				break
			}
			vat := p.here()
			p.expect('$')
			name := p.ident()
			p.ws()
			p.expect(':')
			p.ws()
			rule.config = append(rule.config, configVar{pos: vat, name: normName(name), value: p.spaceList()})
			p.ws()
			if p.peek() != ',' {
				break
			}
			p.i++
		}
		p.expect(')')
	}
	p.endStatement()
	return rule
}

func (p *parser) mixinRule(at pos) stmt {
	name := p.ident()
	if name == "" {
		p.here().fail("expected mixin name")
	}
	rule := &mixinRule{pos: at, name: normName(name)}
	p.ws()
	if p.peek() == '(' {
		p.i++
		for {
			p.ws()
			if p.peek() == ')' {
				break
			}
			p.expect('$')
			pr := param{name: normName(p.ident())}
			p.ws()
			if strings.HasPrefix(p.s[p.i:], "...") {
				p.here().unsupported("rest arguments ($" + pr.name + "...)")
			}
			if p.peek() == ':' {
				p.i++
				p.ws()
				pr.def = p.spaceList()
				p.ws()
			}
			rule.params = append(rule.params, pr)
			if p.peek() != ',' {
				break
			}
			p.i++
		}
		p.expect(')')
		p.ws()
	}
	p.mixinDepth++
	rule.body = p.block()
	p.mixinDepth--
	return rule
}

func (p *parser) includeRule(at pos) stmt {
	name := p.ident()
	if name == "" {
		p.here().fail("expected mixin name")
	}
	rule := &includeRule{pos: at, name: normName(name)}
	if p.peek() == '.' {
		p.i++
		rule.ns = name
		rule.name = normName(p.ident())
	}
	p.ws()
	if p.peek() == '(' {
		rule.args = p.args()
		p.ws()
	}
	if p.keyword("using") {
		at.unsupported("@include ... using")
	}
	if p.peek() == '{' {
		rule.content = p.block()
		rule.hasContent = true
		return rule
	}
	p.endStatement()
	return rule
}

// args parses a call's `(arg, $name: arg, ...)`.
func (p *parser) args() []arg {
	p.expect('(')
	var out []arg
	for {
		p.ws()
		if p.peek() == ')' {
			break
		}
		var a arg
		if p.peek() == '$' {
			start := p.i
			p.i++
			name := p.ident()
			p.ws()
			if p.peek() == ':' && p.at(1) != ':' {
				p.i++
				p.ws()
				a.name = normName(name)
			} else {
				p.i = start
			}
		}
		a.x = p.spaceList()
		p.ws()
		if strings.HasPrefix(p.s[p.i:], "...") {
			p.here().unsupported("argument lists (...)")
		}
		out = append(out, a)
		if p.peek() != ',' {
			break
		}
		p.i++
	}
	p.expect(')')
	return out
}
