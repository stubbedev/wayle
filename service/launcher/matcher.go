package launcher

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/stubbedev/wayle/internal/glob"
)

// MatchMethod is rofi's -matching.
type MatchMethod uint8

// Matching methods.
const (
	// MatchNormal is tokenized substring matching (rofi's default).
	MatchNormal MatchMethod = iota
	// MatchFuzzy is fzf-style fuzzy matching.
	MatchFuzzy
	// MatchPrefix is tokenized prefix matching.
	MatchPrefix
	// MatchRegex treats the whole query as a regular expression.
	MatchRegex
	// MatchGlob is tokenized glob matching (`*token*` per token).
	MatchGlob
)

// CaseMode is rofi's case handling (-case-sensitive and -case-smart
// collapsed).
type CaseMode uint8

// Case modes.
const (
	// CaseModeInsensitive always folds case.
	CaseModeInsensitive CaseMode = iota
	// CaseModeSmart is sensitive only when the query has an uppercase
	// char.
	CaseModeSmart
	// CaseModeSensitive never folds.
	CaseModeSensitive
)

// SortMethod is rofi's -sorting-method.
type SortMethod uint8

// Sort methods.
const (
	// SortFzf ranks by match quality (the fzf-style score).
	SortFzf SortMethod = iota
	// SortLevenshtein ranks by edit distance to the query.
	SortLevenshtein
)

// MatcherOptions are the matching knobs (matcher.rs MatcherOptions).
type MatcherOptions struct {
	Method MatchMethod
	Case   CaseMode
	// Tokenize splits the query into independently matched words.
	Tokenize bool
	// Normalize strips accents while matching.
	Normalize bool
	// NegationChar prefixes a token that must not match
	// (-matching-negate-char).
	NegationChar rune
	// Sort ranks results by match quality; off keeps list order (rofi's
	// default).
	Sort       bool
	SortMethod SortMethod
}

// DefaultMatcherOptions is MatcherOptions::default.
func DefaultMatcherOptions() MatcherOptions {
	return MatcherOptions{Tokenize: true, Normalize: true, NegationChar: '-', SortMethod: SortFzf}
}

// MatchEngine filters and ranks a mode's items against the query. The
// fzf-syntax methods (normal, fuzzy, prefix) rewrite the query into
// nucleo's pattern language and score every item; regex and glob scan.
// It is synchronous: the engine goroutine calls it per keystroke, which
// for launcher-sized lists is cheaper than nucleo's worker threads.
type MatchEngine struct {
	items     []Item
	hay       [][]rune
	permanent []uint32
	opts      MatcherOptions
	query     string
	matched   []uint32
}

// NewMatchEngine builds an engine with no items.
func NewMatchEngine(opts MatcherOptions) *MatchEngine {
	return &MatchEngine{opts: opts}
}

// Items returns the current items.
func (e *MatchEngine) Items() []Item { return e.items }

// Options returns the matching options.
func (e *MatchEngine) Options() MatcherOptions { return e.opts }

// SetItems replaces the item set (a mode load or reload) and re-runs
// the query.
func (e *MatchEngine) SetItems(items []Item) {
	e.items = items
	e.hay = make([][]rune, len(items))
	e.permanent = e.permanent[:0]
	for i, it := range items {
		e.hay[i] = graphemeRunes(it.MatchText)
		if it.Flags.Has(FlagPermanent) {
			e.permanent = append(e.permanent, uint32(i))
		}
	}
	e.SetQuery(e.query)
}

// SetOptions replaces the matching options and re-runs the query.
func (e *MatchEngine) SetOptions(opts MatcherOptions) {
	e.opts = opts
	e.SetQuery(e.query)
}

// SetQuery updates the query and recomputes the matches.
func (e *MatchEngine) SetQuery(query string) {
	e.query = query
	switch e.opts.Method {
	case MatchRegex:
		e.matched = e.scanRegex()
	case MatchGlob:
		e.matched = e.scanGlob()
	default:
		e.matched = e.scanPattern()
	}
}

// Matched returns the ranked matched item indices plus the trailing
// PERMANENT rows the filter dropped.
func (e *MatchEngine) Matched() []uint32 {
	out := slices.Clone(e.matched)
	if e.opts.Sort && e.opts.SortMethod == SortLevenshtein {
		slices.SortStableFunc(out, func(a, b uint32) int {
			return levenshtein(e.query, e.items[a].MatchText) - levenshtein(e.query, e.items[b].MatchText)
		})
	}
	for _, p := range e.permanent {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// caseSensitive resolves the case mode against the query.
func (e *MatchEngine) caseSensitive() bool {
	switch e.opts.Case {
	case CaseModeSensitive:
		return true
	case CaseModeSmart:
		return strings.IndexFunc(e.query, unicode.IsUpper) >= 0
	default:
		return false
	}
}

// scanPattern runs the nucleo path: every item scored against the
// rewritten pattern, in list order unless sorting by quality, where
// nucleo's order is score, then shorter text, then list order.
func (e *MatchEngine) scanPattern() []uint32 {
	atoms := parsePattern(rewriteQuery(e.query, e.opts), !e.caseSensitive(), e.opts.Normalize)
	type hit struct {
		idx   uint32
		score int
	}
	hits := make([]hit, 0, len(e.items))
	for i, hay := range e.hay {
		if s, ok := patternScore(atoms, hay); ok {
			hits = append(hits, hit{idx: uint32(i), score: s})
		}
	}
	if e.opts.Sort && e.query != "" {
		slices.SortStableFunc(hits, func(a, b hit) int {
			if a.score != b.score {
				return b.score - a.score
			}
			if la, lb := len(e.hay[a.idx]), len(e.hay[b.idx]); la != lb {
				return la - lb
			}
			return int(a.idx) - int(b.idx)
		})
	}
	out := make([]uint32, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// rewriteQuery turns a rofi query into nucleo's fzf pattern syntax:
// normal -> 'substring atoms, prefix -> ^prefix atoms, fuzzy -> the
// query untouched. Tokens led by the negation char become ! atoms.
// With tokenize off the whole query is one atom, spaces escaped.
func rewriteQuery(query string, opts MatcherOptions) string {
	if query == "" {
		return ""
	}
	kind := ""
	switch opts.Method {
	case MatchNormal:
		kind = "'"
	case MatchPrefix:
		kind = "^"
	}
	token := func(tok string) string {
		negated, body := false, tok
		if rest, ok := strings.CutPrefix(tok, string(opts.NegationChar)); ok && rest != "" {
			negated, body = true, rest
		}
		out := kind + body
		if negated {
			out = "!" + out
		}
		return out
	}
	if !opts.Tokenize {
		return token(strings.ReplaceAll(query, " ", `\ `))
	}
	fields := strings.Fields(query)
	for i, f := range fields {
		fields[i] = token(f)
	}
	return strings.Join(fields, " ")
}

func (e *MatchEngine) all() []uint32 {
	out := make([]uint32, len(e.items))
	for i := range out {
		out[i] = uint32(i)
	}
	return out
}

// scanRegex matches the whole query as a regular expression. An
// invalid one (a trailing '[' while typing) matches nothing, as rofi.
func (e *MatchEngine) scanRegex() []uint32 {
	if e.query == "" {
		return e.all()
	}
	expr := e.query
	if !e.caseSensitive() {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil
	}
	var out []uint32
	for i, it := range e.items {
		if re.MatchString(it.MatchText) {
			out = append(out, uint32(i))
		}
	}
	return out
}

// scanGlob matches each token as `*token*` (glob crate semantics, `*`
// crossing separators); a malformed token matches nothing.
func (e *MatchEngine) scanGlob() []uint32 {
	if e.query == "" {
		return e.all()
	}
	sensitive := e.caseSensitive()
	tokens := []string{e.query}
	if e.opts.Tokenize {
		tokens = strings.Fields(e.query)
	}
	type pattern struct {
		negated bool
		p       glob.Pattern
	}
	patterns := make([]pattern, 0, len(tokens))
	for _, tok := range tokens {
		negated, body := false, tok
		if rest, ok := strings.CutPrefix(tok, string(e.opts.NegationChar)); ok && rest != "" {
			negated, body = true, rest
		}
		// Wrap for substring semantics without forming `**`, which the
		// glob crate rejects outside a path component.
		lead, trail := "*", "*"
		if strings.HasPrefix(body, "*") {
			lead = ""
		}
		if strings.HasSuffix(body, "*") {
			trail = ""
		}
		p, ok := glob.Compile(lead + body + trail)
		if !ok {
			return nil
		}
		patterns = append(patterns, pattern{negated: negated, p: p})
	}
	var out []uint32
	for i, it := range e.items {
		keep := true
		for _, p := range patterns {
			if p.p.Matches(it.MatchText, sensitive) == p.negated {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, uint32(i))
		}
	}
	return out
}

// levenshtein is the edit distance over chars (two-row DP).
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ar {
		cur[0] = i + 1
		for j, cb := range br {
			cost := 1
			if ca == cb {
				cost = 0
			}
			cur[j+1] = min(prev[j]+cost, prev[j+1]+1, cur[j]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
