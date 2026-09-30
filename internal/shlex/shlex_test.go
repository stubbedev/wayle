package shlex

import (
	"errors"
	"reflect"
	"testing"
)

// The shlex crate's own split table: the contract this package ports.
func TestSplitMatchesTheCrate(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string // nil = does not parse
	}{
		{"foo$baz", []string{"foo$baz"}},
		{"foo baz", []string{"foo", "baz"}},
		{"foo\"bar\"baz", []string{"foobarbaz"}},
		{"foo \"bar\"baz", []string{"foo", "barbaz"}},
		{"   foo \nbar", []string{"foo", "bar"}},
		{"foo\\\nbar", []string{"foobar"}},
		{"\"foo\\\nbar\"", []string{"foobar"}},
		{"'baz\\$b'", []string{"baz\\$b"}},
		{"'baz\\''", nil},
		{"\\", nil},
		{"\"\\", nil},
		{"'\\", nil},
		{"\"", nil},
		{"'", nil},
		{"foo #bar\nbaz", []string{"foo", "baz"}},
		{"foo #bar", []string{"foo"}},
		{"foo#bar", []string{"foo#bar"}},
		{"foo\"#bar", nil},
		{"'\\n'", []string{"\\n"}},
		{"'\\\\n'", []string{"\\\\n"}},
		{"", []string{}},
	} {
		got, ok := Split(tc.in)
		if tc.want == nil {
			if ok {
				t.Errorf("Split(%q) = %q, want a parse failure", tc.in, got)
			}
			continue
		}
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Split(%q) = %q %v, want %q", tc.in, got, ok, tc.want)
		}
	}
}

// The shlex crate's own quote table.
func TestQuoteMatchesTheCrate(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "''"},
		{"foobar", "foobar"},
		{"foo bar", "'foo bar'"},
		{"\"foo bar'\"", "\"\\\"foo bar'\\\"\""},
		{"'foo bar'", "\"'foo bar'\""},
		{"\"", "'\"'"},
		{"\"'", "\"\\\"'\""},
		{"hello!world", "'hello!world'"},
		{"'hello!world", "\"'hello\"'!world'"},
		{"'hello!", "\"'hello\"'!'"},
		{"hello ^ world", "'hello ''^ world'"},
		{"hello^", "hello'^'"},
		{"!world'", "'!world'\"'\""},
		{"{a, b}", "'{a, b}'"},
		{"\n", "'\n'"},
		{"^", "'^'"},
		{"foo^bar", "foo'^bar'"},
		{"\nx^", "'\nx''^'"},
		{"\n^x", "'\n''^x'"},
		{"\n ^x", "'\n ''^x'"},
		{"{a,b}", "'{a,b}'"},
		{"a,b", "'a,b'"},
		{"a..b", "a..b"},
		{"'$", "\"'\"'$'"},
		{"\"^", "'\"''^'"},
	} {
		got, err := Quote(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Quote(%q) = %q %v, want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestQuoteRefusesNul(t *testing.T) {
	if _, err := Quote("a\x00b"); !errors.Is(err, ErrNul) {
		t.Errorf("Quote with a NUL = %v, want ErrNul", err)
	}
	if got := MustQuote("a\x00b"); got != "a\x00b" {
		t.Errorf("MustQuote falls back to the input, got %q", got)
	}
	if got := MustQuote("a b"); got != "'a b'" {
		t.Errorf("MustQuote(a b) = %q", got)
	}
}

func TestQuotedWordsSplitBack(t *testing.T) {
	for _, in := range []string{"it's here", "my song.mp3", "x; rm -rf ~", "$HOME", "a\"b"} {
		q, err := Quote(in)
		if err != nil {
			t.Fatal(err)
		}
		words, ok := Split(q)
		if !ok || len(words) != 1 || words[0] != in {
			t.Errorf("Split(Quote(%q)) = %q %v", in, words, ok)
		}
	}
}
