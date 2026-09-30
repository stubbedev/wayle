package i18n

import (
	"slices"
	"strings"
)

// parser is a byte-for-byte port of fluent-syntax 0.12's runtime
// parser (parser/{runtime,core,pattern,expression,helper}.rs), the one
// fluent-bundle's FluentResource::try_new runs: comments are skipped,
// an entry that fails to parse becomes junk up to the next line that
// starts with a letter, '-', or '#', and parsing carries on.
type parser struct {
	src string
	ptr int
}

// parseResource parses an FTL source. It never fails: unreadable
// entries land in resource.junk.
func parseResource(src string) *resource {
	p := &parser{src: src}
	res := &resource{}
	p.skipBlankBlock()
	for p.ptr < len(p.src) {
		start := p.ptr
		e, err := p.entryRuntime(start)
		switch {
		case err != nil:
			p.skipToNextEntryStart()
			res.junk = append(res.junk, junkEntry{content: p.src[start:p.ptr], err: err})
		case e != nil:
			res.entries = append(res.entries, *e)
		}
		p.skipBlankBlock()
	}
	return res
}

func (p *parser) fail(kind string) *parseError { return &parseError{kind: kind, pos: p.ptr} }

func (p *parser) cur() (byte, bool) {
	if p.ptr < len(p.src) {
		return p.src[p.ptr], true
	}
	return 0, false
}

func (p *parser) byteAt(pos int) (byte, bool) {
	if pos >= 0 && pos < len(p.src) {
		return p.src[pos], true
	}
	return 0, false
}

func (p *parser) isCurrent(b byte) bool {
	c, ok := p.cur()
	return ok && c == b
}

func (p *parser) isByteAt(b byte, pos int) bool {
	c, ok := p.byteAt(pos)
	return ok && c == b
}

func (p *parser) takeIf(b byte) bool {
	if p.isCurrent(b) {
		p.ptr++
		return true
	}
	return false
}

func (p *parser) expect(b byte) *parseError {
	if !p.isCurrent(b) {
		return p.fail("expected token " + string(b))
	}
	p.ptr++
	return nil
}

func (p *parser) entryRuntime(start int) (*entry, *parseError) {
	c, _ := p.cur()
	switch c {
	case '#':
		p.skipComment()
		return nil, nil
	case '-':
		return p.term(start)
	default:
		return p.message(start)
	}
}

func (p *parser) skipComment() {
	for {
		for p.ptr < len(p.src) && !p.isEOL() {
			p.ptr++
		}
		p.ptr++
		if p.isCurrent('#') {
			p.ptr++
		} else {
			break
		}
	}
}

func (p *parser) message(start int) (*entry, *parseError) {
	id, err := p.identifier()
	if err != nil {
		return nil, err
	}
	p.skipBlankInline()
	if err := p.expect('='); err != nil {
		return nil, err
	}
	value, err := p.pattern()
	if err != nil {
		return nil, err
	}
	p.skipBlankBlock()
	attrs := p.attributes()
	if value == nil && len(attrs) == 0 {
		return nil, &parseError{kind: "expected message field for " + id, pos: start}
	}
	return &entry{id: id, value: value, attrs: attrs}, nil
}

func (p *parser) term(start int) (*entry, *parseError) {
	if err := p.expect('-'); err != nil {
		return nil, err
	}
	id, err := p.identifier()
	if err != nil {
		return nil, err
	}
	p.skipBlankInline()
	if err := p.expect('='); err != nil {
		return nil, err
	}
	p.skipBlankInline()
	value, err := p.pattern()
	if err != nil {
		return nil, err
	}
	p.skipBlankBlock()
	attrs := p.attributes()
	if value == nil {
		return nil, &parseError{kind: "expected term field for " + id, pos: start}
	}
	return &entry{term: true, id: id, value: value, attrs: attrs}, nil
}

func (p *parser) attributes() []attribute {
	var attrs []attribute
	for {
		lineStart := p.ptr
		p.skipBlankInline()
		if !p.takeIf('.') {
			p.ptr = lineStart
			return attrs
		}
		attr, err := p.attribute()
		if err != nil {
			p.ptr = lineStart
			return attrs
		}
		attrs = append(attrs, attr)
	}
}

func (p *parser) attribute() (attribute, *parseError) {
	id, err := p.identifier()
	if err != nil {
		return attribute{}, err
	}
	p.skipBlankInline()
	if err := p.expect('='); err != nil {
		return attribute{}, err
	}
	value, err := p.pattern()
	if err != nil {
		return attribute{}, err
	}
	if value == nil {
		return attribute{}, p.fail("missing value")
	}
	return attribute{id: id, value: value}, nil
}

func isAlpha(b byte) bool     { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isDigit(b byte) bool     { return b >= '0' && b <= '9' }
func isIdentByte(b byte) bool { return isAlpha(b) || isDigit(b) || b == '-' || b == '_' }

// identifierRest consumes [a-zA-Z0-9_-]* after an already-consumed
// first letter and returns the whole identifier.
func (p *parser) identifierRest() string {
	start := p.ptr - 1
	for {
		c, ok := p.cur()
		if !ok || !isIdentByte(c) {
			break
		}
		p.ptr++
	}
	return p.src[start:p.ptr]
}

func (p *parser) identifier() (string, *parseError) {
	c, ok := p.cur()
	if !ok || !isAlpha(c) {
		return "", p.fail("expected a-zA-Z")
	}
	p.ptr++
	return p.identifierRest(), nil
}

func (p *parser) attributeAccessor() (string, *parseError) {
	if !p.takeIf('.') {
		return "", nil
	}
	return p.identifier()
}

func (p *parser) skipToNextEntryStart() {
	for p.ptr < len(p.src) {
		b := p.src[p.ptr]
		newLine := p.ptr == 0 || p.src[p.ptr-1] == '\n'
		if newLine && (isAlpha(b) || b == '-' || b == '#') {
			return
		}
		p.ptr++
	}
}

func (p *parser) skipEOL() bool {
	switch {
	case p.isCurrent('\n'):
		p.ptr++
		return true
	case p.isCurrent('\r') && p.isByteAt('\n', p.ptr+1):
		p.ptr += 2
		return true
	}
	return false
}

func (p *parser) isEOL() bool {
	c, ok := p.cur()
	switch {
	case !ok, c == '\n':
		return true
	case c == '\r':
		return p.isByteAt('\n', p.ptr+1)
	}
	return false
}

func (p *parser) skipBlankInline() int {
	start := p.ptr
	for p.isCurrent(' ') {
		p.ptr++
	}
	return p.ptr - start
}

func (p *parser) skipBlankBlock() {
	for {
		start := p.ptr
		p.skipBlankInline()
		if !p.skipEOL() {
			p.ptr = start
			return
		}
	}
}

func (p *parser) skipBlank() {
	for {
		switch {
		case p.isCurrent(' '), p.isCurrent('\n'):
			p.ptr++
		case p.isCurrent('\r') && p.isByteAt('\n', p.ptr+1):
			p.ptr += 2
		default:
			return
		}
	}
}

func (p *parser) skipDigits() *parseError {
	start := p.ptr
	for {
		c, ok := p.cur()
		if !ok || !isDigit(c) {
			break
		}
		p.ptr++
	}
	if start == p.ptr {
		return p.fail("expected 0-9")
	}
	return nil
}

func (p *parser) numberLiteral() (string, *parseError) {
	start := p.ptr
	p.takeIf('-')
	if err := p.skipDigits(); err != nil {
		return "", err
	}
	if p.takeIf('.') {
		if err := p.skipDigits(); err != nil {
			return "", err
		}
	}
	return p.src[start:p.ptr], nil
}

func (p *parser) isNumberStart() bool {
	c, ok := p.cur()
	return ok && (isDigit(c) || c == '-')
}

func (p *parser) skipUnicodeEscape(length int) *parseError {
	start := p.ptr
	for range length {
		c, ok := p.cur()
		if !ok || !isHex(c) {
			break
		}
		p.ptr++
	}
	if p.ptr-start != length {
		return p.fail("invalid unicode escape sequence")
	}
	return nil
}

func isHex(b byte) bool { return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') }

func isPatternContinuation(b byte) bool {
	return b != '.' && b != '}' && b != '[' && b != '*'
}

// textPosition tracks where a text element sits, which drives the
// dedent (pattern.rs TextElementPosition).
type textPosition int

const (
	initialLineStart textPosition = iota
	lineStart
	continuation
)

type termination int

const (
	byLineFeed termination = iota
	byCRLF
	byPlaceableStart
	byEOF
)

// placeholder is a text element still expressed as source offsets, or
// a parsed placeable.
type placeholder struct {
	expr               expression
	start, end, indent int
	role               textPosition
}

// pattern ports get_pattern: inline and block text, placeables, and
// the common-indent dedent. It returns nil for a pattern with no
// non-blank element.
func (p *parser) pattern() (*pattern, *parseError) {
	var elements []placeholder
	lastNonBlank := -1
	commonIndent := -1

	p.skipBlankInline()
	role := initialLineStart
	if p.skipEOL() {
		p.skipBlankBlock()
		role = lineStart
	}

	for p.ptr < len(p.src) {
		if p.takeIf('{') {
			if role == lineStart {
				commonIndent = 0
			}
			exp, err := p.placeable()
			if err != nil {
				return nil, err
			}
			lastNonBlank = len(elements)
			elements = append(elements, placeholder{expr: exp})
			role = continuation
			continue
		}
		sliceStart := p.ptr
		indent := 0
		if role == lineStart {
			indent = p.skipBlankInline()
			c, ok := p.cur()
			if !ok {
				break
			}
			if indent == 0 {
				if c != '\r' && c != '\n' {
					break
				}
			} else if !isPatternContinuation(c) {
				p.ptr = sliceStart
				break
			}
		}
		start, end, blank, term, err := p.textSlice()
		if err != nil {
			return nil, err
		}
		if start != end {
			if role == lineStart && !blank {
				if commonIndent < 0 || indent < commonIndent {
					commonIndent = indent
				}
			}
			if role != lineStart || !blank || term == byLineFeed {
				if !blank {
					lastNonBlank = len(elements)
				}
				elements = append(elements, placeholder{start: sliceStart, end: end, indent: indent, role: role})
			}
		}
		switch term {
		case byLineFeed, byCRLF:
			role = lineStart
		default:
			role = continuation
		}
	}

	if lastNonBlank < 0 {
		return nil, nil
	}
	out := &pattern{}
	for i, el := range elements[:lastNonBlank+1] {
		if el.expr != nil {
			out.elements = append(out.elements, element{expr: el.expr})
			continue
		}
		start := el.start
		if el.role == lineStart {
			if commonIndent < 0 {
				start += el.indent
			} else {
				start += min(el.indent, commonIndent)
			}
		}
		value := p.src[start:el.end]
		if i == lastNonBlank {
			value = trimFluentWS(value)
		}
		out.elements = append(out.elements, element{text: value})
	}
	return out, nil
}

// trimFluentWS trims trailing ' ', '\r', and '\n' (slice.rs trim).
func trimFluentWS(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[:end]
}

// textSlice ports get_text_slice: text runs up to a newline, a '{',
// or EOF; a '}' is an unbalanced-brace error.
func (p *parser) textSlice() (start, end int, blank bool, term termination, err *parseError) {
	start = p.ptr
	rest := p.src[p.ptr:]
	stop := strings.IndexAny(rest, "\n{}")
	isBlank := func(text string) bool { return strings.Trim(text, " ") == "" }
	if stop < 0 {
		p.ptr += len(rest)
		return start, p.ptr, isBlank(rest), byEOF, nil
	}
	text := rest[:stop]
	switch rest[stop] {
	case '}':
		p.ptr += len(text)
		return 0, 0, false, 0, p.fail("unbalanced closing brace")
	case '\n':
		if len(text) > 0 && text[len(text)-1] == '\r' {
			// get_text_slice stops on the '\r' and leaves the '\n' for
			// the next line start, exactly as fluent-syntax does.
			text = text[:len(text)-1]
			p.ptr += len(text) + 1
			return start, p.ptr - 1, isBlank(text), byCRLF, nil
		}
		p.ptr += len(text) + 1
		return start, p.ptr, isBlank(text), byLineFeed, nil
	default: // '{'
		p.ptr += len(text)
		return start, p.ptr, isBlank(text), byPlaceableStart, nil
	}
}

func (p *parser) placeable() (expression, *parseError) {
	p.skipBlank()
	exp, err := p.expression()
	if err != nil {
		return nil, err
	}
	p.skipBlankInline()
	if err := p.expect('}'); err != nil {
		return nil, err
	}
	if t, ok := exp.(termRef); ok && t.attr != "" {
		return nil, p.fail("term attribute as placeable")
	}
	return exp, nil
}

func (p *parser) expression() (expression, *parseError) {
	exp, err := p.inlineExpression(false)
	if err != nil {
		return nil, err
	}
	p.skipBlank()
	if !p.isCurrent('-') || !p.isByteAt('>', p.ptr+1) {
		if t, ok := exp.(termRef); ok && t.attr != "" {
			return nil, p.fail("term attribute as placeable")
		}
		return exp, nil
	}
	switch e := exp.(type) {
	case messageRef:
		if e.attr == "" {
			return nil, p.fail("message reference as selector")
		}
		return nil, p.fail("message attribute as selector")
	case termRef:
		if e.attr == "" {
			return nil, p.fail("term reference as selector")
		}
	case stringLiteral, numberLiteral, variableRef, functionRef:
	default:
		return nil, p.fail("expected simple expression as selector")
	}
	p.ptr += 2 // ->
	p.skipBlankInline()
	if !p.skipEOL() {
		return nil, p.fail("expected line end after ->")
	}
	p.skipBlank()
	variants, err := p.variants()
	if err != nil {
		return nil, err
	}
	return selectExpr{selector: exp, variants: variants}, nil
}

func (p *parser) variants() ([]variant, *parseError) {
	var variants []variant
	hasDefault := false
	for {
		def := p.takeIf('*')
		if def {
			if hasDefault {
				return nil, p.fail("multiple default variants")
			}
			hasDefault = true
		}
		if !p.takeIf('[') {
			break
		}
		key, numeric, err := p.variantKey()
		if err != nil {
			return nil, err
		}
		value, err := p.pattern()
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, p.fail("missing value")
		}
		variants = append(variants, variant{key: key, numberKey: numeric, value: value, isDefault: def})
		p.skipBlank()
	}
	if !hasDefault {
		return nil, p.fail("missing default variant")
	}
	return variants, nil
}

func (p *parser) variantKey() (string, bool, *parseError) {
	p.skipBlank()
	var key string
	numeric := p.isNumberStart()
	var err *parseError
	if numeric {
		key, err = p.numberLiteral()
	} else {
		key, err = p.identifier()
	}
	if err != nil {
		return "", false, err
	}
	p.skipBlank()
	if err := p.expect(']'); err != nil {
		return "", false, err
	}
	return key, numeric, nil
}

func isCallee(name string) bool {
	for i := range len(name) {
		if c := name[i]; (c < 'A' || c > 'Z') && !isDigit(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func (p *parser) inlineExpression(onlyLiteral bool) (inlineExpr, *parseError) {
	c, ok := p.cur()
	switch {
	case ok && c == '"':
		return p.stringLiteral()
	case ok && isDigit(c):
		num, err := p.numberLiteral()
		if err != nil {
			return nil, err
		}
		return numberLiteral{raw: num}, nil
	case ok && c == '-' && !onlyLiteral:
		p.ptr++
		if n, ok := p.cur(); ok && isAlpha(n) {
			p.ptr++
			id := p.identifierRest()
			attr, err := p.attributeAccessor()
			if err != nil {
				return nil, err
			}
			args, err := p.callArguments()
			if err != nil {
				return nil, err
			}
			return termRef{id: id, attr: attr, args: args}, nil
		}
		p.ptr--
		num, err := p.numberLiteral()
		if err != nil {
			return nil, err
		}
		return numberLiteral{raw: num}, nil
	case ok && c == '$' && !onlyLiteral:
		p.ptr++
		id, err := p.identifier()
		if err != nil {
			return nil, err
		}
		return variableRef{id: id}, nil
	case ok && isAlpha(c):
		p.ptr++
		id := p.identifierRest()
		args, err := p.callArguments()
		if err != nil {
			return nil, err
		}
		if args != nil {
			if !isCallee(id) {
				return nil, p.fail("forbidden callee")
			}
			return functionRef{id: id, args: *args}, nil
		}
		attr, err := p.attributeAccessor()
		if err != nil {
			return nil, err
		}
		return messageRef{id: id, attr: attr}, nil
	case ok && c == '{' && !onlyLiteral:
		p.ptr++
		exp, err := p.placeable()
		if err != nil {
			return nil, err
		}
		return nestedPlaceable{expr: exp}, nil
	case onlyLiteral:
		return nil, p.fail("expected literal")
	default:
		return nil, p.fail("expected inline expression")
	}
}

func (p *parser) stringLiteral() (inlineExpr, *parseError) {
	p.ptr++ // "
	start := p.ptr
	for p.ptr < len(p.src) {
		b := p.src[p.ptr]
		if b == '"' {
			break
		}
		if b == '\n' {
			return nil, p.fail("unterminated string literal")
		}
		if b != '\\' {
			p.ptr++
			continue
		}
		next, _ := p.byteAt(p.ptr + 1)
		switch next {
		case '\\', '{', '"':
			p.ptr += 2
		case 'u':
			p.ptr += 2
			if err := p.skipUnicodeEscape(4); err != nil {
				return nil, err
			}
		case 'U':
			p.ptr += 2
			if err := p.skipUnicodeEscape(6); err != nil {
				return nil, err
			}
		default:
			return nil, p.fail("unknown escape sequence")
		}
	}
	if err := p.expect('"'); err != nil {
		return nil, err
	}
	return stringLiteral{raw: p.src[start : p.ptr-1]}, nil
}

func (p *parser) callArguments() (*callArgs, *parseError) {
	p.skipBlank()
	if !p.takeIf('(') {
		return nil, nil
	}
	args := &callArgs{}
	var names []string
	p.skipBlank()
	for p.ptr < len(p.src) {
		if p.isCurrent(')') {
			break
		}
		exp, err := p.inlineExpression(false)
		if err != nil {
			return nil, err
		}
		if ref, ok := exp.(messageRef); ok && ref.attr == "" {
			p.skipBlank()
			if p.isCurrent(':') {
				if slices.Contains(names, ref.id) {
					return nil, p.fail("duplicated named argument " + ref.id)
				}
				p.ptr++
				p.skipBlank()
				val, err := p.inlineExpression(true)
				if err != nil {
					return nil, err
				}
				names = append(names, ref.id)
				args.named = append(args.named, namedArg{name: ref.id, value: val})
			} else {
				if len(names) > 0 {
					return nil, p.fail("positional argument follows named")
				}
				args.positional = append(args.positional, exp)
			}
		} else {
			if len(names) > 0 {
				return nil, p.fail("positional argument follows named")
			}
			args.positional = append(args.positional, exp)
		}
		p.skipBlank()
		p.takeIf(',')
		p.skipBlank()
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return args, nil
}
