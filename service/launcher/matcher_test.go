package launcher

import (
	"reflect"
	"testing"
)

func items(texts ...string) []Item {
	out := make([]Item, len(texts))
	for i, t := range texts {
		out[i] = NewItem(t)
	}
	return out
}

func matches(opts MatcherOptions, list []Item, query string) []uint32 {
	e := NewMatchEngine(opts)
	e.SetItems(list)
	e.SetQuery(query)
	return e.Matched()
}

func TestRewriteQuery(t *testing.T) {
	def := DefaultMatcherOptions()
	for _, tc := range []struct {
		name  string
		opts  MatcherOptions
		query string
		want  string
	}{
		{"normal tokens become substring atoms", def, "fire fox", "'fire 'fox"},
		{"prefix tokens", MatcherOptions{Method: MatchPrefix, Tokenize: true, NegationChar: '-'}, "fire fox", "^fire ^fox"},
		{"the negation char becomes !", def, "fire -fox", "'fire !'fox"},
		{"a lone negation char is literal", def, "fire -", "'fire '-"},
		{"no tokenize escapes spaces", MatcherOptions{Method: MatchNormal, NegationChar: '-'}, "fire fox", `'fire\ fox`},
		{"fuzzy passes through", MatcherOptions{Method: MatchFuzzy, Tokenize: true, NegationChar: '-'}, "ffx -web", "ffx !web"},
		{"empty stays empty", def, "", ""},
	} {
		if got := rewriteQuery(tc.query, tc.opts); got != tc.want {
			t.Errorf("%s: rewrite(%q) = %q, want %q", tc.name, tc.query, got, tc.want)
		}
	}
}

func TestNormalMatchingFiltersAndKeepsOrder(t *testing.T) {
	got := matches(DefaultMatcherOptions(), items("Firefox", "Files", "Terminal", "fire pit"), "fire")
	if !reflect.DeepEqual(got, []uint32{0, 3}) {
		t.Errorf("got %v, want [0 3]", got)
	}
	// Negative: substring, not fuzzy - "frx" is a subsequence of Firefox.
	if got := matches(DefaultMatcherOptions(), items("Firefox"), "frx"); len(got) != 0 {
		t.Errorf("normal matching must not match a subsequence, got %v", got)
	}
}

func TestFuzzyMatchesSubsequences(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Method = MatchFuzzy
	got := matches(opts, items("Firefox", "Files", "Terminal"), "frx")
	if !reflect.DeepEqual(got, []uint32{0}) {
		t.Errorf("got %v, want [0]", got)
	}
	if got := matches(opts, items("Firefox"), "xf"); len(got) != 0 {
		t.Errorf("out-of-order chars must not match, got %v", got)
	}
}

func TestNegationExcludes(t *testing.T) {
	got := matches(DefaultMatcherOptions(), items("Firefox", "fire pit", "Files"), "fi -fox")
	if !reflect.DeepEqual(got, []uint32{1, 2}) {
		t.Errorf("got %v, want [1 2]", got)
	}
}

func TestSmartCaseRespectsUppercase(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Case = CaseModeSmart
	if got := matches(opts, items("firefox", "FireFox"), "FireF"); !reflect.DeepEqual(got, []uint32{1}) {
		t.Errorf("uppercase query: got %v, want [1]", got)
	}
	if got := matches(opts, items("firefox", "FireFox"), "firef"); !reflect.DeepEqual(got, []uint32{0, 1}) {
		t.Errorf("lowercase query folds: got %v, want [0 1]", got)
	}
}

func TestCaseSensitivePrefixMatchesTheRightCase(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Method = MatchPrefix
	opts.Case = CaseModeSensitive
	if got := matches(opts, items("Firefox", "firefox"), "Fire"); !reflect.DeepEqual(got, []uint32{0}) {
		t.Errorf("got %v, want [0]", got)
	}
}

func TestNormalizationStripsAccents(t *testing.T) {
	if got := matches(DefaultMatcherOptions(), items("Café", "Cafe", "Tea"), "cafe"); !reflect.DeepEqual(got, []uint32{0, 1}) {
		t.Errorf("normalized: got %v, want [0 1]", got)
	}
	opts := DefaultMatcherOptions()
	opts.Normalize = false
	if got := matches(opts, items("Café", "Cafe"), "cafe"); !reflect.DeepEqual(got, []uint32{1}) {
		t.Errorf("unnormalized: got %v, want [1]", got)
	}
}

func TestRegexScan(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Method = MatchRegex
	list := items("Firefox", "Files", "Terminal")
	if got := matches(opts, list, "^fi.*x$"); !reflect.DeepEqual(got, []uint32{0}) {
		t.Errorf("got %v, want [0]", got)
	}
	if got := matches(opts, list, "[invalid"); len(got) != 0 {
		t.Errorf("an invalid regex matches nothing, got %v", got)
	}
	if got := matches(opts, list, ""); !reflect.DeepEqual(got, []uint32{0, 1, 2}) {
		t.Errorf("an empty regex matches all, got %v", got)
	}
}

func TestGlobScanWithNegation(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Method = MatchGlob
	if got := matches(opts, items("Firefox", "fire pit", "Files"), "fi* -fox"); !reflect.DeepEqual(got, []uint32{1, 2}) {
		t.Errorf("got %v, want [1 2]", got)
	}
	if got := matches(opts, items("Firefox"), "[unclosed"); len(got) != 0 {
		t.Errorf("a malformed glob matches nothing, got %v", got)
	}
}

func TestPermanentRowsSurviveTheFilter(t *testing.T) {
	list := items("Firefox", "Files", "Quit")
	list[2].Flags |= FlagPermanent
	if got := matches(DefaultMatcherOptions(), list, "fire"); !reflect.DeepEqual(got, []uint32{0, 2}) {
		t.Errorf("got %v, want [0 2]", got)
	}
}

func TestLevenshtein(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"kitten", "sitting", 3}, {"", "abc", 3}, {"abc", "abc", 0}} {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLevenshteinSortOrdersByDistance(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Sort, opts.SortMethod = true, SortLevenshtein
	if got := matches(opts, items("firefight", "fire", "firefox"), "fire"); !reflect.DeepEqual(got, []uint32{1, 2, 0}) {
		t.Errorf("got %v, want [1 2 0]", got)
	}
	// Sort off keeps list order even with a query.
	opts.Sort = false
	if got := matches(opts, items("firefight", "fire", "firefox"), "fire"); !reflect.DeepEqual(got, []uint32{0, 1, 2}) {
		t.Errorf("unsorted: got %v", got)
	}
}

func TestFzfSortRanksByScore(t *testing.T) {
	opts := DefaultMatcherOptions()
	opts.Method = MatchFuzzy
	opts.Sort, opts.SortMethod = true, SortFzf
	// A word-boundary match outranks a mid-word one, and among equal
	// scores the shorter text wins.
	got := matches(opts, items("xxfoxx", "the fox", "fox"), "fox")
	if got[0] != 2 || got[1] != 1 || got[2] != 0 {
		t.Errorf("got %v, want [2 1 0]", got)
	}
}

func TestEmptyQueryMatchesAllInOrder(t *testing.T) {
	if got := matches(DefaultMatcherOptions(), items("b", "a", "c"), ""); !reflect.DeepEqual(got, []uint32{0, 1, 2}) {
		t.Errorf("got %v", got)
	}
}

// nucleo's own scoring examples (lib.rs doc tests) pin the port.
func TestNucleoScoresMatchTheCrate(t *testing.T) {
	cfg := matchConfig{ignoreCase: true, normalize: true}
	fuzzy := func(hay, needle string) int {
		s, ok := cfg.fuzzyMatch(graphemeRunes(hay), []rune(needle))
		if !ok {
			t.Fatalf("%q must fuzzy-match %q", needle, hay)
		}
		return s
	}
	// Atom::new("foo bar", ..., Fuzzy) scores "foo bar" 192 with the
	// default config.
	if got := fuzzy("foo bar", "foo bar"); got != 192 {
		t.Errorf("foo bar = %d, want 192", got)
	}
	if _, ok := cfg.fuzzyMatch(graphemeRunes("foobar"), []rune("foo bar")); ok {
		t.Error("a needle with a space must not match a haystack without one")
	}
}
