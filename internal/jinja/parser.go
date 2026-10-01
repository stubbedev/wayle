package jinja

import (
	"slices"
	"strings"
)

type expr any

type (
	litExpr  struct{ v any }
	nameExpr struct{ name string }
	attrExpr struct {
		x    expr
		name string
	}
	itemExpr  struct{ x, key expr }
	sliceExpr struct{ x, start, stop, step expr }
	callExpr  struct {
		fn   expr
		args []arg
	}
	filterExpr struct {
		name string
		x    expr
		args []arg
	}
	testExpr struct {
		name    string
		x       expr
		args    []arg
		negated bool
	}
	unaryExpr struct {
		op string
		x  expr
	}
	binExpr struct {
		op   string
		l, r expr
	}
	ifExpr   struct{ cond, then, els expr }
	listExpr struct{ items []expr }
	mapExpr  struct{ keys, vals []expr }
)

type arg struct {
	name  string // keyword name, "" for positional
	value expr
}

type node any

type (
	textNode struct{ text string }
	emitNode struct{ x expr }
	ifNode   struct {
		conds  []expr
		bodies [][]node
		els    []node
	}
	forNode struct {
		targets []string
		iter    expr
		filter  expr
		body    []node
		els     []node
	}
	setNode struct {
		targets []string
		x       expr
	}
	setBlockNode struct {
		target string
		body   []node
	}
)

type parser struct {
	toks []token
	i    int
}

func parse(src string) ([]node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	body, end, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	if end != "" {
		return nil, errorf("unexpected tag %q", end)
	}
	return body, nil
}

func (p *parser) cur() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) isOp(op string) bool { t := p.cur(); return t.kind == tOp && t.text == op }

func (p *parser) isIdent(name string) bool {
	t := p.cur()
	return t.kind == tIdent && t.text == name
}

func (p *parser) skipOp(op string) bool {
	if p.isOp(op) {
		p.next()
		return true
	}
	return false
}

func (p *parser) skipIdent(name string) bool {
	if p.isIdent(name) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expectOp(op string) error {
	if !p.skipOp(op) {
		return errorf("expected %q", op)
	}
	return nil
}

func (p *parser) expectIdent() (string, error) {
	t := p.cur()
	if t.kind != tIdent {
		return "", errorf("expected identifier")
	}
	p.next()
	return t.text, nil
}

func (p *parser) expectKind(k tokKind, what string) error {
	if p.cur().kind != k {
		return errorf("expected %s", what)
	}
	p.next()
	return nil
}

// parseBody reads nodes until EOF or a block tag it does not own; it
// returns that tag's name (positioned after the name) for the caller.
func (p *parser) parseBody() ([]node, string, error) {
	var out []node
	for {
		t := p.cur()
		switch t.kind {
		case tEOF:
			return out, "", nil
		case tText:
			p.next()
			out = append(out, textNode{t.text})
		case tVarStart:
			p.next()
			x, err := p.parseExpr()
			if err != nil {
				return nil, "", err
			}
			if err := p.expectKind(tVarEnd, "}}"); err != nil {
				return nil, "", err
			}
			out = append(out, emitNode{x})
		case tBlockStart:
			p.next()
			name, err := p.expectIdent()
			if err != nil {
				return nil, "", err
			}
			n, err := p.parseStmt(name)
			if err != nil {
				return nil, "", err
			}
			if n == nil {
				return out, name, nil
			}
			out = append(out, n)
		default:
			return nil, "", errorf("unexpected token")
		}
	}
}

func (p *parser) endTag() error { return p.expectKind(tBlockEnd, "%}") }

// parseStmt parses the statement a tag name starts; nil (no error) is
// a tag that closes an enclosing block.
func (p *parser) parseStmt(name string) (node, error) {
	switch name {
	case "if":
		return p.parseIf()
	case "for":
		return p.parseFor()
	case "set":
		return p.parseSet()
	case "elif", "else", "endif", "endfor", "endset":
		return nil, nil
	}
	return nil, errorf("unknown statement %s", name)
}

func (p *parser) parseIf() (node, error) {
	n := ifNode{}
	for {
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.endTag(); err != nil {
			return nil, err
		}
		body, end, err := p.parseBody()
		if err != nil {
			return nil, err
		}
		n.conds, n.bodies = append(n.conds, cond), append(n.bodies, body)
		switch end {
		case "elif":
			continue
		case "else":
			if err := p.endTag(); err != nil {
				return nil, err
			}
			els, end, err := p.parseBody()
			if err != nil {
				return nil, err
			}
			if end != "endif" {
				return nil, errorf("expected endif")
			}
			n.els = els
			return n, p.endTag()
		case "endif":
			return n, p.endTag()
		}
		return nil, errorf("expected endif")
	}
}

func (p *parser) parseFor() (node, error) {
	n := forNode{}
	for {
		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		n.targets = append(n.targets, name)
		if !p.skipOp(",") {
			break
		}
	}
	if !p.skipIdent("in") {
		return nil, errorf("expected in")
	}
	iter, err := p.parseExprNoIf()
	if err != nil {
		return nil, err
	}
	n.iter = iter
	if p.skipIdent("if") {
		if n.filter, err = p.parseExprNoIf(); err != nil {
			return nil, err
		}
	}
	if err := p.endTag(); err != nil {
		return nil, err
	}
	body, end, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	n.body = body
	if end == "else" {
		if err := p.endTag(); err != nil {
			return nil, err
		}
		if n.els, end, err = p.parseBody(); err != nil {
			return nil, err
		}
	}
	if end != "endfor" {
		return nil, errorf("expected endfor")
	}
	return n, p.endTag()
}

func (p *parser) parseSet() (node, error) {
	var targets []string
	for {
		name, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		targets = append(targets, name)
		if !p.skipOp(",") {
			break
		}
	}
	if p.cur().kind == tBlockEnd && len(targets) == 1 {
		p.next()
		body, end, err := p.parseBody()
		if err != nil {
			return nil, err
		}
		if end != "endset" {
			return nil, errorf("expected endset")
		}
		return setBlockNode{target: targets[0], body: body}, p.endTag()
	}
	if err := p.expectOp("="); err != nil {
		return nil, err
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return setNode{targets: targets, x: x}, p.endTag()
}

// Expressions, loosest first (minijinja's parser): if-expressions, or,
// and, not, comparisons, + -, ~, * / // %, **, unary minus, then
// postfix access and calls, then filters and tests.

func (p *parser) parseExpr() (expr, error) {
	x, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	for p.skipIdent("if") {
		cond, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		var els expr
		if p.skipIdent("else") {
			if els, err = p.parseExpr(); err != nil {
				return nil, err
			}
		}
		x = ifExpr{cond: cond, then: x, els: els}
	}
	return x, nil
}

func (p *parser) parseExprNoIf() (expr, error) { return p.parseOr() }

func (p *parser) binary(next func() (expr, error), match func() (string, bool)) (expr, error) {
	x, err := next()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := match()
		if !ok {
			return x, nil
		}
		r, err := next()
		if err != nil {
			return nil, err
		}
		x = binExpr{op: op, l: x, r: r}
	}
}

func (p *parser) identOp(names ...string) func() (string, bool) {
	return func() (string, bool) {
		t := p.cur()
		if t.kind == tIdent && slices.Contains(names, t.text) {
			p.next()
			return t.text, true
		}
		return "", false
	}
}

func (p *parser) symOp(ops ...string) func() (string, bool) {
	return func() (string, bool) {
		t := p.cur()
		if t.kind == tOp && slices.Contains(ops, t.text) {
			p.next()
			return t.text, true
		}
		return "", false
	}
}

func (p *parser) parseOr() (expr, error)  { return p.binary(p.parseAnd, p.identOp("or")) }
func (p *parser) parseAnd() (expr, error) { return p.binary(p.parseNot, p.identOp("and")) }

func (p *parser) parseNot() (expr, error) {
	if p.skipIdent("not") {
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: "not", x: x}, nil
	}
	return p.parseCompare()
}

func (p *parser) parseCompare() (expr, error) {
	x, err := p.parseMath1()
	if err != nil {
		return nil, err
	}
	for {
		t := p.cur()
		op, negated := "", false
		switch {
		case t.kind == tOp && slices.Contains([]string{"==", "!=", "<", "<=", ">", ">="}, t.text):
			op = t.text
			p.next()
		case t.kind == tIdent && t.text == "in":
			op = "in"
			p.next()
		case t.kind == tIdent && t.text == "not" && p.toks[p.i+1].kind == tIdent && p.toks[p.i+1].text == "in":
			op, negated = "in", true
			p.next()
			p.next()
		default:
			return x, nil
		}
		r, err := p.parseMath1()
		if err != nil {
			return nil, err
		}
		x = binExpr{op: op, l: x, r: r}
		if negated {
			x = unaryExpr{op: "not", x: x}
		}
	}
}

func (p *parser) parseMath1() (expr, error)  { return p.binary(p.parseConcat, p.symOp("+", "-")) }
func (p *parser) parseConcat() (expr, error) { return p.binary(p.parseMath2, p.symOp("~")) }
func (p *parser) parseMath2() (expr, error) {
	return p.binary(p.parsePow, p.symOp("*", "/", "//", "%"))
}
func (p *parser) parsePow() (expr, error) { return p.binary(p.parseUnary, p.symOp("**")) }

func (p *parser) parseUnary() (expr, error) {
	var x expr
	var err error
	if p.skipOp("-") {
		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		x = unaryExpr{op: "-", x: inner}
	} else if x, err = p.parsePrimary(); err != nil {
		return nil, err
	}
	if x, err = p.parsePostfix(x); err != nil {
		return nil, err
	}
	return p.parseFilters(x)
}

func (p *parser) parsePostfix(x expr) (expr, error) {
	for {
		switch {
		case p.skipOp("."):
			name, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			x = attrExpr{x: x, name: name}
		case p.skipOp("["):
			var start, stop, step expr
			var err error
			slice := false
			if !p.isOp(":") {
				if start, err = p.parseExpr(); err != nil {
					return nil, err
				}
			}
			if p.skipOp(":") {
				slice = true
				if !p.isOp("]") && !p.isOp(":") {
					if stop, err = p.parseExpr(); err != nil {
						return nil, err
					}
				}
				if p.skipOp(":") && !p.isOp("]") {
					if step, err = p.parseExpr(); err != nil {
						return nil, err
					}
				}
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			if slice {
				x = sliceExpr{x: x, start: start, stop: stop, step: step}
			} else {
				if start == nil {
					return nil, errorf("empty subscript")
				}
				x = itemExpr{x: x, key: start}
			}
		case p.isOp("("):
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			x = callExpr{fn: x, args: args}
		default:
			return x, nil
		}
	}
}

func (p *parser) parseFilters(x expr) (expr, error) {
	for {
		switch {
		case p.skipOp("|"):
			name, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			var args []arg
			if p.isOp("(") {
				if args, err = p.parseArgs(); err != nil {
					return nil, err
				}
			}
			x = filterExpr{name: name, x: x, args: args}
		case p.skipIdent("is"):
			negated := p.skipIdent("not")
			name, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			var args []arg
			t := p.cur()
			switch {
			case p.isOp("("):
				if args, err = p.parseArgs(); err != nil {
					return nil, err
				}
			case t.kind == tString || t.kind == tInt || t.kind == tFloat ||
				t.kind == tOp && slices.Contains([]string{"+", "-", "[", "{"}, t.text) ||
				(t.kind == tIdent && !slices.Contains([]string{"and", "or", "else", "is"}, t.text)):
				// A test takes one bare argument: "x is divisibleby 3".
				a, err := p.parseUnaryOnlyPrimary()
				if err != nil {
					return nil, err
				}
				args = []arg{{value: a}}
			}
			x = testExpr{name: name, x: x, args: args, negated: negated}
		default:
			return x, nil
		}
	}
}

func (p *parser) parseUnaryOnlyPrimary() (expr, error) {
	var x expr
	var err error
	if p.skipOp("-") {
		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		x = unaryExpr{op: "-", x: inner}
	} else {
		x, err = p.parsePrimary()
	}
	if err != nil {
		return nil, err
	}
	return p.parsePostfix(x)
}

func (p *parser) parseArgs() ([]arg, error) {
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	var args []arg
	for !p.skipOp(")") {
		if len(args) > 0 {
			if err := p.expectOp(","); err != nil {
				return nil, err
			}
			if p.skipOp(")") {
				break
			}
		}
		t := p.cur()
		if t.kind == tIdent && p.toks[p.i+1].kind == tOp && p.toks[p.i+1].text == "=" {
			p.next()
			p.next()
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg{name: t.text, value: v})
			continue
		}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg{value: v})
	}
	return args, nil
}

func (p *parser) parsePrimary() (expr, error) {
	t := p.cur()
	switch t.kind {
	case tString:
		p.next()
		// Adjacent string literals concatenate.
		var b strings.Builder
		b.WriteString(t.text)
		for p.cur().kind == tString {
			b.WriteString(p.next().text)
		}
		return litExpr{b.String()}, nil
	case tInt:
		p.next()
		return litExpr{t.num}, nil
	case tFloat:
		p.next()
		return litExpr{t.flt}, nil
	case tIdent:
		p.next()
		switch t.text {
		case "true", "True":
			return litExpr{true}, nil
		case "false", "False":
			return litExpr{false}, nil
		case "none", "None":
			return litExpr{nil}, nil
		}
		return nameExpr{t.text}, nil
	case tOp:
		switch t.text {
		case "(":
			p.next()
			if p.skipOp(")") {
				return listExpr{}, nil
			}
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if p.isOp(",") {
				items := []expr{x}
				for p.skipOp(",") {
					if p.isOp(")") {
						break
					}
					item, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					items = append(items, item)
				}
				x = listExpr{items: items}
			}
			return x, p.expectOp(")")
		case "[":
			p.next()
			var items []expr
			for !p.skipOp("]") {
				if len(items) > 0 {
					if err := p.expectOp(","); err != nil {
						return nil, err
					}
					if p.skipOp("]") {
						break
					}
				}
				item, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return listExpr{items: items}, nil
		case "{":
			p.next()
			m := mapExpr{}
			for !p.skipOp("}") {
				if len(m.keys) > 0 {
					if err := p.expectOp(","); err != nil {
						return nil, err
					}
					if p.skipOp("}") {
						break
					}
				}
				k, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if err := p.expectOp(":"); err != nil {
					return nil, err
				}
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				m.keys, m.vals = append(m.keys, k), append(m.vals, v)
			}
			return m, nil
		}
	}
	return nil, errorf("unexpected token in expression")
}
