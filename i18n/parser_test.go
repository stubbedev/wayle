package i18n

import (
	"strings"
	"testing"
)

func ids(res *resource) []string {
	var out []string
	for _, e := range res.entries {
		id := e.id
		if e.term {
			id = "-" + id
		}
		out = append(out, id)
	}
	return out
}

func TestParseEntriesCommentsAndAttributes(t *testing.T) {
	res := parseResource("### resource\n## group\n# note\nmsg = Value\n    .title = Title\n    .desc = Desc\n\n-term = T\nattrs-only =\n    .a = A\n")
	if len(res.junk) != 0 {
		t.Fatalf("junk: %+v", res.junk)
	}
	if got := strings.Join(ids(res), ","); got != "msg,-term,attrs-only" {
		t.Fatalf("entries = %s", got)
	}
	msg := res.entries[0]
	if len(msg.attrs) != 2 || msg.attrs[0].id != "title" || msg.attrs[1].id != "desc" {
		t.Errorf("attributes = %+v", msg.attrs)
	}
	if res.entries[2].value != nil {
		t.Error("attributes-only message must have no value")
	}
}

func TestParseRecoversFromJunk(t *testing.T) {
	src := "good = ok\nbad = { unclosed\n  still junk\nnext = fine\n= no id\n-t =\nlast = done\n"
	res := parseResource(src)
	if got := strings.Join(ids(res), ","); got != "good,next,last" {
		t.Fatalf("entries = %s", got)
	}
	if len(res.junk) != 3 {
		t.Fatalf("want 3 junk entries, got %d: %+v", len(res.junk), res.junk)
	}
	if res.junk[0].content != "bad = { unclosed\n  still junk\n" {
		t.Errorf("junk runs to the next entry start: %q", res.junk[0].content)
	}
}

func TestParseRejects(t *testing.T) {
	for name, src := range map[string]string{
		"unbalanced brace":     "a = x } y\n",
		"no default variant":   "a = { $n ->\n [one] x\n [other] y\n}\n",
		"two defaults":         "a = { $n ->\n*[one] x\n*[other] y\n}\n",
		"empty message":        "a =\n",
		"term without value":   "-t =\n    .a = x\n",
		"message as selector":  "a = { b ->\n*[x] y\n}\n",
		"term attr placeable":  "a = { -t.attr }\n",
		"lowercase callee":     "a = { number($n) }\n",
		"unknown escape":       "a = { \"\\n\" }\n",
		"unterminated string":  "a = { \"abc }\n",
		"short unicode escape": "a = { \"\\u12\" }\n",
	} {
		res := parseResource(src)
		if len(res.junk) == 0 || len(res.entries) != 0 {
			t.Errorf("%s: want junk only, got entries %v junk %d", name, ids(res), len(res.junk))
		}
	}
}

func TestParseBlockTextDedentAndBlankLines(t *testing.T) {
	res := parseResource("a =\n      deep\n    shallow\n\n    after blank\n")
	if len(res.junk) != 0 {
		t.Fatalf("junk: %+v", res.junk)
	}
	var b strings.Builder
	for _, el := range res.entries[0].value.elements {
		b.WriteString(el.text)
	}
	if got := b.String(); got != "  deep\nshallow\n\nafter blank" {
		t.Errorf("text = %q", got)
	}
}

func TestParseContinuationStopsAtSpecialCharacters(t *testing.T) {
	// An indented line starting with '[', '*', or '.' is not text.
	res := parseResource("a = x\n    .attr = y\n")
	if len(res.entries) != 1 || len(res.entries[0].attrs) != 1 {
		t.Fatalf("got %+v", res.entries)
	}
	if got := res.entries[0].value.elements[0].text; got != "x" {
		t.Errorf("value = %q", got)
	}
}

func TestParseSelectVariantKeys(t *testing.T) {
	res := parseResource("a = { $n ->\n [0] zero\n [1.5] frac\n [-2] neg\n *[other] x\n}\n")
	if len(res.junk) != 0 {
		t.Fatalf("junk: %+v", res.junk)
	}
	sel := res.entries[0].value.elements[0].expr.(selectExpr)
	var keys []string
	for _, v := range sel.variants {
		if !v.numberKey && v.key != "other" {
			t.Errorf("key %q should be numeric", v.key)
		}
		keys = append(keys, v.key)
	}
	if strings.Join(keys, ",") != "0,1.5,-2,other" || !sel.variants[3].isDefault {
		t.Errorf("variants = %+v", sel.variants)
	}
}

func TestParseCRLF(t *testing.T) {
	res := parseResource("a = one\r\nb = two\r\n")
	if got := strings.Join(ids(res), ","); got != "a,b" || len(res.junk) != 0 {
		t.Fatalf("entries = %s junk = %+v", got, res.junk)
	}
	if got := res.entries[0].value.elements[0].text; got != "one" {
		t.Errorf("a = %q", got)
	}
}
