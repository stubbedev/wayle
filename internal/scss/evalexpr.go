package scss

import "strings"

// interpText evaluates text with interpolation, splicing values in
// unquoted.
func (c *compiler) interpText(in interp, e env) string {
	if s, ok := in.literal(); ok {
		return s
	}
	var b strings.Builder
	for _, part := range in {
		if part.e == nil {
			b.WriteString(part.lit)
		} else {
			b.WriteString(unquoted(c.eval(part.e, e)))
		}
	}
	return b.String()
}

// stripSlash turns a slash-separated number into its quotient, as a
// variable reference or parentheses do in Sass.
func stripSlash(v value) value {
	if n, ok := v.(number); ok && n.slash != nil {
		n.slash = nil
		return n
	}
	return v
}

func (c *compiler) eval(x expr, e env) value {
	switch x := x.(type) {
	case *numLit:
		return number{v: x.v, unit: x.unit}
	case *strLit:
		if !x.quoted {
			return sstring{text: c.interpText(x.parts, e)}
		}
		var b strings.Builder
		for _, part := range x.parts {
			if part.e == nil {
				b.WriteString(part.lit)
			} else {
				b.WriteString(escapeQuoted(unquoted(c.eval(part.e, e))))
			}
		}
		return sstring{text: b.String(), quoted: true}
	case *colorLit:
		return x.c
	case *boolLit:
		return boolean(x.v)
	case *nullLit:
		return nullValue{}
	case *varRef:
		return stripSlash(e.lookupVar(x))
	case *parenExpr:
		return stripSlash(c.eval(x.x, e))
	case *listExpr:
		l := list{sep: x.sep, bracketed: x.bracketed}
		for _, item := range x.items {
			l.items = append(l.items, c.eval(item, e))
		}
		return l
	case *unaryExpr:
		v := c.eval(x.x, e)
		switch x.op {
		case "not":
			return boolean(!truthy(v))
		case "-":
			if n, ok := v.(number); ok {
				return number{v: -n.v, unit: n.unit}
			}
		case "+":
			if n, ok := v.(number); ok {
				return number{v: n.v, unit: n.unit}
			}
		}
		return sstring{text: x.op + css(v)}
	case *binaryExpr:
		return c.binary(x, e)
	case *callExpr:
		return c.call(x, e)
	case *rawExpr:
		return sstring{text: c.interpText(x.parts, e)}
	}
	x.exprPos().fail("unexpected expression")
	return nil
}

func (c *compiler) binary(x *binaryExpr, e env) value {
	switch x.op {
	case "and", "or":
		l := c.eval(x.l, e)
		if truthy(l) == (x.op == "or") {
			return l
		}
		return c.eval(x.r, e)
	}
	l, r := c.eval(x.l, e), c.eval(x.r, e)
	ln, lnum := l.(number)
	rn, rnum := r.(number)
	undefined := func() value {
		x.fail("undefined operation \"%s %s %s\"", css(l), x.op, css(r))
		return nil
	}
	switch x.op {
	case "==":
		return boolean(equal(l, r))
	case "!=":
		return boolean(!equal(l, r))
	case "<", "<=", ">", ">=":
		if !lnum || !rnum {
			return undefined()
		}
		cmp, ok := compare(ln, rn)
		if !ok {
			x.fail("incompatible units %s and %s", unitName(ln.unit), unitName(rn.unit))
		}
		switch x.op {
		case "<":
			return boolean(cmp < 0)
		case "<=":
			return boolean(cmp <= 0)
		case ">":
			return boolean(cmp > 0)
		}
		return boolean(cmp >= 0)
	}
	if lnum && rnum {
		n, msg, ok := arith(x.op, ln, rn, false)
		if x.slash {
			if !ok {
				// No valid quotient (`0/50%`): only the separated pair
				// exists, as a slash list.
				return list{items: []value{ln, rn}, sep: '/'}
			}
			n.slash = &[2]number{ln, rn}
			return n
		}
		if !ok {
			x.fail("%s", msg)
		}
		return n
	}
	ls, lstr := l.(sstring)
	rs, rstr := r.(sstring)
	_, lcol := l.(color)
	_, rcol := r.(color)
	if x.op == "*" || x.op == "%" || (lcol || rcol) && !lstr && !rstr {
		// Colors only concatenate with strings; * and % need numbers.
		return undefined()
	}
	// Everything else concatenates, as grass does: `a + b` is `ab`,
	// `a - b` is `a-b`, `a / b` is `a/b`.
	switch {
	case x.op != "+":
		return sstring{text: css(l) + x.op + css(r)}
	case lcol:
		return sstring{text: css(l) + css(r)}
	case lstr && ls.quoted:
		return sstring{text: ls.text + escapeQuoted(unquoted(r)), quoted: true}
	case lstr:
		return sstring{text: ls.text + unquoted(r)}
	case rstr && rs.quoted:
		return sstring{text: escapeQuoted(css(l)) + rs.text, quoted: true}
	}
	return sstring{text: css(l) + css(r)}
}
