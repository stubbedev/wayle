package i18n

import (
	"testing"
	"testing/fstest"
)

const (
	iso = "\u2068"
	pop = "\u2069"
)

// loaderFor builds a loader over in-memory locale trees.
func loaderFor(t *testing.T, files map[string]string, requested ...string) *Loader {
	t.Helper()
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	var req []LangID
	for _, r := range requested {
		req = append(req, MustLangID(r))
	}
	l, err := Select(NewAssets(m), req)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func enOnly(t *testing.T, src string) *Loader {
	t.Helper()
	return loaderFor(t, map[string]string{"en-US/_a.ftl": src})
}

func TestPlainTextAndMultiline(t *testing.T) {
	l := enOnly(t, "hello = Hello, world!\n"+
		"block =\n    First line\n      indented\n    Last line\n\n\n"+
		"trail = trailing spaces   \n")
	cases := map[string]string{
		"hello": "Hello, world!",
		"block": "First line\n  indented\nLast line",
		"trail": "trailing spaces",
	}
	for id, want := range cases {
		if got := l.Get(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestVariablesAreIsolatedOnlyWithSurroundingText(t *testing.T) {
	l := enOnly(t, "greet = Hi { $name }!\nalone = { $name }\nlit = a { \"b\" } c\n")
	if got := l.Get("greet", Str("name", "Ann")); got != "Hi "+iso+"Ann"+pop+"!" {
		t.Errorf("greet = %q", got)
	}
	// A lone placeable is the whole pattern: no isolation.
	if got := l.Get("alone", Str("name", "Ann")); got != "Ann" {
		t.Errorf("alone = %q", got)
	}
	// String literals are never isolated.
	if got := l.Get("lit"); got != "a b c" {
		t.Errorf("lit = %q", got)
	}
}

func TestMissingVariableWritesItsReference(t *testing.T) {
	l := enOnly(t, "greet = Hi { $name }!\n")
	if got := l.Get("greet"); got != "Hi "+iso+"{$name}"+pop+"!" {
		t.Errorf("greet = %q", got)
	}
}

func TestNumbersFormatLikeRust(t *testing.T) {
	l := enOnly(t, "n = { $n }\nlit = { 1.50 }\n")
	cases := []struct {
		arg  Arg
		want string
	}{
		{Int("n", 3), "3"},
		{Float("n", 2.5), "2.5"},
		{Float("n", 3.0), "3"},
		{Float("n", 1e21), "1000000000000000000000"},
	}
	for _, c := range cases {
		if got := l.Get("n", c.arg); got != c.want {
			t.Errorf("n(%v) = %q, want %q", c.arg, got, c.want)
		}
	}
	if got := l.Get("lit"); got != "1.50" {
		t.Errorf("literal keeps its fraction digits: %q", got)
	}
}

func TestSelectPluralCategories(t *testing.T) {
	src := "count = { $count ->\n    [one] one item\n    [0] no items\n   *[other] { $count } items\n}\n"
	files := map[string]string{"en-US/_a.ftl": src, "fr/_a.ftl": src}
	en := loaderFor(t, files)
	fr := loaderFor(t, files, "fr")
	for _, c := range []struct {
		l    *Loader
		n    int64
		want string
	}{
		{en, 1, "one item"},
		{en, 0, "no items"}, // exact numeric keys win in source order
		{en, 5, iso + "5" + pop + " items"},
		{fr, 1, "one item"},
		{fr, 0, "one item"}, // fr "one" is i = 0,1 and comes first
		{fr, 2, iso + "2" + pop + " items"},
	} {
		if got := c.l.Get("count", Int("count", c.n)); got != c.want {
			t.Errorf("%v count(%d) = %q, want %q", c.l.Languages(), c.n, got, c.want)
		}
	}
	// French "one" covers 0 and 1 when no exact key precedes it.
	frOnly := loaderFor(t, map[string]string{
		"en-US/_a.ftl": "x = en\n",
		"fr/_a.ftl":    "c = { $n ->\n [one] un\n*[other] plusieurs\n}\n",
	}, "fr")
	if got := frOnly.Get("c", Int("n", 0)); got != "un" {
		t.Errorf("fr 0 = %q, want un", got)
	}
	if got := frOnly.Get("c", Float("n", 1.5)); got != "un" {
		t.Errorf("fr 1.5 = %q, want un (i = 1)", got)
	}
	// English "one" needs v = 0.
	enOne := enOnly(t, "c = { $n ->\n [one] one\n*[other] other\n}\n")
	if got := enOne.Get("c", Float("n", 1.5)); got != "other" {
		t.Errorf("en 1.5 = %q, want other", got)
	}
}

func TestSelectStringsAndDefault(t *testing.T) {
	l := enOnly(t, "s = { $kind ->\n    [wifi] Wireless\n   *[other] Unknown\n}\n")
	if got := l.Get("s", Str("kind", "wifi")); got != "Wireless" {
		t.Errorf("wifi = %q", got)
	}
	if got := l.Get("s", Str("kind", "one")); got != "Unknown" {
		t.Errorf("a plural keyword never matches a string selector: %q", got)
	}
	if got := l.Get("s"); got != "Unknown" {
		t.Errorf("missing selector falls to default: %q", got)
	}
}

func TestReferencesTermsAndAttributes(t *testing.T) {
	l := enOnly(t, "-brand = Wayle\n    .short = W\n"+
		"about = About { -brand }\n"+
		"ref = See { about }\n"+
		"attr = Tip: { menu.tooltip }\n"+
		"menu = Menu\n    .tooltip = Open the menu\n"+
		"param = { -greet(who: \"Bob\") }\n"+
		"-greet = Hello { $who }\n"+
		"-leak = { $x }\n"+
		"leak = { -leak }\n")
	cases := map[string]string{
		"about": "About Wayle",
		"ref":   "See About Wayle",
		"attr":  "Tip: Open the menu",
		"param": "Hello " + iso + "Bob" + pop,
	}
	for id, want := range cases {
		if got := l.Get(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if got := l.Attr("menu", "tooltip"); got != "Open the menu" {
		t.Errorf("attr = %q", got)
	}
	// Terms see only their own arguments, never the caller's.
	if got := l.Get("leak", Str("x", "outer")); got != "{$x}" {
		t.Errorf("leak = %q", got)
	}
}

func TestBrokenReferencesWriteMarkers(t *testing.T) {
	l := enOnly(t, "a = x { missing } y\nb = { -nope }\nc = { NUMBER($n) }\n"+
		"d = { onlyattr }\nonlyattr =\n    .t = T\ne = { menu.nope }\nmenu = M\n"+
		"cyc = { cyc }\n")
	cases := map[string]string{
		"a":   "x {missing} y",
		"b":   "{-nope}",
		"c":   "{NUMBER()}",
		"d":   "{onlyattr}",
		"e":   "{menu.nope}",
		"cyc": "{cyc}",
	}
	for id, want := range cases {
		if got := l.Get(id, Int("n", 1)); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestMissingMessageAndAttributeMarkers(t *testing.T) {
	l := enOnly(t, "menu = M\n    .tooltip = T\nnovalue =\n    .a = A\n")
	if got := l.Get("nope"); got != `No localization for id: "nope"` {
		t.Errorf("missing = %q", got)
	}
	// A message with attributes only has no value for Get.
	if got := l.Get("novalue"); got != `No localization for id: "novalue"` {
		t.Errorf("novalue = %q", got)
	}
	if got := l.Attr("menu", "nope"); got != `No localization for message id: "menu" and attribute id: "nope"` {
		t.Errorf("missing attr = %q", got)
	}
	if l.Has("nope") || !l.Has("menu") {
		t.Error("Has disagrees with Get")
	}
}

func TestFallbackChainAndDuplicates(t *testing.T) {
	l := loaderFor(t, map[string]string{
		"en-US/_a.ftl": "both = English\nonly-en = Only English\n    .a = en attr\n",
		"fr/_a.ftl":    "both = Français\nboth = ignored duplicate\n",
	}, "fr-FR")
	if got := l.Get("both"); got != "Français" {
		t.Errorf("both = %q", got)
	}
	if got := l.Get("only-en"); got != "Only English" {
		t.Errorf("fallback = %q", got)
	}
	if got := l.Attr("only-en", "a"); got != "en attr" {
		t.Errorf("attr fallback = %q", got)
	}
}

func TestStringLiteralEscapes(t *testing.T) {
	l := enOnly(t, `a = { "q\"b\\s\u00e9\U01F600\{" }`+"\n")
	if got := l.Get("a"); got != "q\"b\\s\u00e9\U0001F600\uFFFD" {
		t.Errorf("a = %q", got)
	}
}

func TestPartialsConcatenateInPathOrder(t *testing.T) {
	m := fstest.MapFS{
		"en-US/z/_late.ftl":   {Data: []byte("x = second")},
		"en-US/_early.ftl":    {Data: []byte("x = first")},
		"en-US/generated.ftl": {Data: []byte("x = never")},
	}
	a := NewAssets(m)
	src, err := a.Source("en-US")
	if err != nil {
		t.Fatal(err)
	}
	if src != "x = first\nx = second\n" {
		t.Errorf("source = %q", src)
	}
	l, err := Select(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Get("x"); got != "first" {
		t.Errorf("first definition wins: %q", got)
	}
}

func TestSelectFailsWithoutFallback(t *testing.T) {
	m := fstest.MapFS{"fr/_a.ftl": {Data: []byte("x = y\n")}}
	if _, err := Select(NewAssets(m), []LangID{MustLangID("fr")}); err == nil {
		t.Fatal("a tree without en-US partials must fail to load")
	}
}
