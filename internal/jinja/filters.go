package jinja

import (
	"html"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// filterFunc is one builtin filter: the value, then the arguments.
type filterFunc func(ev *evaluator, v any, args []any, kwargs map[string]any) (any, error)

// filterSpec is one builtin filter with its signature: at most max
// positional arguments (-1 for any), and the keyword arguments it
// takes. Anything beyond is an error, as minijinja's argument
// conversion rejects it.
type filterSpec struct {
	fn     filterFunc
	max    int
	kwargs []string
}

var filters map[string]filterSpec

func init() {
	none := func(fn filterFunc) filterSpec { return filterSpec{fn: fn} }
	pos := func(n int, fn filterFunc) filterSpec { return filterSpec{fn: fn, max: n} }
	filters = map[string]filterSpec{
		"safe":       none(func(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) { return v, nil }),
		"escape":     none(strFilter(html.EscapeString)),
		"e":          none(strFilter(html.EscapeString)),
		"lower":      none(strFilter(lower)),
		"upper":      none(strFilter(upper)),
		"title":      none(strFilter(title)),
		"capitalize": none(strFilter(capitalize)),
		"replace":    pos(2, replaceFilter),
		"length":     none(lengthFilter),
		"count":      none(lengthFilter),
		"dictsort":   {fn: dictsortFilter, kwargs: []string{"by", "case_sensitive", "reverse"}},
		"items":      none(itemsFilter),
		"reverse":    none(reverseFilter),
		"trim":       pos(1, trimFilter),
		"join":       pos(1, joinFilter),
		"split":      pos(2, splitFilter),
		"lines":      none(linesFilter),
		"default":    pos(2, defaultFilter),
		"d":          pos(2, defaultFilter),
		"round":      pos(1, roundFilter),
		"abs":        none(absFilter),
		"int":        none(intFilter),
		"float":      none(floatFilter),
		"attr":       pos(1, attrFilter),
		"first":      none(firstFilter),
		"last":       none(lastFilter),
		"min":        none(minMaxFilter(-1)),
		"max":        none(minMaxFilter(1)),
		"sort":       {fn: sortFilter, kwargs: []string{"case_sensitive", "reverse", "attribute"}},
		"list":       none(listFilter),
		"string":     none(stringFilter),
		"bool":       none(func(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) { return truthy(v), nil }),
		"batch":      pos(2, batchFilter),
		"slice":      pos(2, sliceFilter),
		"sum":        none(sumFilter),
		"indent":     pos(3, indentFilter),
		"select":     pos(-1, selectFilter(true, false)),
		"reject":     pos(-1, selectFilter(false, false)),
		"selectattr": pos(-1, selectFilter(true, true)),
		"rejectattr": pos(-1, selectFilter(false, true)),
		"map":        {fn: mapFilter, max: -1, kwargs: []string{"attribute", "default"}},
		"unique":     {fn: uniqueFilter, kwargs: []string{"case_sensitive", "attribute"}},
		"format":     pos(-1, formatFilter),
	}
}

func (ev *evaluator) applyFilter(name string, v any, args []any, kwargs map[string]any) (any, error) {
	f, ok := filters[name]
	if !ok {
		return nil, errorf("unknown filter %s", name)
	}
	if f.max >= 0 && len(args) > f.max {
		return nil, errorf("too many arguments to filter %s", name)
	}
	for k := range kwargs {
		if !slices.Contains(f.kwargs, k) {
			return nil, errorf("filter %s got an unexpected keyword argument %q", name, k)
		}
	}
	return f.fn(ev, v, args, kwargs)
}

func argAt(args []any, kwargs map[string]any, i int, name string) (any, bool) {
	if v, ok := kwargs[name]; ok {
		return v, true
	}
	if i < len(args) {
		return args[i], true
	}
	return nil, false
}

// asString is the Cow<str> argument conversion: strings as they are,
// anything else by its display.
func asString(v any) string {
	if s, ok := normalize(v).(string); ok {
		return s
	}
	return display(v)
}

func strFilter(fn func(string) string) filterFunc {
	return func(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
		return fn(asString(v)), nil
	}
}

// title capitalizes after whitespace and ASCII punctuation.
func title(s string) string {
	var b strings.Builder
	capital := true
	for _, r := range s {
		switch {
		case r < unicode.MaxASCII && unicode.IsPunct(r) || r < unicode.MaxASCII && unicode.IsSymbol(r) || unicode.IsSpace(r):
			b.WriteRune(r)
			capital = true
		case capital:
			b.WriteString(upper(string(r)))
			capital = false
		default:
			b.WriteString(lower(string(r)))
		}
	}
	return b.String()
}

func capitalize(s string) string {
	for i, r := range s {
		return upper(string(r)) + lower(s[i+len(string(r)):])
	}
	return ""
}

func replaceFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	from, ok1 := argAt(args, kwargs, 0, "old")
	to, ok2 := argAt(args, kwargs, 1, "new")
	if !ok1 || !ok2 {
		return nil, errorf("replace takes two arguments")
	}
	return strings.ReplaceAll(asString(v), asString(from), asString(to)), nil
}

func lengthFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	n, ok := length(v)
	if !ok {
		return nil, errorf("cannot calculate length of value of type %s", kind(v))
	}
	return int64(n), nil
}

func dictsortFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	m, ok := normalize(v).(map[string]any)
	if !ok {
		return nil, errorf("dictsort filter argument must be a mapping")
	}
	byValue := false
	if by, ok := argAt(args, kwargs, 1, "by"); ok && asString(by) == "value" {
		byValue = true
	}
	caseSensitive := false
	if cs, ok := argAt(args, kwargs, 0, "case_sensitive"); ok {
		caseSensitive = truthy(cs)
	}
	reverse := false
	if r, ok := kwargs["reverse"]; ok {
		reverse = truthy(r)
	}
	type pair struct {
		key string
		val any
	}
	pairs := make([]pair, 0, len(m))
	for _, k := range sortedKeys(m) {
		pairs = append(pairs, pair{k, m[k]})
	}
	key := func(p pair) any {
		if byValue {
			return p.val
		}
		return p.key
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		c := orderValues(key(pairs[i]), key(pairs[j]), caseSensitive)
		if reverse {
			return c > 0
		}
		return c < 0
	})
	out := make([]any, len(pairs))
	for i, p := range pairs {
		out[i] = []any{p.key, p.val}
	}
	return out, nil
}

func itemsFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	m, ok := normalize(v).(map[string]any)
	if !ok {
		return nil, errorf("items filter argument must be a mapping")
	}
	out := make([]any, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, []any{k, m[k]})
	}
	return out, nil
}

func reverseFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	if s, ok := normalize(v).(string); ok {
		r := []rune(s)
		slices.Reverse(r)
		return string(r), nil
	}
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(items)
	slices.Reverse(out)
	return out, nil
}

func trimFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	if chars, ok := argAt(args, kwargs, 0, "chars"); ok && chars != nil {
		return strings.Trim(asString(v), asString(chars)), nil
	}
	return strings.TrimSpace(asString(v)), nil
}

func joinFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	if v == nil || isUndefined(v) {
		return "", nil
	}
	sep := ""
	if s, ok := argAt(args, kwargs, 0, "d"); ok {
		sep = asString(s)
	}
	items, err := iterate(v)
	if err != nil {
		return nil, errorf("cannot join value of type %s", kind(v))
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = asString(item)
	}
	return strings.Join(parts, sep), nil
}

func splitFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	s := asString(v)
	limit := -1
	if n, ok := argAt(args, kwargs, 1, "maxsplits"); ok && n != nil {
		if i, ok := asInt(n); ok {
			limit = int(i) + 1
		}
	}
	var parts []string
	if sep, ok := argAt(args, kwargs, 0, "split"); ok && sep != nil {
		parts = strings.SplitN(s, asString(sep), limit)
	} else {
		parts = strings.Fields(s)
		if limit > 0 && len(parts) > limit {
			parts = append(parts[:limit-1], strings.Join(parts[limit-1:], " "))
		}
	}
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out, nil
}

func linesFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	lines := strings.Split(strings.ReplaceAll(asString(v), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]any, len(lines))
	for i, l := range lines {
		out[i] = l
	}
	return out, nil
}

// defaultFilter is default(value="", boolean=false): only undefined
// takes the default unless boolean asks for every falsy value.
func defaultFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	other, ok := argAt(args, kwargs, 0, "default_value")
	if !ok {
		other = ""
	}
	lax := false
	if b, ok := argAt(args, kwargs, 1, "boolean"); ok {
		lax = truthy(b)
	}
	if isUndefined(v) || lax && !truthy(v) {
		return other, nil
	}
	return v, nil
}

func roundFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	switch x := normalize(v).(type) {
	case int64:
		return x, nil
	case float64:
		precision := int64(0)
		if p, ok := argAt(args, kwargs, 0, "precision"); ok {
			precision, _ = asInt(p)
		}
		scale := math.Pow(10, float64(precision))
		return math.Round(x*scale) / scale, nil
	}
	return nil, errorf("cannot round value (%s)", kind(v))
}

func absFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	switch x := normalize(v).(type) {
	case int64:
		if x == math.MinInt64 {
			return nil, errorf("overflow on abs")
		}
		if x < 0 {
			return -x, nil
		}
		return x, nil
	case float64:
		return math.Abs(x), nil
	}
	return nil, errorf("cannot get absolute value")
}

func intFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	switch x := normalize(v).(type) {
	case nil, undefined:
		return int64(0), nil
	case bool:
		i, _ := asInt(x)
		return i, nil
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	case string:
		s := strings.TrimSpace(x)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, errorf("invalid digit found in string")
		}
		return int64(f), nil
	}
	return nil, errorf("cannot convert %s to integer", kind(v))
}

func floatFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	switch x := normalize(v).(type) {
	case nil, undefined:
		return 0.0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil, errorf("invalid float literal")
		}
		return f, nil
	}
	if f, ok := asFloat(v); ok {
		return f, nil
	}
	return nil, errorf("cannot convert %s to float", kind(v))
}

func attrFilter(_ *evaluator, v any, args []any, _ map[string]any) (any, error) {
	if len(args) == 0 {
		return nil, errorf("attr takes one argument")
	}
	return getAttr(v, asString(args[0]))
}

func firstFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	if s, ok := normalize(v).(string); ok {
		for _, r := range s {
			return string(r), nil
		}
		return Undefined, nil
	}
	items, err := iterate(v)
	if err != nil {
		return nil, errorf("cannot get first item from value")
	}
	if len(items) == 0 {
		return Undefined, nil
	}
	return items[0], nil
}

func lastFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	if s, ok := normalize(v).(string); ok {
		r := []rune(s)
		if len(r) == 0 {
			return Undefined, nil
		}
		return string(r[len(r)-1]), nil
	}
	items, ok := normalize(v).([]any)
	if !ok {
		return nil, errorf("cannot get last item from value")
	}
	if len(items) == 0 {
		return Undefined, nil
	}
	return items[len(items)-1], nil
}

func minMaxFilter(sign int) filterFunc {
	return func(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
		items, err := iterate(v)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return Undefined, nil
		}
		best := items[0]
		for _, item := range items[1:] {
			if orderValues(item, best, true)*sign > 0 {
				best = item
			}
		}
		return best, nil
	}
}

// orderValues is the sort order: numbers, then strings (optionally
// case-folded), other kinds by display.
func orderValues(a, b any, caseSensitive bool) int {
	if !caseSensitive {
		if x, ok := normalize(a).(string); ok {
			if y, ok := normalize(b).(string); ok {
				// ASCII folding: the Rust build has minijinja's unicode
				// feature off.
				return strings.Compare(asciiLower(x), asciiLower(y))
			}
		}
	}
	return compare(a, b)
}

func sortFilter(ev *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(items)
	caseSensitive := false
	if cs, ok := kwargs["case_sensitive"]; ok {
		caseSensitive = truthy(cs)
	}
	reverse := truthy(kwargs["reverse"])
	var keys []string
	if attr, ok := kwargs["attribute"]; ok {
		for k := range strings.SplitSeq(asString(attr), ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
	}
	key := func(x any) any {
		switch len(keys) {
		case 0:
			return x
		case 1:
			return path(x, keys[0])
		}
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = path(x, k)
		}
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		c := orderValues(key(out[i]), key(out[j]), caseSensitive)
		if reverse {
			return c > 0
		}
		return c < 0
	})
	return out, nil
}

func listFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []any{}
	}
	return slices.Clone(items), nil
}

func stringFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	return asString(v), nil
}

func batchFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	n, ok := argAt(args, kwargs, 0, "count")
	size, isInt := asInt(n)
	if !ok || !isInt || size <= 0 {
		return nil, errorf("count must be a positive integer")
	}
	fill, hasFill := argAt(args, kwargs, 1, "fill_with")
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	var out []any
	for i := 0; i < len(items); i += int(size) {
		chunk := slices.Clone(items[i:min(i+int(size), len(items))])
		for hasFill && len(chunk) < int(size) {
			chunk = append(chunk, fill)
		}
		out = append(out, chunk)
	}
	return out, nil
}

func sliceFilter(_ *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	n, _ := argAt(args, kwargs, 0, "count")
	count, ok := asInt(n)
	if !ok || count <= 0 {
		return nil, errorf("count must be a positive integer")
	}
	fill, hasFill := argAt(args, kwargs, 1, "fill_with")
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	per, extra := len(items)/int(count), len(items)%int(count)
	out := []any{}
	offset := 0
	for i := range int(count) {
		size := per
		if i < extra {
			size++
		}
		chunk := slices.Clone(items[offset : offset+size])
		offset += size
		if hasFill && i >= extra {
			chunk = append(chunk, fill)
		}
		out = append(out, chunk)
	}
	return out, nil
}

func sumFilter(_ *evaluator, v any, _ []any, _ map[string]any) (any, error) {
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	var acc any = int64(0)
	for _, item := range items {
		if acc, err = arith("+", acc, item); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// indentFilter is indent(width, first=false, blank=false): every line
// but the first (unless first) gains width spaces; blank lines stay
// empty unless blank. A trailing newline is dropped.
func indentFilter(_ *evaluator, v any, args []any, _ map[string]any) (any, error) {
	if len(args) == 0 {
		return nil, errorf("missing argument width")
	}
	width, ok := asInt(args[0])
	if !ok || width < 0 {
		return nil, errorf("width must be a non-negative integer")
	}
	first := len(args) > 1 && truthy(args[1])
	blank := len(args) > 2 && truthy(args[2])
	stripNewline := func(s string) string {
		s = strings.TrimSuffix(s, "\n")
		return strings.TrimSuffix(s, "\r")
	}
	value := stripNewline(asString(v))
	pad := strings.Repeat(" ", int(width))
	lines := strings.Split(value, "\n")
	var b strings.Builder
	if !first {
		b.WriteString(lines[0])
		b.WriteByte('\n')
		lines = lines[1:]
	}
	for _, line := range lines {
		if line == "" {
			if blank {
				b.WriteString(pad)
			}
		} else {
			b.WriteString(pad + line)
		}
		b.WriteByte('\n')
	}
	return stripNewline(b.String()), nil
}

// path is get_path: a dotted attribute (or index) path, undefined on a
// miss.
func path(v any, dotted string) any {
	for part := range strings.SplitSeq(dotted, ".") {
		if i, err := strconv.ParseInt(part, 10, 64); err == nil {
			v, _ = getItem(v, i)
			continue
		}
		var err error
		if v, err = getAttr(v, part); err != nil {
			return Undefined
		}
	}
	return v
}

func selectFilter(keep, byAttr bool) filterFunc {
	return func(ev *evaluator, v any, args []any, _ map[string]any) (any, error) {
		items, err := iterate(v)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, item := range items {
			subject, rest := item, args
			if byAttr {
				if len(args) == 0 {
					return nil, errorf("an attribute is required")
				}
				subject, err = getAttr(item, asString(args[0]))
				if err != nil {
					return nil, err
				}
				rest = args[1:]
			}
			ok := truthy(subject)
			if len(rest) > 0 {
				if ok, err = ev.applyTest(asString(rest[0]), subject, rest[1:]); err != nil {
					return nil, err
				}
			}
			if ok == keep {
				out = append(out, item)
			}
		}
		return out, nil
	}
}

func mapFilter(ev *evaluator, v any, args []any, kwargs map[string]any) (any, error) {
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(items))
	if attr, ok := kwargs["attribute"]; ok {
		def, hasDef := kwargs["default"]
		for _, item := range items {
			got := path(item, asString(attr))
			if isUndefined(got) && hasDef {
				got = def
			}
			out = append(out, got)
		}
		return out, nil
	}
	if len(args) == 0 {
		return nil, errorf("map needs a filter name or attribute")
	}
	for _, item := range items {
		got, err := ev.applyFilter(asString(args[0]), item, args[1:], nil)
		if err != nil {
			return nil, err
		}
		out = append(out, got)
	}
	return out, nil
}

// uniqueFilter keeps each item's first occurrence, comparing by an
// attribute path when one is given and case-insensitively (full
// lowercase) unless case_sensitive.
func uniqueFilter(_ *evaluator, v any, _ []any, kwargs map[string]any) (any, error) {
	items, err := iterate(v)
	if err != nil {
		return nil, err
	}
	caseSensitive := truthy(kwargs["case_sensitive"])
	attr, hasAttr := kwargs["attribute"]
	out := []any{}
	var seen []any
	for _, item := range items {
		key := item
		if hasAttr {
			key = path(item, asString(attr))
		}
		if s, ok := normalize(key).(string); ok && !caseSensitive {
			key = lower(s)
		}
		if slices.ContainsFunc(seen, func(s any) bool { return compare(s, key) == 0 }) {
			continue
		}
		seen = append(seen, key)
		out = append(out, item)
	}
	return out, nil
}

// asciiLower is str::to_ascii_lowercase.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func formatFilter(_ *evaluator, v any, args []any, _ map[string]any) (any, error) {
	return printf(asString(v), args)
}

// upper and lower are Rust's str::to_uppercase/to_lowercase: the full
// Unicode mappings ("ß" uppercases to "SS"), not Go's simple ones.
func upper(s string) string { return cases.Upper(language.Und).String(s) }
func lower(s string) string { return cases.Lower(language.Und).String(s) }
