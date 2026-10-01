package usvg

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// This file ports simplecss 0.2: the stylesheet parser, selectors, and
// the declaration tokenizer for style attributes.

type cssStream struct {
	text string
	pos  int
}

func (s *cssStream) atEnd() bool { return s.pos >= len(s.text) }

func (s *cssStream) curr() (byte, bool) {
	if s.atEnd() {
		return 0, false
	}
	return s.text[s.pos], true
}

func (s *cssStream) isCurr(c byte) bool { b, ok := s.curr(); return ok && b == c }

func (s *cssStream) next() (byte, bool) {
	if s.pos+1 >= len(s.text) {
		return 0, false
	}
	return s.text[s.pos+1], true
}

func (s *cssStream) skipSpaces() {
	for !s.atEnd() {
		switch s.text[s.pos] {
		case ' ', '\t', '\n', '\r', '\f':
			s.pos++
		default:
			return
		}
	}
}

func (s *cssStream) skipSpacesAndComments() error {
	s.skipSpaces()
	for s.isCurr('/') {
		if n, ok := s.next(); !ok || n != '*' {
			break
		}
		if err := s.skipComment(); err != nil {
			return err
		}
		s.skipSpaces()
	}
	return nil
}

func (s *cssStream) skipComment() error {
	if s.consumeByte('/') != nil || s.consumeByte('*') != nil {
		return errParse
	}
	for !s.atEnd() {
		if s.text[s.pos] == '*' {
			if n, ok := s.next(); ok && n == '/' {
				break
			}
		}
		s.pos++
	}
	if s.consumeByte('*') != nil || s.consumeByte('/') != nil {
		return errParse
	}
	return nil
}

func (s *cssStream) consumeByte(c byte) error {
	if b, ok := s.curr(); !ok || b != c {
		return errParse
	}
	s.pos++
	return nil
}

func isNameStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 237
}

func isNameChar(r rune) bool {
	return isNameStart(r) || (r >= '0' && r <= '9') || r == '-'
}

func (s *cssStream) consumeIdent() (string, error) {
	start := s.pos
	if s.isCurr('-') {
		s.pos++
	}
	if !s.atEnd() {
		r, size := utf8.DecodeRuneInString(s.text[s.pos:])
		if !isNameStart(r) {
			return "", errParse
		}
		s.pos += size
	}
	for !s.atEnd() {
		r, size := utf8.DecodeRuneInString(s.text[s.pos:])
		if !isNameChar(r) {
			break
		}
		s.pos += size
	}
	if start == s.pos {
		return "", errParse
	}
	return s.text[start:s.pos], nil
}

func (s *cssStream) consumeString() (string, error) {
	quote, ok := s.curr()
	if !ok {
		return "", errParse
	}
	if quote != '\'' && quote != '"' {
		return s.consumeIdent()
	}
	prev := quote
	s.pos++
	start := s.pos
	for !s.atEnd() {
		c := s.text[s.pos]
		if c == quote && prev != '\\' {
			break
		}
		prev = c
		s.pos++
	}
	v := s.text[start:s.pos]
	if s.consumeByte(quote) != nil {
		return "", errParse
	}
	return v, nil
}

type declaration struct {
	name      string
	value     string
	important bool
}

type attrOp uint8

const (
	opExists attrOp = iota
	opMatches
	opContains
	opStartsWith
)

type subSelector struct {
	pseudo string // "first-child" etc., "" for an attribute test
	name   string
	op     attrOp
	value  string
}

type combinator uint8

const (
	combNone combinator = iota
	combDescendant
	combChild
	combAdjacent
)

type component struct {
	comb     combinator
	typeName string // "" for the universal selector
	subs     []subSelector
}

type selector struct{ components []component }

type cssRule struct {
	sel   selector
	decls []declaration
}

func (sel selector) specificity() [3]uint8 {
	var spec [3]uint8
	inc := func(i int) {
		if spec[i] < 255 {
			spec[i]++
		}
	}
	for _, c := range sel.components {
		if c.typeName != "" {
			inc(2)
		}
		for _, sub := range c.subs {
			if sub.pseudo == "" && sub.name == "id" {
				inc(0)
			} else {
				inc(1)
			}
		}
	}
	return spec
}

func (sel selector) matches(n *xmlNode) bool {
	return sel.matchesAt(len(sel.components)-1, n)
}

func (sel selector) matchesAt(idx int, n *xmlNode) bool {
	c := sel.components[idx]
	if c.typeName != "" && n.name != c.typeName {
		return false
	}
	for _, sub := range c.subs {
		if sub.pseudo != "" {
			if sub.pseudo != "first-child" || n.prevSiblingElement() != nil {
				return false
			}
			continue
		}
		v, ok := n.attrAny(sub.name)
		if !ok || !sub.op.match(v, sub.value) {
			return false
		}
	}
	switch c.comb {
	case combDescendant:
		for p := n.parentElement(); p != nil; p = p.parentElement() {
			if sel.matchesAt(idx-1, p) {
				return true
			}
		}
		return false
	case combChild:
		p := n.parentElement()
		return p != nil && sel.matchesAt(idx-1, p)
	case combAdjacent:
		p := n.prevSiblingElement()
		return p != nil && sel.matchesAt(idx-1, p)
	default:
		return true
	}
}

func (n *xmlNode) parentElement() *xmlNode {
	if n.parent == nil || n.parent.parent == nil {
		return nil
	}
	return n.parent
}

// attrAny is roxmltree's attribute(local_name): the first attribute
// with that local name and no namespace.
func (n *xmlNode) attrAny(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name && a.space == "" {
			return a.value, true
		}
	}
	return "", false
}

func (op attrOp) match(value, want string) bool {
	switch op {
	case opExists:
		return true
	case opMatches:
		return value == want
	case opContains:
		return slices.Contains(strings.Split(value, " "), want)
	default:
		if value == want {
			return true
		}
		return strings.HasPrefix(value, want) && len(value) > len(want) && value[len(want)] == '-'
	}
}

// parseSelector is selector::parse: the selector and the bytes read.
func parseSelector(text string) (selector, bool, int) {
	s := &cssStream{text: text}
	var comps []component
	comb := combNone
	afterComb := true
	finished := false
	addSub := func(sub subSelector) {
		if comb == combNone && len(comps) > 0 {
			comps[len(comps)-1].subs = append(comps[len(comps)-1].subs, sub)
			return
		}
		comps = append(comps, component{comb: comb, subs: []subSelector{sub}})
		comb = combNone
	}
	fail := func() (selector, bool, int) { return selector{}, false, s.pos }
	for {
		if finished || s.atEnd() {
			if afterComb {
				return fail()
			}
			break
		}
		c := s.text[s.pos]
		switch c {
		case '*':
			if !afterComb {
				return fail()
			}
			afterComb = false
			s.pos++
			comps = append(comps, component{comb: comb})
			comb = combNone
		case '#', '.':
			afterComb = false
			s.pos++
			ident, err := s.consumeIdent()
			if err != nil {
				return fail()
			}
			if c == '#' {
				addSub(subSelector{name: "id", op: opMatches, value: ident})
			} else {
				addSub(subSelector{name: "class", op: opContains, value: ident})
			}
		case '[':
			afterComb = false
			s.pos++
			ident, err := s.consumeIdent()
			if err != nil {
				return fail()
			}
			b, ok := s.curr()
			if !ok {
				return fail()
			}
			sub := subSelector{name: ident}
			switch b {
			case ']':
				sub.op = opExists
			case '=':
				s.pos++
				if sub.value, err = s.consumeString(); err != nil {
					return fail()
				}
				sub.op = opMatches
			case '~', '|':
				s.pos++
				if s.consumeByte('=') != nil {
					return fail()
				}
				if sub.value, err = s.consumeString(); err != nil {
					return fail()
				}
				sub.op = opContains
				if b == '|' {
					sub.op = opStartsWith
				}
			default:
				return fail()
			}
			if s.consumeByte(']') != nil {
				return fail()
			}
			addSub(sub)
		case ':':
			afterComb = false
			s.pos++
			ident, err := s.consumeIdent()
			if err != nil {
				return fail()
			}
			if ident == "lang" {
				// :lang() never matches a static SVG in usvg.
				if s.consumeByte('(') != nil {
					return fail()
				}
				start := s.pos
				for !s.atEnd() && s.text[s.pos] != ')' {
					s.pos++
				}
				lang := strings.TrimSpace(s.text[start:s.pos])
				if s.consumeByte(')') != nil || lang == "" {
					return fail()
				}
				addSub(subSelector{pseudo: "lang"})
				break
			}
			switch ident {
			case "first-child", "link", "visited", "hover", "active", "focus":
				addSub(subSelector{pseudo: ident})
			default:
				return fail()
			}
		case '>', '+':
			if afterComb {
				return fail()
			}
			s.pos++
			afterComb = true
			if c == '>' {
				comb = combChild
			} else {
				comb = combAdjacent
			}
		case ' ', '\t', '\n', '\r', '\f':
			s.skipSpaces()
			if afterComb {
				continue
			}
			for s.isCurr('/') {
				if s.skipComment() != nil {
					return fail()
				}
				s.skipSpaces()
			}
			b, ok := s.curr()
			if !ok || b == '>' || b == '+' || b == ',' || b == '{' {
				continue
			}
			afterComb = true
			comb = combDescendant
		case '/':
			if n, ok := s.next(); ok && n == '*' {
				if s.skipComment() != nil {
					return fail()
				}
			} else {
				finished = true
			}
		case ',', '{':
			finished = true
		default:
			ident, err := s.consumeIdent()
			if err != nil {
				return fail()
			}
			if !afterComb {
				return fail()
			}
			afterComb = false
			comps = append(comps, component{comb: comb, typeName: ident})
			comb = combNone
		}
	}
	if len(comps) == 0 || comps[0].comb != combNone {
		return selector{}, false, s.pos
	}
	return selector{components: comps}, true, s.pos
}

// parseStyleSheet is StyleSheet::parse_more over every text, then the
// empty rules dropped and the rest stably sorted by specificity.
func parseStyleSheet(texts []string) []cssRule {
	var rules []cssRule
	for _, text := range texts {
		s := &cssStream{text: text}
		if s.skipSpacesAndComments() != nil {
			continue
		}
		for !s.atEnd() {
			if s.skipSpacesAndComments() != nil {
				break
			}
			if s.atEnd() {
				break
			}
			if s.isCurr('@') {
				// A malformed at-rule is skipped; parsing resumes after the '@'.
				s.pos++
				_ = consumeAtRule(s)
			} else if consumeRuleSet(s, &rules) != nil {
				break
			}
		}
	}
	kept := rules[:0]
	for _, r := range rules {
		if len(r.decls) > 0 {
			kept = append(kept, r)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i].sel.specificity(), kept[j].sel.specificity()
		for k := range 3 {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	return kept
}

func consumeAtRule(s *cssStream) error {
	if _, err := s.consumeIdent(); err != nil {
		return err
	}
	for !s.atEnd() && s.text[s.pos] != ';' && s.text[s.pos] != '{' {
		s.pos++
	}
	c, ok := s.curr()
	if !ok {
		return errParse
	}
	if c == ';' {
		s.pos++
	} else {
		s.pos++
		consumeUntilBlockEnd(s)
	}
	return nil
}

func consumeRuleSet(s *cssStream, rules *[]cssRule) error {
	start := len(*rules)
	for {
		c, ok := s.curr()
		if !ok {
			return errParse
		}
		if c != ',' && start != len(*rules) {
			break
		}
		if c == ',' {
			s.pos++
		}
		sel, ok, n := parseSelector(s.text[s.pos:])
		s.pos += n
		s.skipSpaces()
		if ok {
			*rules = append(*rules, cssRule{sel: sel})
		}
		c, ok = s.curr()
		if !ok {
			return errParse
		}
		if c == '{' {
			break
		}
		if c != ',' {
			for !s.atEnd() && s.text[s.pos] != '{' {
				s.pos++
			}
			break
		}
	}
	if s.isCurr('{') {
		s.pos++
	}
	decls := consumeDeclarations(s)
	for i := start; i < len(*rules); i++ {
		(*rules)[i].decls = decls
	}
	if s.isCurr('}') {
		s.pos++
	}
	return nil
}

func consumeUntilBlockEnd(s *cssStream) {
	braces := 0
	for !s.atEnd() {
		switch s.text[s.pos] {
		case '{':
			braces++
		case '}':
			if braces == 0 {
				s.pos++
				return
			}
			braces--
		}
		s.pos++
	}
}

func consumeDeclarations(s *cssStream) []declaration {
	var decls []declaration
	for !s.atEnd() && !s.isCurr('}') {
		d, err := consumeDeclaration(s)
		if err != nil {
			consumeUntilBlockEnd(s)
			break
		}
		decls = append(decls, d)
	}
	return decls
}

// parseStyleAttr is DeclarationTokenizer over a style attribute.
func parseStyleAttr(text string) []declaration {
	s := &cssStream{text: text}
	var out []declaration
	for {
		_ = s.skipSpacesAndComments()
		if s.atEnd() {
			return out
		}
		d, err := consumeDeclaration(s)
		if err != nil {
			return out
		}
		out = append(out, d)
	}
}

func consumeDeclaration(s *cssStream) (declaration, error) {
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	if s.isCurr('*') {
		s.pos++
	}
	name, err := s.consumeIdent()
	if err != nil {
		return declaration{}, err
	}
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	if err := s.consumeByte(':'); err != nil {
		return declaration{}, err
	}
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	start, end := s.pos, s.pos
	for consumeTerm(s) == nil {
		end = s.pos
		if err := s.skipSpacesAndComments(); err != nil {
			return declaration{}, err
		}
	}
	value := strings.TrimSpace(s.text[start:end])
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	important := false
	if s.isCurr('!') {
		s.pos++
		if err := s.skipSpacesAndComments(); err != nil {
			return declaration{}, err
		}
		if strings.HasPrefix(s.text[s.pos:], "important") {
			s.pos += 9
			important = true
		}
	}
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	for s.isCurr(';') {
		s.pos++
		if err := s.skipSpacesAndComments(); err != nil {
			return declaration{}, err
		}
	}
	if err := s.skipSpacesAndComments(); err != nil {
		return declaration{}, err
	}
	if value == "" {
		return declaration{}, errParse
	}
	return declaration{name: name, value: value, important: important}, nil
}

func consumeTerm(s *cssStream) error {
	digits := func() {
		for !s.atEnd() && s.text[s.pos] >= '0' && s.text[s.pos] <= '9' {
			s.pos++
		}
	}
	c, ok := s.curr()
	if !ok {
		return errParse
	}
	switch {
	case c == '#':
		s.pos++
		if _, err := s.consumeIdent(); err != nil {
			for !s.atEnd() && isHex(s.text[s.pos]) {
				s.pos++
			}
		}
	case c == '+' || c == '-' || (c >= '0' && c <= '9') || c == '.':
		s.pos++
		digits()
		if s.isCurr('.') {
			s.pos++
			digits()
		}
		if s.isCurr('%') {
			s.pos++
		} else {
			_, _ = s.consumeIdent()
		}
	case c == '\'' || c == '"':
		if _, err := s.consumeString(); err != nil {
			return err
		}
	case c == ',':
		s.pos++
	default:
		if _, err := s.consumeIdent(); err != nil {
			return err
		}
		if s.isCurr('(') {
			for !s.atEnd() && s.text[s.pos] != ')' {
				s.pos++
			}
			if err := s.consumeByte(')'); err != nil {
				return err
			}
		}
	}
	return nil
}
