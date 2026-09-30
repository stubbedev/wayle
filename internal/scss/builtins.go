package scss

import (
	"math"
	"slices"
	"strings"
)

// sassFunctions are Sass's global built-in functions outside the
// subset. A call to one is rejected rather than passed through, since
// Sass would evaluate it.
var sassFunctions = map[string]bool{
	// color
	"adjust-color": true, "adjust-hue": true, "blue": true, "change-color": true,
	"complement": true, "darken": true, "desaturate": true, "fade-in": true,
	"fade-out": true, "green": true, "hue": true, "ie-hex-str": true,
	"lighten": true, "lightness": true, "mix": true, "opacify": true, "red": true,
	"saturation": true, "scale-color": true, "transparentize": true,
	// list
	"append": true, "index": true, "is-bracketed": true, "join": true, "length": true,
	"list-separator": true, "nth": true, "set-nth": true, "zip": true,
	// map
	"map-get": true, "map-has-key": true, "map-keys": true, "map-merge": true,
	"map-remove": true, "map-values": true,
	// math
	"abs": true, "ceil": true, "comparable": true, "floor": true, "percentage": true,
	"random": true, "round": true, "unit": true, "unitless": true,
	// string
	"quote": true, "str-index": true, "str-insert": true, "str-length": true,
	"str-slice": true, "to-lower-case": true, "to-upper-case": true,
	"unique-id": true, "unquote": true,
	// meta
	"call": true, "content-exists": true, "feature-exists": true,
	"function-exists": true, "get-function": true, "global-variable-exists": true,
	"inspect": true, "keywords": true, "mixin-exists": true, "type-of": true,
	"variable-exists": true, "if": true,
	// selector
	"is-superselector": true, "selector-append": true, "selector-extend": true,
	"selector-nest": true, "selector-parse": true, "selector-replace": true,
	"selector-unify": true, "simple-selectors": true,
}

// filterFunctions are Sass color functions that are also CSS filter
// functions: with one number argument Sass emits the CSS function.
var filterFunctions = map[string]bool{
	"invert": true, "grayscale": true, "saturate": true, "opacity": true, "alpha": true,
}

func (c *compiler) call(x *callExpr, e env) value {
	if x.ns != "" {
		x.unsupported("module functions (" + x.ns + "." + x.name + "())")
	}
	switch x.name {
	case "calc", "min", "max", "clamp":
		return c.calculation(x, e)
	case "rgb", "rgba":
		return c.rgb(x, e)
	case "hsl", "hsla":
		return c.hsl(x, e)
	}
	if filterFunctions[x.name] {
		args := c.positional(x, e)
		if len(args) == 1 {
			if _, ok := args[0].(number); ok || isSpecial(args[0]) {
				return plainCall(x.name, args)
			}
		}
		x.unsupported("the Sass function " + x.name + "()")
	}
	if sassFunctions[normName(x.name)] {
		x.unsupported("the Sass function " + x.name + "()")
	}
	for _, a := range x.args {
		if a.name != "" {
			x.fail("plain CSS functions don't support keyword arguments")
		}
	}
	return plainCall(x.name, c.positional(x, e))
}

// positional evaluates a built-in call's arguments, which must all be
// positional.
func (c *compiler) positional(x *callExpr, e env) []value {
	var out []value
	for _, a := range x.args {
		if a.name != "" {
			x.unsupported("keyword arguments to " + x.name + "()")
		}
		out = append(out, c.eval(a.x, e))
	}
	return out
}

// plainCall renders a CSS function call with evaluated arguments.
func plainCall(name string, args []value) value {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = css(a)
	}
	return sstring{text: name + "(" + strings.Join(parts, ", ") + ")"}
}

// isSpecial reports a value Sass passes through a color function
// untouched: a var(), env(), attr(), or unsimplified calculation.
func isSpecial(v value) bool {
	s, ok := v.(sstring)
	if !ok || s.quoted {
		return false
	}
	lower := strings.ToLower(s.text)
	for _, prefix := range []string{"var(", "env(", "attr(", "calc(", "min(", "max(", "clamp("} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// calculation evaluates calc(), min(), max(), or clamp() like Sass:
// down to a number when the operands' units allow, otherwise to the
// simplified CSS function.
func (c *compiler) calculation(x *callExpr, e env) value {
	var args []value
	for _, a := range x.args {
		if a.name != "" {
			x.fail("%s() doesn't take keyword arguments", x.name)
		}
		args = append(args, c.calcArg(a.x, e))
	}
	switch want := map[string]int{"calc": 1, "clamp": 3}[x.name]; {
	case want != 0 && len(args) != want:
		x.fail("%s() takes exactly %d argument(s)", x.name, want)
	case len(args) == 0:
		x.fail("%s() needs at least one argument", x.name)
	}
	if x.name == "calc" {
		if n, ok := args[0].(number); ok {
			return n
		}
		return sstring{text: "calc(" + calcText(args[0]) + ")"}
	}
	if nums, ok := compatibleNumbers(args); ok {
		switch x.name {
		case "min":
			return pick(nums, -1)
		case "max":
			return pick(nums, 1)
		}
		return pick([]number{pick([]number{nums[1], nums[2]}, -1), nums[0]}, 1)
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = calcText(a)
	}
	return sstring{text: x.name + "(" + strings.Join(parts, ", ") + ")"}
}

// calcArg evaluates a calculation operand: + - * / fold numbers whose
// units combine and stay symbolic otherwise.
func (c *compiler) calcArg(x expr, e env) value {
	switch x := x.(type) {
	case *binaryExpr:
		switch x.op {
		case "+", "-", "*", "/":
			l, r := c.calcArg(x.l, e), c.calcArg(x.r, e)
			if ln, ok := l.(number); ok {
				if rn, ok := r.(number); ok {
					if n, _, ok := arith(x.op, ln, rn, true); ok {
						return n
					}
				}
			}
			return calcValue{op: x.op, l: l, r: r}
		}
	case *parenExpr:
		return c.calcArg(x.x, e)
	case *callExpr:
		// A nested calc() is just grouping.
		if x.ns == "" && x.name == "calc" && len(x.args) == 1 && x.args[0].name == "" {
			return c.calcArg(x.args[0].x, e)
		}
	}
	return stripSlash(c.eval(x, e))
}

// compatibleNumbers reports whether every value is a number in units
// comparable with the first's.
func compatibleNumbers(args []value) ([]number, bool) {
	nums := make([]number, 0, len(args))
	for _, a := range args {
		n, ok := a.(number)
		if !ok {
			return nil, false
		}
		if len(nums) > 0 {
			if _, _, _, ok := coerce(nums[0], n, true); !ok {
				return nil, false
			}
		}
		nums = append(nums, n)
	}
	return nums, true
}

// pick returns the smallest (dir -1) or largest (dir 1) number.
func pick(nums []number, dir int) number {
	best := nums[0]
	for _, n := range nums[1:] {
		if cmp, _ := compare(n, best); cmp == dir {
			best = n
		}
	}
	return best
}

// rgb evaluates rgb()/rgba(): channels, the (color, alpha) form, or
// the space-separated `r g b / a` form.
func (c *compiler) rgb(x *callExpr, e env) value {
	args := c.positional(x, e)
	if slices.ContainsFunc(args, isSpecial) {
		return plainCall(x.name, args)
	}
	switch len(args) {
	case 1:
		chans, ok := spaceChannels(args[0])
		if !ok {
			x.fail("%s(): expected red, green, and blue channels", x.name)
		}
		if slices.ContainsFunc(chans, isSpecial) {
			return plainCall(x.name, args)
		}
		return c.rgbChannels(x, chans)
	case 2:
		col, ok := args[0].(color)
		if !ok {
			x.fail("%s(): $color: %s is not a color", x.name, css(args[0]))
		}
		col.text = ""
		col.a = alpha(x, args[1])
		return col
	case 3, 4:
		return c.rgbChannels(x, args)
	}
	x.fail("%s() takes 1 to 4 arguments, but %d were passed", x.name, len(args))
	return nil
}

// spaceChannels splits `r g b` or `r g b / a` (the slash parsed as a
// kept separator on the last number) into channel values.
func spaceChannels(v value) ([]value, bool) {
	l, ok := v.(list)
	if !ok || l.sep != ' ' || len(l.items) != 3 {
		return nil, false
	}
	chans := append([]value(nil), l.items...)
	switch last := chans[2].(type) {
	case number:
		if last.slash != nil {
			chans[2] = last.slash[0]
			chans = append(chans, last.slash[1])
		}
	case list:
		if last.sep == '/' && len(last.items) == 2 {
			chans[2] = last.items[0]
			chans = append(chans, last.items[1])
		}
	}
	return chans, true
}

func (c *compiler) rgbChannels(x *callExpr, args []value) value {
	col := color{a: 1}
	for i, ch := range []*float64{&col.r, &col.g, &col.b} {
		n, ok := args[i].(number)
		if !ok {
			x.fail("%s(): %s is not a number", x.name, css(args[i]))
		}
		switch n.unit {
		case "":
			*ch = n.v
		case "%":
			*ch = n.v * 255 / 100
		default:
			x.fail("%s(): expected %s to have no units or \"%%\"", x.name, n.css())
		}
		*ch = math.Max(0, math.Min(255, *ch))
	}
	if len(args) == 4 {
		col.a = alpha(x, args[3])
	}
	return col
}

// alpha reads an alpha channel: unitless 0-1 or a percentage.
func alpha(x *callExpr, v value) float64 {
	n, ok := v.(number)
	if !ok {
		x.fail("%s(): $alpha: %s is not a number", x.name, css(v))
	}
	a := n.v
	switch n.unit {
	case "":
	case "%":
		a /= 100
	default:
		x.fail("%s(): expected %s to have no units or \"%%\"", x.name, n.css())
	}
	return math.Max(0, math.Min(1, a))
}

// hsl evaluates hsl()/hsla() to an RGB color.
func (c *compiler) hsl(x *callExpr, e env) value {
	args := c.positional(x, e)
	if len(args) == 1 {
		chans, ok := spaceChannels(args[0])
		if !ok {
			x.fail("%s(): expected hue, saturation, and lightness", x.name)
		}
		args = chans
	}
	if slices.ContainsFunc(args, isSpecial) {
		return plainCall(x.name, args)
	}
	if len(args) != 3 && len(args) != 4 {
		x.fail("%s() takes 3 or 4 arguments, but %d were passed", x.name, len(args))
	}
	var hsl [3]float64
	for i := range hsl {
		n, ok := args[i].(number)
		if !ok {
			x.fail("%s(): %s is not a number", x.name, css(args[i]))
		}
		hsl[i] = n.v
		if i > 0 && n.unit != "" && n.unit != "%" {
			x.fail("%s(): expected %s to have no units or \"%%\"", x.name, n.css())
		}
		if i == 0 && n.unit != "" {
			deg, ok := convert(n, "deg")
			if !ok {
				x.fail("%s(): $hue: expected %s to be an angle", x.name, n.css())
			}
			hsl[i] = deg
		}
	}
	col := color{a: 1}
	col.r, col.g, col.b = hslToRGB(hsl[0], hsl[1], hsl[2])
	if len(args) == 4 {
		col.a = alpha(x, args[3])
	}
	return col
}
