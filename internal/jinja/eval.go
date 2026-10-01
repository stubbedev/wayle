package jinja

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
)

// Error is a template syntax or render error.
type Error struct{ msg string }

func (e *Error) Error() string { return "template: " + e.msg }

func errorf(format string, args ...any) error {
	return &Error{msg: fmt.Sprintf(format, args...)}
}

// Func is a template global function: positional and keyword
// arguments in, one value out.
type Func func(args []any, kwargs map[string]any) (any, error)

// Env holds the global functions a template may call; the zero value
// has the builtins only.
type Env struct {
	funcs map[string]Func
}

// AddFunction registers a global function (env.add_function).
func (e *Env) AddFunction(name string, fn Func) {
	if e.funcs == nil {
		e.funcs = map[string]Func{}
	}
	e.funcs[name] = fn
}

// Render renders src with ctx's top-level keys as variables (a map, or
// nil for none): minijinja's render_str with the default environment.
func Render(src string, ctx any) (string, error) {
	var e Env
	return e.Render(src, ctx)
}

// RenderOr is Render that renders failure as "": the Rust callers'
// render(...).unwrap_or_default().
func RenderOr(src string, ctx any) string {
	out, err := Render(src, ctx)
	if err != nil {
		return ""
	}
	return out
}

var cache sync.Map // template source → []node

func compile(src string) ([]node, error) {
	if body, ok := cache.Load(src); ok {
		if nodes, ok := body.([]node); ok {
			return nodes, nil
		}
	}
	body, err := parse(src)
	if err != nil {
		return nil, err
	}
	cache.Store(src, body)
	return body, nil
}

// Render renders src in this environment.
func (e *Env) Render(src string, ctx any) (string, error) {
	body, err := compile(src)
	if err != nil {
		return "", err
	}
	root := map[string]any{}
	switch c := normalize(ctx).(type) {
	case nil:
	case map[string]any:
		root = c
	default:
		return "", errorf("context must be a map, not %s", kind(ctx))
	}
	ev := &evaluator{env: e, scopes: []map[string]any{root, {}}}
	var b strings.Builder
	if err := ev.run(body, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

type evaluator struct {
	env    *Env
	scopes []map[string]any
}

func (ev *evaluator) lookup(name string) any {
	for _, scope := range slices.Backward(ev.scopes) {
		if v, ok := scope[name]; ok {
			return v
		}
	}
	if fn, ok := ev.env.funcs[name]; ok {
		return fn
	}
	if fn, ok := builtinFuncs[name]; ok {
		return fn
	}
	return Undefined
}

func (ev *evaluator) set(name string, v any) { ev.scopes[len(ev.scopes)-1][name] = v }

func (ev *evaluator) run(body []node, b *strings.Builder) error {
	for _, n := range body {
		if err := ev.exec(n, b); err != nil {
			return err
		}
	}
	return nil
}

func (ev *evaluator) exec(n node, b *strings.Builder) error {
	switch n := n.(type) {
	case textNode:
		b.WriteString(n.text)
	case emitNode:
		v, err := ev.eval(n.x)
		if err != nil {
			return err
		}
		b.WriteString(display(v))
	case ifNode:
		for i, cond := range n.conds {
			v, err := ev.eval(cond)
			if err != nil {
				return err
			}
			if truthy(v) {
				return ev.run(n.bodies[i], b)
			}
		}
		return ev.run(n.els, b)
	case forNode:
		return ev.execFor(n, b)
	case setNode:
		v, err := ev.eval(n.x)
		if err != nil {
			return err
		}
		return ev.assign(n.targets, v)
	case setBlockNode:
		var inner strings.Builder
		if err := ev.run(n.body, &inner); err != nil {
			return err
		}
		ev.set(n.target, inner.String())
	}
	return nil
}

// assign binds one value, or unpacks a sequence over several names.
func (ev *evaluator) assign(targets []string, v any) error {
	if len(targets) == 1 {
		ev.set(targets[0], v)
		return nil
	}
	items, err := iterate(v)
	if err != nil {
		return err
	}
	if len(items) != len(targets) {
		return errorf("cannot unpack %d values into %d names", len(items), len(targets))
	}
	for i, t := range targets {
		ev.set(t, items[i])
	}
	return nil
}

func (ev *evaluator) execFor(n forNode, b *strings.Builder) error {
	iterV, err := ev.eval(n.iter)
	if err != nil {
		return err
	}
	all, err := iterate(iterV)
	if err != nil {
		return err
	}
	ev.scopes = append(ev.scopes, map[string]any{})
	defer func() { ev.scopes = ev.scopes[:len(ev.scopes)-1] }()
	items := all
	if n.filter != nil {
		items = nil
		for _, item := range all {
			if err := ev.assign(n.targets, item); err != nil {
				return err
			}
			keep, err := ev.eval(n.filter)
			if err != nil {
				return err
			}
			if truthy(keep) {
				items = append(items, item)
			}
		}
	}
	if len(items) == 0 {
		return ev.run(n.els, b)
	}
	for i, item := range items {
		if err := ev.assign(n.targets, item); err != nil {
			return err
		}
		ev.set("loop", loopValue(items, i))
		if err := ev.run(n.body, b); err != nil {
			return err
		}
	}
	return nil
}

// loopValue is the loop variable of iteration i.
func loopValue(items []any, i int) map[string]any {
	n := len(items)
	loop := map[string]any{
		"index": int64(i + 1), "index0": int64(i),
		"revindex": int64(n - i), "revindex0": int64(n - i - 1),
		"first": i == 0, "last": i == n-1, "length": int64(n), "depth": int64(1), "depth0": int64(0),
	}
	// previtem/nextitem are the adjacent_loop_items feature, off in the
	// Rust build.
	loop["cycle"] = Func(func(args []any, _ map[string]any) (any, error) {
		if len(args) == 0 {
			return Undefined, nil
		}
		return args[i%len(args)], nil
	})
	return loop
}

func (ev *evaluator) eval(x expr) (any, error) {
	switch x := x.(type) {
	case litExpr:
		return x.v, nil
	case nameExpr:
		return ev.lookup(x.name), nil
	case listExpr:
		out := make([]any, len(x.items))
		for i, item := range x.items {
			v, err := ev.eval(item)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case mapExpr:
		out := make(map[string]any, len(x.keys))
		for i := range x.keys {
			k, err := ev.eval(x.keys[i])
			if err != nil {
				return nil, err
			}
			v, err := ev.eval(x.vals[i])
			if err != nil {
				return nil, err
			}
			out[display(k)] = v
		}
		return out, nil
	case attrExpr:
		base, err := ev.eval(x.x)
		if err != nil {
			return nil, err
		}
		return getAttr(base, x.name)
	case itemExpr:
		base, err := ev.eval(x.x)
		if err != nil {
			return nil, err
		}
		key, err := ev.eval(x.key)
		if err != nil {
			return nil, err
		}
		return getItem(base, key)
	case sliceExpr:
		return ev.evalSlice(x)
	case callExpr:
		fn, err := ev.eval(x.fn)
		if err != nil {
			return nil, err
		}
		args, kwargs, err := ev.evalArgs(x.args)
		if err != nil {
			return nil, err
		}
		f, ok := fn.(Func)
		if !ok {
			return nil, errorf("%s is not callable", kind(fn))
		}
		return f(args, kwargs)
	case filterExpr:
		v, err := ev.eval(x.x)
		if err != nil {
			return nil, err
		}
		args, kwargs, err := ev.evalArgs(x.args)
		if err != nil {
			return nil, err
		}
		return ev.applyFilter(x.name, v, args, kwargs)
	case testExpr:
		v, err := ev.eval(x.x)
		if err != nil {
			return nil, err
		}
		args, _, err := ev.evalArgs(x.args)
		if err != nil {
			return nil, err
		}
		ok, err := ev.applyTest(x.name, v, args)
		if err != nil {
			return nil, err
		}
		return ok != x.negated, nil
	case unaryExpr:
		v, err := ev.eval(x.x)
		if err != nil {
			return nil, err
		}
		if x.op == "not" {
			return !truthy(v), nil
		}
		return neg(v)
	case ifExpr:
		cond, err := ev.eval(x.cond)
		if err != nil {
			return nil, err
		}
		if truthy(cond) {
			return ev.eval(x.then)
		}
		if x.els == nil {
			return Undefined, nil
		}
		return ev.eval(x.els)
	case binExpr:
		return ev.evalBinary(x)
	}
	return nil, errorf("unknown expression")
}

func (ev *evaluator) evalArgs(in []arg) ([]any, map[string]any, error) {
	var args []any
	var kwargs map[string]any
	for _, a := range in {
		v, err := ev.eval(a.value)
		if err != nil {
			return nil, nil, err
		}
		if a.name != "" {
			if kwargs == nil {
				kwargs = map[string]any{}
			}
			kwargs[a.name] = v
			continue
		}
		args = append(args, v)
	}
	return args, kwargs, nil
}

// getAttr is GetAttr: a map's key, else undefined; on undefined it
// fails (lenient undefined does not chain).
func getAttr(base any, name string) (any, error) {
	switch b := normalize(base).(type) {
	case undefined:
		return nil, errorf("undefined value has no attribute %q", name)
	case map[string]any:
		if v, ok := b[name]; ok {
			return v, nil
		}
	}
	return Undefined, nil
}

// getItem is GetItem: sequence index (negative from the end), map key,
// or a string's character; a miss is undefined.
func getItem(base, key any) (any, error) {
	switch b := normalize(base).(type) {
	case undefined:
		return nil, errorf("undefined value has no item %s", display(key))
	case []any:
		if i, ok := asInt(key); ok {
			if i < 0 {
				i += int64(len(b))
			}
			if i >= 0 && i < int64(len(b)) {
				return b[i], nil
			}
		}
	case map[string]any:
		if v, ok := b[display(key)]; ok {
			return v, nil
		}
	case string:
		if i, ok := asInt(key); ok {
			r := []rune(b)
			if i < 0 {
				i += int64(len(r))
			}
			if i >= 0 && i < int64(len(r)) {
				return string(r[i]), nil
			}
		}
	}
	return Undefined, nil
}

func (ev *evaluator) evalSlice(x sliceExpr) (any, error) {
	base, err := ev.eval(x.x)
	if err != nil {
		return nil, err
	}
	bound := func(e expr) (*int64, error) {
		if e == nil {
			return nil, nil
		}
		v, err := ev.eval(e)
		if err != nil {
			return nil, err
		}
		if v == nil || isUndefined(v) {
			return nil, nil
		}
		i, ok := asInt(v)
		if !ok {
			return nil, errorf("slice bounds must be integers")
		}
		return &i, nil
	}
	start, err := bound(x.start)
	if err != nil {
		return nil, err
	}
	stop, err := bound(x.stop)
	if err != nil {
		return nil, err
	}
	step, err := bound(x.step)
	if err != nil {
		return nil, err
	}
	return sliceValue(base, start, stop, step)
}

// sliceValue is Python slicing over sequences and strings.
func sliceValue(base any, start, stop, step *int64) (any, error) {
	st := int64(1)
	if step != nil {
		st = *step
	}
	if st == 0 {
		return nil, errorf("slice step cannot be zero")
	}
	var items []any
	isStr := false
	switch b := normalize(base).(type) {
	case []any:
		items = b
	case string:
		isStr = true
		for _, r := range b {
			items = append(items, string(r))
		}
	case undefined:
		return []any{}, nil
	default:
		return nil, errorf("cannot slice %s", kind(base))
	}
	n := int64(len(items))
	norm := func(p *int64, def int64) int64 {
		if p == nil {
			return def
		}
		v := *p
		if v < 0 {
			v += n
		}
		if st > 0 {
			return max(0, min(v, n))
		}
		return max(-1, min(v, n-1))
	}
	var out []any
	if st > 0 {
		for i := norm(start, 0); i < norm(stop, n); i += st {
			out = append(out, items[i])
		}
	} else {
		for i := norm(start, n-1); i > norm(stop, -1); i += st {
			out = append(out, items[i])
		}
	}
	if isStr {
		var b strings.Builder
		for _, s := range out {
			b.WriteString(asString(s))
		}
		return b.String(), nil
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

func (ev *evaluator) evalBinary(x binExpr) (any, error) {
	l, err := ev.eval(x.l)
	if err != nil {
		return nil, err
	}
	switch x.op {
	case "and":
		if !truthy(l) {
			return l, nil
		}
		return ev.eval(x.r)
	case "or":
		if truthy(l) {
			return l, nil
		}
		return ev.eval(x.r)
	}
	r, err := ev.eval(x.r)
	if err != nil {
		return nil, err
	}
	switch x.op {
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	case "<", "<=", ">", ">=":
		c := compare(l, r)
		switch x.op {
		case "<":
			return c < 0, nil
		case "<=":
			return c <= 0, nil
		case ">":
			return c > 0, nil
		}
		return c >= 0, nil
	case "in":
		return contains(r, l)
	case "~":
		return display(l) + display(r), nil
	}
	return arith(x.op, l, r)
}

// contains is the "in" operator.
func contains(container, item any) (bool, error) {
	switch c := normalize(container).(type) {
	case undefined:
		return false, nil
	case string:
		return strings.Contains(c, display(item)), nil
	case []any:
		for _, v := range c {
			if equal(v, item) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		_, ok := c[display(item)]
		return ok, nil
	}
	return false, errorf("cannot perform a containment check on %s", kind(container))
}

func neg(v any) (any, error) {
	switch x := normalize(v).(type) {
	case int64:
		if x == math.MinInt64 {
			return nil, errorf("overflow")
		}
		return -x, nil
	case float64:
		return -x, nil
	}
	return nil, errorf("cannot negate %s", kind(v))
}

// arith is the math operators with minijinja's coercion: two integers
// stay integers (checked), a float makes it float, + joins strings and
// sequences, * repeats them.
func arith(op string, l, r any) (any, error) {
	l, r = normalize(l), normalize(r)
	switch op {
	case "+":
		if a, ok := l.([]any); ok {
			if b, ok := r.([]any); ok {
				return append(append([]any{}, a...), b...), nil
			}
		}
		if a, ok := l.(string); ok {
			if b, ok := r.(string); ok {
				return a + b, nil
			}
		}
	case "*":
		if v, ok, err := repeatOperands(l, r); ok {
			return v, err
		}
	case "/":
		a, b, ok := numbers(l, r)
		if !ok {
			return nil, opError(op, l, r)
		}
		return a / b, nil
	}
	ai, aInt := intOperand(l)
	bi, bInt := intOperand(r)
	if aInt && bInt {
		return intArith(op, ai, bi, l, r)
	}
	a, b, ok := numbers(l, r)
	if !ok {
		return nil, opError(op, l, r)
	}
	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "//":
		return math.Floor(a / b), nil
	case "%":
		// Floats take the truncated remainder (f64 %), integers the
		// euclidean one.
		return math.Mod(a, b), nil
	case "**":
		return math.Pow(a, b), nil
	}
	return nil, opError(op, l, r)
}

func intOperand(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case bool:
		return asInt(x)
	}
	return 0, false
}

func intArith(op string, a, b int64, l, r any) (any, error) {
	fail := func() (any, error) { return nil, errorf("unable to calculate %s %s %s", display(l), op, display(r)) }
	switch op {
	case "+":
		s := a + b
		if (s > a) != (b > 0) {
			return fail()
		}
		return s, nil
	case "-":
		s := a - b
		if (s < a) != (b > 0) {
			return fail()
		}
		return s, nil
	case "*":
		if a != 0 && b != 0 {
			p := a * b
			if p/b != a || (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
				return fail()
			}
			return p, nil
		}
		return int64(0), nil
	case "//":
		if b == 0 {
			return fail()
		}
		return euclidDiv(a, b), nil
	case "%":
		if b == 0 {
			return fail()
		}
		m := a % b
		if m < 0 {
			if b > 0 {
				m += b
			} else {
				m -= b
			}
		}
		return m, nil
	case "**":
		if b < 0 {
			return fail()
		}
		result := int64(1)
		for range b {
			next := result * a
			if a != 0 && next/a != result {
				return fail()
			}
			result = next
		}
		return result, nil
	}
	return fail()
}

// euclidDiv is i128::div_euclid: the quotient whose remainder is never
// negative.
func euclidDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		if b > 0 {
			q--
		} else {
			q++
		}
	}
	return q
}

// repeatOperands is string or sequence repetition; ok is false when
// neither side is one.
func repeatOperands(l, r any) (v any, ok bool, err error) {
	s, n := l, r
	if _, ok := s.(string); !ok {
		if _, ok := s.([]any); !ok {
			s, n = r, l
		}
	}
	count, isInt := n.(int64)
	switch seq := s.(type) {
	case string:
		if !isInt || count < 0 {
			return nil, true, errorf("strings can only be multiplied with integers")
		}
		return strings.Repeat(seq, int(count)), true, nil
	case []any:
		if !isInt || count < 0 {
			return nil, true, errorf("sequences can only be multiplied with integers")
		}
		out := []any{}
		for range count {
			out = append(out, seq...)
		}
		return out, true, nil
	}
	return nil, false, nil
}

func opError(op string, l, r any) error {
	return errorf("tried to use %s operator on unsupported types %s and %s", op, kind(l), kind(r))
}
