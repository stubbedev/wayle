package scss

import (
	"strconv"
	"strings"
)

// atExprEnd reports whether the next byte ends the current expression.
func (p *parser) atExprEnd() bool {
	switch c := p.peek(); c {
	case 0, ',', ';', ')', ']', '}', '{', ':', '=':
		return true
	case '!':
		rest := strings.TrimLeft(p.s[p.i+1:], " \t\n\r\f")
		return strings.HasPrefix(rest, "default") || strings.HasPrefix(rest, "global")
	case '.':
		return !isDigit(p.at(1))
	}
	return false
}

// expression parses a comma-separated list (or a single value).
func (p *parser) expression() expr {
	at := p.here()
	first := p.spaceList()
	p.ws()
	if p.peek() != ',' {
		return first
	}
	items := []expr{first}
	for p.peek() == ',' {
		p.i++
		p.ws()
		if p.atExprEnd() {
			break
		}
		items = append(items, p.spaceList())
		p.ws()
	}
	return &listExpr{pos: at, items: items, sep: ','}
}

// spaceList parses a whitespace-separated list (or a single value).
func (p *parser) spaceList() expr {
	at := p.here()
	first := p.orExpr()
	items := []expr{first}
	for {
		p.ws()
		if p.atExprEnd() {
			break
		}
		items = append(items, p.orExpr())
	}
	if len(items) == 1 {
		return first
	}
	return &listExpr{pos: at, items: items, sep: ' '}
}

// binaryLevel parses one precedence level: next parses the operands,
// op reports (and consumes) an operator at the cursor.
func (p *parser) binaryLevel(next func() expr, op func(ws bool) string) expr {
	left := next()
	for {
		save := p.i
		ws := p.ws()
		at := p.here()
		o := op(ws)
		if o == "" {
			p.i = save
			return left
		}
		p.ws()
		right := next()
		left = &binaryExpr{pos: at, op: o, l: left, r: right}
	}
}

func (p *parser) orExpr() expr {
	return p.binaryLevel(p.andExpr, func(bool) string {
		if p.keyword("or") {
			return "or"
		}
		return ""
	})
}

func (p *parser) andExpr() expr {
	return p.binaryLevel(p.eqExpr, func(bool) string {
		if p.keyword("and") {
			return "and"
		}
		return ""
	})
}

func (p *parser) eqExpr() expr {
	return p.binaryLevel(p.relExpr, func(bool) string {
		for _, o := range []string{"==", "!="} {
			if strings.HasPrefix(p.s[p.i:], o) {
				p.i += 2
				return o
			}
		}
		return ""
	})
}

func (p *parser) relExpr() expr {
	return p.binaryLevel(p.addExpr, func(bool) string {
		for _, o := range []string{"<=", ">=", "<", ">"} {
			if strings.HasPrefix(p.s[p.i:], o) {
				p.i += len(o)
				return o
			}
		}
		return ""
	})
}

func (p *parser) addExpr() expr {
	return p.binaryLevel(p.mulExpr, func(ws bool) string {
		switch p.peek() {
		case '+':
			p.i++
			return "+"
		case '-':
			if p.minusIsBinary(ws) {
				p.i++
				return "-"
			}
		}
		return ""
	})
}

// minusIsBinary applies Sass's whitespace rule to a `-` after an
// operand: `a - b` and `a-b` subtract, while `a -b` and `1 -2` start a
// new list element (but `$a -$b` still subtracts).
func (p *parser) minusIsBinary(wsBefore bool) bool {
	if !wsBefore {
		return true
	}
	next := p.at(1)
	switch {
	case isSpace(next):
		return true
	case isDigit(next) || next == '.' || isNameStart(next) || next == '-' || next == '#':
		return false
	}
	return true
}

func (p *parser) mulExpr() expr {
	left := p.unary()
	for {
		save := p.i
		p.ws()
		at := p.here()
		var op string
		switch c := p.peek(); c {
		case '*', '%':
			op = string(c)
		case '/':
			if next := p.at(1); next != '/' && next != '*' {
				op = "/"
			}
		}
		if op == "" {
			p.i = save
			return left
		}
		p.i++
		p.ws()
		right := p.unary()
		left = &binaryExpr{pos: at, op: op, l: left, r: right, slash: op == "/" && slashable(left) && slashable(right)}
	}
}

// slashable reports an operand that keeps `/` a separator: a number
// literal or another such slash.
func slashable(e expr) bool {
	switch x := e.(type) {
	case *numLit:
		return true
	case *binaryExpr:
		return x.slash
	}
	return false
}

func (p *parser) unary() expr {
	at := p.here()
	switch c, next := p.peek(), p.at(1); {
	case c == '-' || c == '+':
		if isDigit(next) || (next == '.' && isDigit(p.at(2))) {
			return p.number()
		}
		if c == '-' && (isNameStart(next) || next == '-' || (next == '#' && p.at(2) == '{')) {
			return p.primary()
		}
		p.i++
		p.ws()
		return &unaryExpr{pos: at, op: string(c), x: p.unary()}
	case p.keyword("not"):
		p.ws()
		return &unaryExpr{pos: at, op: "not", x: p.unary()}
	}
	return p.primary()
}

func (p *parser) primary() expr {
	at := p.here()
	c := p.peek()
	switch {
	case c == '(':
		p.i++
		p.ws()
		if p.peek() == ')' {
			p.i++
			return &listExpr{pos: at, sep: ' '}
		}
		x := p.expression()
		p.ws()
		if p.peek() == ':' {
			at.unsupported("maps")
		}
		p.expect(')')
		return &parenExpr{pos: at, x: x}
	case c == '[':
		p.i++
		p.ws()
		l := &listExpr{pos: at, sep: ' ', bracketed: true}
		if p.peek() != ']' {
			x := p.expression()
			if inner, ok := x.(*listExpr); ok && !inner.bracketed {
				l.items, l.sep = inner.items, inner.sep
			} else {
				l.items = []expr{x}
			}
			p.ws()
		}
		p.expect(']')
		return l
	case c == '$':
		p.plainOnly("Sass variables")
		p.i++
		name := p.ident()
		if name == "" {
			at.fail("expected variable name")
		}
		return &varRef{pos: at, name: normName(name)}
	case c == '"' || c == '\'':
		return p.quoted()
	case isDigit(c) || (c == '.' && isDigit(p.at(1))):
		return p.number()
	case c == '#' && p.at(1) != '{':
		return p.hexColor()
	case c == '!':
		p.i++
		p.ws()
		if !p.keyword("important") {
			at.fail("expected \"important\"")
		}
		return &rawExpr{pos: at, parts: interp{{lit: "!important"}}}
	case c == '&':
		at.unsupported("the parent selector & in an expression")
	case (c == 'u' || c == 'U') && p.at(1) == '+' && (isHex(p.at(2)) || p.at(2) == '?'):
		return p.unicodeRange()
	case isNameStart(c) || c == '-' || c == '#':
		return p.identExpr()
	}
	at.fail("expected expression")
	return nil
}

func (p *parser) number() expr {
	at := p.here()
	start := p.i
	if c := p.peek(); c == '-' || c == '+' {
		p.i++
	}
	for isDigit(p.peek()) {
		p.i++
	}
	if p.peek() == '.' && isDigit(p.at(1)) {
		p.i++
		for isDigit(p.peek()) {
			p.i++
		}
	}
	if e := p.peek(); e == 'e' || e == 'E' {
		n := 1
		if s := p.at(1); s == '-' || s == '+' {
			n = 2
		}
		if isDigit(p.at(n)) {
			p.i += n
			for isDigit(p.peek()) {
				p.i++
			}
		}
	}
	v, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		at.fail("invalid number %q", p.s[start:p.i])
	}
	n := &numLit{pos: at, v: v}
	switch c := p.peek(); {
	case c == '%':
		p.i++
		n.unit = "%"
	case isNameStart(c):
		ustart := p.i
		for !p.eof() {
			c := p.s[p.i]
			if c == '-' && (isDigit(p.at(1)) || p.at(1) == '.') {
				break
			}
			if !isName(c) {
				break
			}
			p.i++
		}
		n.unit = p.s[ustart:p.i]
	}
	return n
}

func (p *parser) hexColor() expr {
	at := p.here()
	p.i++
	start := p.i
	for isName(p.peek()) {
		p.i++
	}
	text := p.s[start:p.i]
	c, ok := parseHex(text)
	if !ok {
		at.fail("invalid color #%s", text)
	}
	c.text = "#" + text
	return &colorLit{pos: at, c: c}
}

func (p *parser) unicodeRange() expr {
	at := p.here()
	start := p.i
	p.i += 2
	for isHex(p.peek()) || p.peek() == '?' {
		p.i++
	}
	if p.peek() == '-' && isHex(p.at(1)) {
		p.i++
		for isHex(p.peek()) {
			p.i++
		}
	}
	return &rawExpr{pos: at, parts: interp{{lit: p.s[start:p.i]}}}
}

// quoted parses a quoted string, normalizing its text to double-quote
// escaping (a `"` inside becomes `\"`, `\'` becomes `'`).
func (p *parser) quoted() expr {
	at := p.here()
	q := p.peek()
	p.i++
	var b ibuilder
	for {
		if p.eof() || p.peek() == '\n' {
			at.fail("unterminated string")
		}
		c := p.peek()
		switch {
		case c == q:
			p.i++
			return &strLit{pos: at, quoted: true, parts: b.done()}
		case c == '\\':
			next := p.at(1)
			switch next {
			case '\n':
				p.i += 2
			case '\'':
				b.byte('\'')
				p.i += 2
			case 0:
				p.i++
			default:
				b.byte('\\')
				b.byte(next)
				p.i += 2
			}
		case c == '"':
			b.str(`\"`)
			p.i++
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		default:
			b.byte(c)
			p.i++
		}
	}
}

// identParts reads an identifier that may contain interpolation.
func (p *parser) identParts() interp {
	var b ibuilder
	for !p.eof() {
		c := p.s[p.i]
		switch {
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		case c == '\\' && p.i+1 < len(p.s):
			b.str(p.s[p.i : p.i+2])
			p.i += 2
		case isName(c):
			b.byte(c)
			p.i++
		default:
			return b.done()
		}
	}
	return b.done()
}

// identExpr parses an identifier-led expression: a function call, a
// namespaced member, a keyword literal, a named color, or an unquoted
// string.
func (p *parser) identExpr() expr {
	at := p.here()
	parts := p.identParts()
	name, lit := parts.literal()
	if len(parts) == 0 {
		at.fail("expected expression")
	}
	if !lit {
		if p.peek() == '(' {
			at.unsupported("interpolated function names")
		}
		return &strLit{pos: at, parts: parts}
	}
	switch p.peek() {
	case '(':
		if name == "url" {
			if raw := p.specialURL(at); raw != nil {
				return raw
			}
		}
		return &callExpr{pos: at, name: name, args: p.args()}
	case '.':
		if p.at(1) == '$' {
			p.i += 2
			member := p.ident()
			if member == "" {
				p.here().fail("expected variable name")
			}
			return &varRef{pos: at, ns: name, name: normName(member)}
		}
		if isNameStart(p.at(1)) {
			save := p.i
			p.i++
			member := p.ident()
			if p.peek() == '(' {
				return &callExpr{pos: at, ns: name, name: member, args: p.args()}
			}
			p.i = save
		}
	}
	switch name {
	case "true", "false":
		return &boolLit{pos: at, v: name == "true"}
	case "null":
		return &nullLit{pos: at}
	}
	if c, ok := namedColor(name); ok {
		return &colorLit{pos: at, c: c}
	}
	return &strLit{pos: at, parts: parts}
}

// specialURL parses an unquoted url(...) verbatim (with
// interpolation), as Sass does; nil means the argument is SassScript
// (a quoted string or a variable) and url() is an ordinary call.
func (p *parser) specialURL(at pos) expr {
	save := p.i
	p.i++ // (
	var b ibuilder
	b.str("url(")
	for isSpace(p.peek()) {
		p.i++
	}
	for {
		c := p.peek()
		switch {
		case c == ')':
			p.i++
			b.byte(')')
			return &rawExpr{pos: at, parts: b.done()}
		case c == 0 || c == '"' || c == '\'' || c == '(' || c == '$':
			p.i = save
			return nil
		case isSpace(c):
			for isSpace(p.peek()) {
				p.i++
			}
			if p.peek() != ')' {
				p.i = save
				return nil
			}
		case c == '#' && p.at(1) == '{':
			b.interp(p.interpolation())
		case c == '\\' && p.i+1 < len(p.s):
			b.str(p.s[p.i : p.i+2])
			p.i += 2
		default:
			b.byte(c)
			p.i++
		}
	}
}
