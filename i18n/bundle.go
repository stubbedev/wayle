package i18n

import (
	"slices"
	"strings"
)

// Unicode isolation marks fluent-bundle wraps interpolated placeables
// in (use_isolating defaults to true and i18n-embed never clears it).
const (
	fsi = '\u2068'
	pdi = '\u2069'
)

// maxPlaceables is fluent-bundle's billion-laughs guard.
const maxPlaceables = 100

// bundle is one language's resource: fluent-bundle's FluentBundle with
// a single locale and no registered functions (i18n-embed adds none).
type bundle struct {
	locale   LangID
	plural   pluralRule
	messages map[string]*entry
	terms    map[string]*entry
}

// newBundle adds a resource the way FluentBundle::add_resource does: a
// duplicate id keeps the first definition.
func newBundle(locale LangID, res *resource) *bundle {
	_, rule := pluralRuleFor(locale)
	b := &bundle{
		locale:   locale,
		plural:   rule,
		messages: map[string]*entry{},
		terms:    map[string]*entry{},
	}
	for i := range res.entries {
		e := &res.entries[i]
		table := b.messages
		if e.term {
			table = b.terms
		}
		if _, dup := table[e.id]; !dup {
			table[e.id] = e
		}
	}
	return b
}

func (e *entry) attribute(name string) *pattern {
	for _, a := range e.attrs {
		if a.id == name {
			return a.value
		}
	}
	return nil
}

// format resolves a pattern with the caller's arguments
// (FluentBundle::format_pattern); resolution errors are swallowed the
// way i18n-embed only logs them, leaving fluent's inline {...} markers.
func (b *bundle) format(p *pattern, args map[string]value) string {
	s := &scope{bundle: b, args: args}
	var out strings.Builder
	s.writePattern(&out, p)
	return out.String()
}

// scope is fluent-bundle's resolver Scope for one format call.
type scope struct {
	bundle     *bundle
	args       map[string]value
	localArgs  map[string]value // non-nil while resolving a term
	placeables int
	traveled   []*pattern
	dirty      bool
}

func (s *scope) writePattern(w *strings.Builder, p *pattern) {
	for _, el := range p.elements {
		if s.dirty {
			return
		}
		if el.expr == nil {
			w.WriteString(el.text)
			continue
		}
		s.placeables++
		if s.placeables > maxPlaceables {
			s.dirty = true
			return
		}
		isolate := len(p.elements) > 1 && needsIsolation(el.expr)
		if isolate {
			w.WriteRune(fsi)
		}
		// maybe_track: the top-level pattern joins the cycle guard
		// lazily, on its first placeable.
		if len(s.traveled) == 0 {
			s.traveled = append(s.traveled, p)
		}
		s.writeExpression(w, el.expr)
		if s.dirty {
			w.WriteByte('{')
			writeError(w, el.expr)
			w.WriteByte('}')
		}
		if isolate {
			w.WriteRune(pdi)
		}
	}
}

// needsIsolation exempts message references, term references, and
// string literals, as pattern.rs does.
func needsIsolation(e expression) bool {
	switch e.(type) {
	case messageRef, termRef, stringLiteral:
		return false
	}
	return true
}

func (s *scope) writeExpression(w *strings.Builder, e expression) {
	var sel selectExpr
	switch x := e.(type) {
	case inlineExpr:
		s.writeInline(w, x)
		return
	case selectExpr:
		sel = x
	}
	selector := s.resolveInline(sel.selector)
	if selector.kind == valueString || selector.kind == valueNumber {
		for _, v := range sel.variants {
			key := stringValue(v.key)
			if v.numberKey {
				key = tryNumber(v.key)
			}
			if s.matches(key, selector) {
				s.writePattern(w, v.value)
				return
			}
		}
	}
	for _, v := range sel.variants {
		if v.isDefault {
			s.writePattern(w, v.value)
			return
		}
	}
}

// matches ports FluentValue::matches: equal strings, equal numbers
// (value and fraction digits), or a plural keyword that names the
// number's cardinal category.
func (s *scope) matches(key, selector value) bool {
	switch {
	case key.kind == valueString && selector.kind == valueString:
		return key.str == selector.str
	case key.kind == valueNumber && selector.kind == valueNumber:
		return key.num == selector.num
	case key.kind == valueString && selector.kind == valueNumber:
		if !slices.Contains([]string{"zero", "one", "two", "few", "many", "other"}, key.str) {
			return false
		}
		ops, ok := selector.num.pluralOperands()
		return ok && s.bundle.plural(ops) == key.str
	}
	return false
}

func (s *scope) writeInline(w *strings.Builder, e inlineExpr) {
	switch x := e.(type) {
	case stringLiteral:
		w.WriteString(unescape(x.raw))
	case numberLiteral:
		tryNumber(x.raw).write(w)
	case messageRef:
		msg := s.bundle.messages[x.id]
		switch {
		case msg == nil:
			s.writeRefError(w, x)
		case x.attr != "":
			if attr := msg.attribute(x.attr); attr != nil {
				s.track(w, attr, x)
			} else {
				s.writeRefError(w, x)
			}
		case msg.value != nil:
			s.track(w, msg.value, x)
		default:
			s.writeRefError(w, x) // a message with attributes only
		}
	case termRef:
		s.localArgs = s.namedArgs(x.args)
		term := s.bundle.terms[x.id]
		switch {
		case term == nil:
			s.writeRefError(w, x)
		case x.attr != "":
			if attr := term.attribute(x.attr); attr != nil {
				s.track(w, attr, x)
			} else {
				s.writeRefError(w, x)
			}
		default:
			s.track(w, term.value, x)
		}
		s.localArgs = nil
	case functionRef:
		// No functions are registered, so every call is a reference
		// error, as with a bare i18n-embed bundle.
		s.namedArgs(&x.args)
		s.writeRefError(w, x)
	case variableRef:
		args := s.args
		if s.localArgs != nil {
			args = s.localArgs
		}
		if v, ok := args[x.id]; ok {
			v.write(w)
			return
		}
		s.writeRefError(w, x)
	case nestedPlaceable:
		s.writeExpression(w, x.expr)
	}
}

// resolveInline is InlineExpression::resolve, used for selectors.
func (s *scope) resolveInline(e inlineExpr) value {
	switch x := e.(type) {
	case stringLiteral:
		return stringValue(unescape(x.raw))
	case numberLiteral:
		return tryNumber(x.raw)
	case variableRef:
		if s.localArgs != nil {
			if v, ok := s.localArgs[x.id]; ok {
				return v
			}
		} else if v, ok := s.args[x.id]; ok {
			return v
		}
		return value{kind: valueError}
	case functionRef:
		return value{kind: valueError}
	}
	var b strings.Builder
	s.writeInline(&b, e)
	return stringValue(b.String())
}

// namedArgs resolves a term call's named arguments; positional ones
// are evaluated and dropped, as get_arguments does for terms.
func (s *scope) namedArgs(args *callArgs) map[string]value {
	named := map[string]value{}
	if args == nil {
		return named
	}
	for _, p := range args.positional {
		s.resolveInline(p)
	}
	for _, n := range args.named {
		named[n.name] = s.resolveInline(n.value)
	}
	return named
}

func (s *scope) track(w *strings.Builder, p *pattern, e inlineExpr) {
	if slices.Contains(s.traveled, p) {
		s.writeRefError(w, e) // a cycle
		return
	}
	s.traveled = append(s.traveled, p)
	s.writePattern(w, p)
	s.traveled = s.traveled[:len(s.traveled)-1]
}

func (s *scope) writeRefError(w *strings.Builder, e inlineExpr) {
	w.WriteByte('{')
	writeError(w, e)
	w.WriteByte('}')
}

// writeError is write_error: the source-ish spelling of the reference
// that failed.
func writeError(w *strings.Builder, e expression) {
	switch x := e.(type) {
	case messageRef:
		w.WriteString(x.id)
		if x.attr != "" {
			w.WriteString("." + x.attr)
		}
	case termRef:
		w.WriteString("-" + x.id)
		if x.attr != "" {
			w.WriteString("." + x.attr)
		}
	case functionRef:
		w.WriteString(x.id + "()")
	case variableRef:
		w.WriteString("$" + x.id)
	case selectExpr:
		writeError(w, x.selector)
	case nestedPlaceable:
		writeError(w, x.expr)
	}
}
