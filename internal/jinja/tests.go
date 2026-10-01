package jinja

import (
	"maps"
	"strings"
	"unicode"
)

func (ev *evaluator) applyTest(name string, v any, args []any) (bool, error) {
	one := func() (any, error) {
		if len(args) == 0 {
			return nil, errorf("test %s takes one argument", name)
		}
		return args[0], nil
	}
	cmp := func(want func(int) bool) (bool, error) {
		other, err := one()
		if err != nil {
			return false, err
		}
		return want(compare(v, other)), nil
	}
	nv := normalize(v)
	switch name {
	case "defined":
		return !isUndefined(v), nil
	case "undefined":
		return isUndefined(v), nil
	case "none":
		return nv == nil, nil
	case "boolean":
		_, ok := nv.(bool)
		return ok, nil
	case "true":
		b, ok := nv.(bool)
		return ok && b, nil
	case "false":
		b, ok := nv.(bool)
		return ok && !b, nil
	case "odd", "even":
		i, ok := nv.(int64)
		if !ok {
			return false, nil
		}
		return (i%2 != 0) == (name == "odd"), nil
	case "divisibleby":
		other, err := one()
		if err != nil {
			return false, err
		}
		a, ok1 := nv.(int64)
		b, ok2 := normalize(other).(int64)
		if !ok1 || !ok2 || b == 0 {
			return false, nil
		}
		return a%b == 0, nil
	case "number":
		_, ok := asFloat(v)
		_, isBool := nv.(bool)
		return ok && !isBool, nil
	case "integer", "int":
		_, ok := nv.(int64)
		return ok, nil
	case "float":
		_, ok := nv.(float64)
		return ok, nil
	case "string":
		_, ok := nv.(string)
		return ok, nil
	case "sequence":
		_, ok := nv.([]any)
		return ok, nil
	case "mapping":
		_, ok := nv.(map[string]any)
		return ok, nil
	case "iterable":
		_, err := iterate(v)
		return err == nil && !isUndefined(v), nil
	case "startingwith", "endingwith", "containing":
		other, err := one()
		if err != nil {
			return false, err
		}
		s, needle := asString(v), asString(other)
		switch name {
		case "startingwith":
			return strings.HasPrefix(s, needle), nil
		case "endingwith":
			return strings.HasSuffix(s, needle), nil
		}
		return contains(v, other)
	case "in":
		other, err := one()
		if err != nil {
			return false, err
		}
		return contains(other, v)
	case "eq", "equalto", "==":
		other, err := one()
		if err != nil {
			return false, err
		}
		return equal(v, other), nil
	case "ne", "!=":
		other, err := one()
		if err != nil {
			return false, err
		}
		return !equal(v, other), nil
	case "lt", "lessthan", "<":
		return cmp(func(c int) bool { return c < 0 })
	case "le", "<=":
		return cmp(func(c int) bool { return c <= 0 })
	case "gt", "greaterthan", ">":
		return cmp(func(c int) bool { return c > 0 })
	case "ge", ">=":
		return cmp(func(c int) bool { return c >= 0 })
	case "lower", "upper":
		// Every character has the case (is_lowercase / is_uppercase), so
		// digits and spaces fail both.
		is := unicode.IsLower
		if name == "upper" {
			is = unicode.IsUpper
		}
		for _, r := range asString(v) {
			if !is(r) {
				return false, nil
			}
		}
		return true, nil
	case "filter":
		_, ok := filters[asString(v)]
		return ok, nil
	case "test":
		return builtinTests[asString(v)], nil
	case "safe", "escaped":
		return false, nil
	}
	return false, errorf("unknown test %s", name)
}

var builtinTests = map[string]bool{}

func init() {
	for _, n := range []string{"defined", "undefined", "none", "boolean", "true", "false", "odd", "even", "divisibleby", "number", "integer", "int", "float", "string", "sequence", "mapping", "iterable", "startingwith", "endingwith", "containing", "in", "eq", "equalto", "ne", "lt", "lessthan", "le", "gt", "greaterthan", "ge", "lower", "upper", "filter", "test", "safe", "escaped"} {
		builtinTests[n] = true
	}
}

// builtinFuncs are the global functions: range, dict.
var builtinFuncs = map[string]Func{
	"range": func(args []any, _ map[string]any) (any, error) {
		ints := make([]int64, len(args))
		for i, a := range args {
			n, ok := asInt(a)
			if !ok {
				return nil, errorf("range takes integers")
			}
			ints[i] = n
		}
		start, stop, step := int64(0), int64(0), int64(1)
		switch len(ints) {
		case 1:
			stop = ints[0]
		case 2:
			start, stop = ints[0], ints[1]
		case 3:
			start, stop, step = ints[0], ints[1], ints[2]
		default:
			return nil, errorf("range takes one to three arguments")
		}
		if step == 0 {
			return nil, errorf("range step cannot be zero")
		}
		out := []any{}
		for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
			if len(out) >= 100000 {
				return nil, errorf("range has too many elements")
			}
			out = append(out, i)
		}
		return out, nil
	},
	"dict": func(_ []any, kwargs map[string]any) (any, error) {
		out := map[string]any{}
		maps.Copy(out, kwargs)
		return out, nil
	},
}
