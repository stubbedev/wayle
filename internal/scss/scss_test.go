package scss

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden outputs below were checked against grass 0.13.4 (the
// Rust shell's compiler): equal up to whitespace, except where a test
// comment names a deliberate formatting difference.

// compileFiles writes files into a temp dir and compiles index.scss
// with that dir as the load path.
func compileFiles(t *testing.T, files map[string]string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	css, err := Compile(filepath.Join(dir, "index.scss"), dir)
	return css, dir, err
}

type golden struct {
	name  string
	files map[string]string
	want  string
}

func index(src string) map[string]string { return map[string]string{"index.scss": src} }

func runGoldens(t *testing.T, cases []golden) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := compileFiles(t, tc.files)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if want := strings.TrimLeft(tc.want, "\n"); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

type failure struct {
	name  string
	files map[string]string
	// file is the stylesheet the error names, line its line.
	file        string
	line        int
	msg         string
	unsupported bool
}

func runFailures(t *testing.T, cases []failure) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			css, dir, err := compileFiles(t, tc.files)
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("want *Error, got %v (css %q)", err, css)
			}
			file := tc.file
			if file == "" {
				file = "index.scss"
			}
			if e.File != filepath.Join(dir, file) || e.Line != tc.line || !strings.Contains(e.Msg, tc.msg) {
				t.Errorf("got %s:%d: %s, want %s:%d: ...%s...", e.File, e.Line, e.Msg, file, tc.line, tc.msg)
			}
			if got := errors.Is(err, ErrUnsupported); got != tc.unsupported {
				t.Errorf("errors.Is(err, ErrUnsupported) = %v, want %v", got, tc.unsupported)
			}
		})
	}
}

func TestPlainCSSCompilesToItself(t *testing.T) {
	src := `@import url(theme.css);
/* a comment */
window.background:backdrop > box.horizontal button:not(.flat):hover {
  color: #FFF;
  background-image: linear-gradient(to right, red 0%, blue 100%);
  transition: opacity 200ms cubic-bezier(0.4, 0, 0.2, 1);
  font: 12px/1.5 "Inter", sans-serif;
  margin: 0 -2px;
  width: calc(100% - 10px);
  color: var(--palette-fg, #fff) !important;
}
@define-color accent #ff0000;
@media (min-width: 600px) {
  .a {
    padding: 1px;
  }
}
@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
@font-face {
  font-family: "X";
  src: url(x.woff2) format("woff2");
}
@layer base;
@supports (display: grid) {
  .g {
    display: grid;
  }
}
.empty {}
`
	runGoldens(t, []golden{{"stylesheet", index(src), `
@import url(theme.css);
window.background:backdrop > box.horizontal button:not(.flat):hover {
  color: #FFF;
  background-image: linear-gradient(to right, red 0%, blue 100%);
  transition: opacity 200ms cubic-bezier(0.4, 0, 0.2, 1);
  font: 12px/1.5 "Inter", sans-serif;
  margin: 0 -2px;
  width: calc(100% - 10px);
  color: var(--palette-fg, #fff) !important;
}
@define-color accent #ff0000;
@media (min-width: 600px) {
  .a {
    padding: 1px;
  }
}
@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
@font-face {
  font-family: "X";
  src: url(x.woff2) format("woff2");
}
@layer base;
@supports (display: grid) {
  .g {
    display: grid;
  }
}
`}})
}

func TestComments(t *testing.T) {
	// grass keeps loud comments; they are dropped here (no CSS meaning).
	runGoldens(t, []golden{{"line and block comments are dropped", index(`
// line
.a { /* in */ x: 1; // trailing
  y: /* mid */ 2; }
`), `
.a {
  x: 1;
  y: 2;
}
`}})
	runFailures(t, []failure{{name: "unterminated", files: index(".a {\n /* x: 1; }"), line: 2, msg: "unterminated comment"}})
}

func TestNesting(t *testing.T) {
	runGoldens(t, []golden{
		{"parent selector forms", index(`
.a {
  x: 1;
  &:hover { h: 1; }
  &.b { c: 1; }
  &-suffix { s: 1; }
  .p & { p: 1; }
  > .child { k: 1; }
  + .next { n: 1; }
  ~ .sib { g: 1; }
  .desc { d: 1; }
  :not(&) { not: 1; }
  z: 3;
}
.m > { .n { m: 1; } }
`), `
.a {
  x: 1;
  z: 3;
}
.a:hover {
  h: 1;
}
.a.b {
  c: 1;
}
.a-suffix {
  s: 1;
}
.p .a {
  p: 1;
}
.a > .child {
  k: 1;
}
.a + .next {
  n: 1;
}
.a ~ .sib {
  g: 1;
}
.a .desc {
  d: 1;
}
:not(.a) {
  not: 1;
}
.m > .n {
  m: 1;
}
`},
		{"comma lists expand like grass", index(`
.a, .b { .c, .d { x: y } }
.a, .b { &.x, & + & { x: y } }
.a, .b { :is(& .x) { x: y } }
`), `
.a .c, .a .d, .b .c, .b .d {
  x: y;
}
.a.x, .a + .a, .b.x, .a + .b, .b + .a, .b + .b {
  x: y;
}
:is(.a .x, .b .x) {
  x: y;
}
`},
		{"nested properties", index(`.a { font: 12px { family: x; size: 1px; } margin: { top: 1px; } }`), `
.a {
  font: 12px;
  font-family: x;
  font-size: 1px;
  margin-top: 1px;
}
`},
	})
	runFailures(t, []failure{
		{name: "top-level parent selector", files: index("\n& { a: b }"), line: 2, msg: `parent selector "&"`},
		{name: "top-level declaration", files: index("a: b;"), line: 1, msg: "declarations may only be used within style rules"},
		{name: "placeholder", files: index("%p { a: b }"), line: 1, msg: "placeholder selectors", unsupported: true},
		{name: "rule in nested property", files: index(".a { font: { .b { c: d } } }"), line: 1, msg: "nested property"},
		{name: "unclosed block", files: index(".a {\n b: c;"), line: 2, msg: `expected "}"`},
	})
}

func TestVariables(t *testing.T) {
	runGoldens(t, []golden{
		{"scoping", index(`
$v: 1;
$d: 1;
$d: 2 !default;
$n: null;
$n: 3 !default;
$a_b: under;
.r { $v: 2; a: $v; .t { $v: 3; b: $v; } c: $v; $w: 4; }
.s { a: $v; b: $d; c: $n; d: $a-b; }
.g { $v: 9 !global; }
.h { a: $v; }
`), `
.r {
  a: 2;
  c: 3;
}
.r .t {
  b: 3;
}
.s {
  a: 1;
  b: 1;
  c: 3;
  d: under;
}
.h {
  a: 9;
}
`},
		{"interpolation", index(`
$n: foo;
$side: left;
.#{$n}-bar #{$n} { #{$side}: 0; margin-#{$side}: 1px; x: "#{$n}"; y: a#{$n}c; z: #{"q\"uote"}; }
`), `
.foo-bar foo {
  left: 0;
  margin-left: 1px;
  x: "foo";
  y: afooc;
  z: q"uote;
}
`},
		{"custom properties keep their text", index(`$a: 1px; .r { --x: #{$a} $a calc(1px + 2px); --e: ; }`), `
.r {
  --x: 1px $a calc(1px + 2px);
  --e: ;
}
`},
	})
	runFailures(t, []failure{
		{name: "undefined", files: index(".r {\n  a: $nope;\n}"), line: 2, msg: "undefined variable $nope"},
		{name: "local does not leak", files: index(".r { $l: 1; }\n.s { a: $l; }"), line: 2, msg: "undefined variable $l"},
		{name: "bad flag", files: index("$a: 1 !nope;"), line: 1, msg: `expected "important"`},
	})
}

func TestExpressions(t *testing.T) {
	runGoldens(t, []golden{{"values", index(`
$a: 10px;
$c: #abc;
$l: 1px 2px;
.a {
  a1: $a / 2; a2: (10px / 2); a3: 10px/2; a4: 12px/1.5 serif; a5: 1 + 1 / 2; a6: (1/3);
  b1: 1px + 2px; b2: 1px - 2px; b3: 1px -2px; b4: 1px-2px; b5: $a -1; b6: $a - 1; b7: -$a; b8: 1px + 1in;
  c1: 3 * 2px; c2: 7 % 3; c3: 10px % 3px; c4: 10px / 4px; c5: 0.1 + 0.2; c6: 1e3px; c7: 1.0px .50 1.23456789012345;
  d1: a + b; d2: "a" + b; d3: a + "b"; d4: a - b; d5: a -b; d6: $l + 1; d7: -$l; d8: "a" - "b"; d9: red + a;
  e1: 1 < 2; e2: 1px == 1; e3: 1in == 96px; e4: a == "a"; e5: not true; e6: x and y; e7: false or 3; e8: 1 + 2 == 3;
  f1: null; f2: (); f3: #{null}; f4: ""; f5: $c; f6: [a b]; f7: ((a, b), c); f8: 1px !important; f9: U+0025-00FF;
}
`), `
.a {
  a1: 5px;
  a2: 5px;
  a3: 10px/2;
  a4: 12px/1.5 serif;
  a5: 1.5;
  a6: 0.3333333333;
  b1: 3px;
  b2: -1px;
  b3: 1px -2px;
  b4: -1px;
  b5: 10px -1;
  b6: 9px;
  b7: -10px;
  b8: 97px;
  c1: 6px;
  c2: 1;
  c3: 1px;
  c4: 10px/4px;
  c5: 0.3;
  c6: 1000px;
  c7: 1px 0.5 1.2345678901;
  d1: ab;
  d2: "ab";
  d3: ab;
  d4: a-b;
  d5: a -b;
  d6: 1px 2px1;
  d7: -1px 2px;
  d8: "a"-"b";
  d9: reda;
  e1: true;
  e2: false;
  e3: true;
  e4: true;
  e5: false;
  e6: y;
  e7: 3;
  e8: true;
  f4: "";
  f5: #abc;
  f6: [a b];
  f7: a, b, c;
  f8: 1px !important;
  f9: U+0025-00FF;
}
`}})
	runFailures(t, []failure{
		{name: "incompatible units", files: index(".a {\n  b: 1px + 1em;\n}"), line: 2, msg: "incompatible units"},
		{name: "percent and length", files: index(".a { b: 50% + 10px; }"), line: 1, msg: "incompatible units"},
		{name: "color arithmetic", files: index(".a { b: #fff + 1; }"), line: 1, msg: "undefined operation"},
		{name: "string multiply", files: index(".a { b: a * 2; }"), line: 1, msg: "undefined operation"},
		{name: "complex unit", files: index(".a { b: 2px * 2px; }"), line: 1, msg: "isn't a valid CSS value"},
		{name: "division by zero", files: index("$z: 0; .a { b: 1 / $z; }"), line: 1, msg: "division by zero"},
		{name: "map", files: index("$m: (a: 1);"), line: 1, msg: "maps", unsupported: true},
		{name: "parent in expression", files: index(".a { b: &; }"), line: 1, msg: "parent selector", unsupported: true},
	})
}

func TestCalculationsAndColors(t *testing.T) {
	// Computed colors print as hex or rgba() (grass keeps rgb()/hsl()
	// notation); the colors are identical.
	runGoldens(t, []golden{{"calc and color functions", index(`
$a: 10px;
$c: #abc;
.a {
  a: calc(1px + 2px * 3); b: calc((1px + 2em) * 2); c: calc(100% - (2 * $a)); d: calc(100% / 3);
  e: calc(1px - (2em - 3px)); f: calc(var(--a) + var(--b)); g: calc(calc(1px + 2em) * 2); h: calc(100% - -1px);
  i: calc(#{$a} + 1px); j: min(1px, 1in); k: max(1px, $a); l: min(100%, $a); m: clamp(1px, 2px, 3px);
  n: rgba(0,0,0,.5); o: rgba($c, .5); p: rgba($c, 1); q: rgb(0 0 0 / 50%); r: rgba(red, 50%);
  s: hsl(120, 50%, 50%); t: hsla(120, 50%, 50%, .5); u: rgb(100%, 0%, 50%); v: rgba(var(--c), .5);
  w: color-mix(in srgb, $c 50%, red); x: invert(50%); y: var(--x, $a); z: url(foo.png) url("x");
}
`), `
.a {
  a: 7px;
  b: calc((1px + 2em) * 2);
  c: calc(100% - 20px);
  d: 33.3333333333%;
  e: calc(1px - (2em - 3px));
  f: calc(var(--a) + var(--b));
  g: calc((1px + 2em) * 2);
  h: calc(100% + 1px);
  i: calc(10px + 1px);
  j: 1px;
  k: 10px;
  l: min(100%, 10px);
  m: 2px;
  n: rgba(0, 0, 0, 0.5);
  o: rgba(170, 187, 204, 0.5);
  p: #aabbcc;
  q: rgba(0, 0, 0, 0.5);
  r: rgba(255, 0, 0, 0.5);
  s: #40bf40;
  t: rgba(64, 191, 64, 0.5);
  u: #ff0080;
  v: rgba(var(--c), 0.5);
  w: color-mix(in srgb, #abc 50%, red);
  x: invert(50%);
  y: var(--x, 10px);
  z: url(foo.png) url("x");
}
`}})
	runFailures(t, []failure{
		{name: "sass color function", files: index(".a { b: darken(#fff, 10%); }"), line: 1, msg: "the Sass function darken()", unsupported: true},
		{name: "sass list function", files: index("$l: a b;\n.a { b: nth($l, 1); }"), line: 2, msg: "nth()", unsupported: true},
		{name: "gtk alpha with a color", files: index(".a { b: alpha(currentColor, 0.5); }"), line: 1, msg: "alpha()", unsupported: true},
		{name: "module function", files: index(".a { b: math.div(1, 2); }"), line: 1, msg: "math.div", unsupported: true},
		{name: "not a color", files: index(".a { b: rgba(foo, .5); }"), line: 1, msg: "is not a color"},
		{name: "bad channel unit", files: index(".a { b: rgb(1px, 0, 0); }"), line: 1, msg: `no units or "%"`},
		{name: "keyword args to css function", files: index(".a { b: foo($x: 1); }"), line: 1, msg: "keyword arguments"},
	})
}

func TestMedia(t *testing.T) {
	runGoldens(t, []golden{{"media bubbling and merging", index(`
$bp: 600px;
.a {
  x: 1;
  @media (min-width: $bp) { y: 2; .b { c: d; } @media (max-width: #{$bp * 2}) { z: 3; } }
}
@media screen { .c { @media (min-width: $bp + 1) { d: e; } } }
@media print { @media screen { .p { q: r; } } }
@supports (display: grid) { .s { @media (x: 1) { t: u; } } }
.k { @keyframes spin { to { a: b; } } @font-face { font-family: x; } }
`), `
.a {
  x: 1;
}
@media (min-width: 600px) {
  .a {
    y: 2;
  }
  .a .b {
    c: d;
  }
}
@media (min-width: 600px) and (max-width: 1200px) {
  .a {
    z: 3;
  }
}
@media screen and (min-width: 601px) {
  .c {
    d: e;
  }
}
@media print {
  @media screen {
    .p {
      q: r;
    }
  }
}
@supports (display: grid) {
  @media (x: 1) {
    .s {
      t: u;
    }
  }
}
@keyframes spin {
  to {
    a: b;
  }
}
@font-face {
  .k {
    font-family: x;
  }
}
`}})
	runFailures(t, []failure{
		{name: "declaration in top-level media", files: index("@media (x: 1) {\n  a: b;\n}"), line: 2, msg: "within style rules"},
		{name: "variable outside a feature", files: index("$q: screen;\n@media $q { .a { b: c } }"), line: 2, msg: "#{}", unsupported: true},
	})
}

func TestImports(t *testing.T) {
	runGoldens(t, []golden{
		{"partials, lists, subdirectories, shared scope", map[string]string{
			"index.scss":     "$m: 5px;\n@import \"mod\", \"sub/a\";\n.i { x: $m; y: $n; }\n",
			"_mod.scss":      "$m: 1px !default;\n$n: 2;\n.mod { m: $m; }\n",
			"sub/_a.scss":    "@import \"b\";\n",
			"sub/_b.scss":    ".rel { a: b; }\n",
			"_b.scss":        ".load { a: b; }\n",
			"unused.css":     ".unused { a: b; }\n",
			"sub/other.scss": ".never { a: b; }\n",
		}, `
.mod {
  m: 5px;
}
.rel {
  a: b;
}
.i {
  x: 5px;
  y: 2;
}
`},
		{"plain CSS imports are hoisted, .css files inlined", map[string]string{
			"index.scss": ".a { b: c; }\n@import \"remote.css\";\n@import url(x.css);\n@import \"http://x/y\";\n@import \"q\" screen;\n@import \"inline\";\n",
			"inline.css": ".css { a: b; }\n",
		}, `
@import "remote.css";
@import url(x.css);
@import "http://x/y";
@import "q" screen;
.a {
  b: c;
}
.css {
  a: b;
}
`},
		{"nested import", map[string]string{
			"index.scss": ".w { @import \"a\"; }\n",
			"_a.scss":    ".x { a: b; }\n",
		}, `
.w .x {
  a: b;
}
`},
	})
	// grass's resolution order, one candidate pair at a time.
	for _, tc := range []struct {
		name   string
		files  map[string]string
		winner string
	}{
		{"name.scss over _name.scss", map[string]string{"a.scss": "", "_a.scss": ""}, "a.scss"},
		{"_name.scss over name.css", map[string]string{"_a.scss": "", "a.css": ""}, "_a.scss"},
		{"name.css over name/index.scss", map[string]string{"a.css": "", "a/index.scss": ""}, "a.css"},
		{"_name.css over name/_index.scss", map[string]string{"_a.css": "", "a/_index.scss": ""}, "_a.css"},
		{"name/index.scss over name/_index.scss", map[string]string{"a/index.scss": "", "a/_index.scss": ""}, "a/index.scss"},
		{"name/_index.scss", map[string]string{"a/_index.scss": ""}, "a/_index.scss"},
		{"name/index.css", map[string]string{"a/index.css": ""}, "a/index.css"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"index.scss": `@import "a";`}
			for name := range tc.files {
				files[name] = "." + strings.NewReplacer("/", "-", ".", "-", "_", "u").Replace(name) + " { a: b; }\n"
			}
			got, _, err := compileFiles(t, files)
			if err != nil {
				t.Fatal(err)
			}
			if want := files[tc.winner]; !strings.HasPrefix(got, strings.Fields(want)[0]+" {") {
				t.Errorf("got %q, want the rule from %s", got, tc.winner)
			}
		})
	}
	runFailures(t, []failure{
		{name: "missing", files: index("\n@import \"nope\";"), line: 2, msg: `can't find stylesheet to import: "nope"`},
		{name: "cycle", files: map[string]string{"index.scss": `@import "a";`, "_a.scss": `@import "b";`, "_b.scss": "\n@import \"a\";"}, file: "_b.scss", line: 2, msg: "already being loaded"},
		{name: "self import", files: index(`@import "index";`), line: 1, msg: "already being loaded"},
		{name: "css file with a variable", files: map[string]string{"index.scss": `@import "p";`, "p.css": ".p { x: $v; }"}, file: "p.css", line: 1, msg: "Sass variables aren't allowed in plain CSS"},
		{name: "css file with nesting", files: map[string]string{"index.scss": `@import "p";`, "p.css": ".p {\n .n { y: z } }"}, file: "p.css", line: 2, msg: "nested style rules"},
		{name: "css file with a silent comment", files: map[string]string{"index.scss": `@import "p";`, "p.css": ".p { x: y; } // c"}, file: "p.css", line: 1, msg: "silent comments"},
		{name: "indented syntax", files: map[string]string{"index.scss": `@import "a";`, "a.sass": ".s\n  a: b"}, line: 1, msg: "indented syntax", unsupported: true},
		{name: "imported file error names that file", files: map[string]string{"index.scss": `@import "a";`, "_a.scss": ".x {\n\n y: $nope; }"}, file: "_a.scss", line: 3, msg: "undefined variable"},
	})
}

func TestUse(t *testing.T) {
	runGoldens(t, []golden{
		{"members, namespaces, configuration, one load", map[string]string{
			"index.scss": "@use \"mod\";\n@use \"mod\" as m;\n@use \"star\" as *;\n@use \"conf\" with ($v: 2);\n@use \"sub\";\n" +
				".a { x: mod.$m; y: m.$m; z: $s; @include mod.mx(1) { c: d; } @include smx; }\n",
			"_mod.scss":       "$m: 1px !default;\n@mixin mx($a, $b: 2) { m: $a $b; @content; }\n.mod { m: $m; }\n",
			"_star.scss":      "$s: star;\n@mixin smx { s: 1; }\n",
			"_conf.scss":      "$v: 1 !default;\n.conf { v: $v; }\n",
			"sub/_index.scss": "@use \"../mod\";\n.idx { m: mod.$m; }\n",
		}, `
.mod {
  m: 1px;
}
.conf {
  v: 2;
}
.idx {
  m: 1px;
}
.a {
  x: 1px;
  y: 1px;
  z: star;
  m: 1 2;
  c: d;
  s: 1;
}
`},
		{"module variable assignment", map[string]string{
			"index.scss": "@use \"a\";\na.$v: 3;\n.r { x: a.$v; }\n",
			"_a.scss":    "$v: 1;\n",
		}, `
.r {
  x: 3;
}
`},
	})
	runFailures(t, []failure{
		{name: "private member", files: map[string]string{"index.scss": "@use \"mod\";\n.a { x: mod.$-p; }", "_mod.scss": "$-p: 1;"}, line: 2, msg: "private members"},
		{name: "module does not see importer", files: map[string]string{"index.scss": "$z: 1;\n@use \"mod\";", "_mod.scss": ".a { x: $z; }"}, file: "_mod.scss", line: 1, msg: "undefined variable $z"},
		{name: "use after rule", files: index(".a { b: c; }\n@use \"x\";"), line: 2, msg: "@use rules must be written before"},
		{name: "use inside rule", files: index(".a { @use \"x\"; }"), line: 1, msg: "@use rules must be written before"},
		{name: "namespace clash", files: map[string]string{"index.scss": "@use \"a\";\n@use \"sub/a\";", "_a.scss": "", "sub/_a.scss": ""}, line: 2, msg: `already a module with namespace "a"`},
		{name: "no such namespace", files: index(".a { x: q.$v; }"), line: 1, msg: `no module with the namespace "q"`},
		{name: "module loop", files: map[string]string{"index.scss": `@use "a";`, "_a.scss": `@use "b";`, "_b.scss": `@use "a";`}, file: "_b.scss", line: 1, msg: "module loop"},
		{name: "unused configuration", files: map[string]string{"index.scss": `@use "a" with ($nope: 1);`, "_a.scss": "$v: 1 !default;"}, line: 1, msg: "$nope was not declared with !default"},
		{name: "configured twice", files: map[string]string{"index.scss": "@use \"a\";\n@use \"b\";", "_a.scss": "$v: 1 !default;", "_b.scss": "@use \"a\" with ($v: 2);"}, file: "_b.scss", line: 1, msg: "already loaded"},
		{name: "built-in module", files: index(`@use "sass:math";`), line: 1, msg: "sass:math", unsupported: true},
	})
}

func TestMixins(t *testing.T) {
	runGoldens(t, []golden{{"arguments, content, and scoping", index(`
$g: outer;
@mixin m($x, $y: $x) { a: $x $y; .in { @content; } b: $g; }
@mixin kw($a, $b: 2) { k: $a $b; }
@mixin outer { .o { @content; } }
@mixin inner { @include outer { i: 1; @content; } }
@mixin media { @media (x: 1) { mq: 1; } }
@mixin setg { $g2: 5 !global; }
@mixin top { .t { a: b; } }
@include top;
.r {
  $g: inner;
  @include m(1) { c: $g; }
  @include kw($b: 3, $a: 0);
  @include inner { c: 2; }
  @include media;
  @include setg;
  g2: $g2;
}
`), `
.t {
  a: b;
}
.r {
  a: 1 1;
  b: outer;
  k: 0 3;
  g2: 5;
}
.r .in {
  c: inner;
}
.r .o {
  i: 1;
  c: 2;
}
@media (x: 1) {
  .r {
    mq: 1;
  }
}
`}})
	runFailures(t, []failure{
		{name: "missing argument", files: index("@mixin m($a) { a: $a; }\n.r { @include m; }"), line: 2, msg: "missing argument $a"},
		{name: "too many arguments", files: index("@mixin m($a) { a: $a; }\n.r { @include m(1, 2); }"), line: 2, msg: "only 1 argument(s) allowed, but 2 were passed"},
		{name: "unknown keyword", files: index("@mixin m($a: 1) { a: $a; }\n.r { @include m($b: 1); }"), line: 2, msg: "no argument named $b"},
		{name: "undefined mixin", files: index(".r { @include nope; }"), line: 1, msg: "undefined mixin nope"},
		{name: "closure does not see caller", files: index("@mixin m { a: $v; }\n.r { $v: 1; @include m; }"), line: 1, msg: "undefined variable $v"},
		{name: "top-level declarations", files: index("@mixin m { a: b; }\n@include m;"), line: 1, msg: "within style rules"},
		{name: "content outside mixin", files: index(".r { @content; }"), line: 1, msg: "@content is only allowed within mixin"},
		{name: "recursion", files: index("@mixin m { @include m; }\n.r { @include m; }"), line: 1, msg: "@include nesting deeper"},
		{name: "rest arguments", files: index("@mixin m($a...) {}"), line: 1, msg: "rest arguments", unsupported: true},
		{name: "using", files: index("@mixin m { }\n.r { @include m using ($x) { } }"), line: 2, msg: "using", unsupported: true},
	})
}

func TestUnsupportedAtRules(t *testing.T) {
	var cases []failure
	for _, rule := range []string{"if true", "each $x in a", "for $i from 1 through 2", "while false", "function f()", "extend .a", "at-root .b", "debug 1", "warn 1", "error 1", "forward \"a\""} {
		name := strings.Fields(rule)[0]
		cases = append(cases, failure{name: name, files: index(".a { b: c; }\n@" + rule + " { }"), line: 2, msg: "@" + name, unsupported: true})
	}
	runFailures(t, cases)
}

func TestErrorFormatting(t *testing.T) {
	err := &Error{File: "/c/styles/index.scss", Line: 3, Msg: "boom"}
	if got := err.Error(); got != "/c/styles/index.scss:3: boom" {
		t.Errorf("Error() = %q", got)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Error("a plain error must not match ErrUnsupported")
	}
	if !errors.Is(&Error{unsupported: true}, ErrUnsupported) {
		t.Error("an unsupported error must match ErrUnsupported")
	}
}

func TestCompileMissingEntry(t *testing.T) {
	_, err := Compile(filepath.Join(t.TempDir(), "index.scss"))
	var e *Error
	if !errors.Is(err, fs.ErrNotExist) || errors.As(err, &e) {
		t.Errorf("missing entry: got %v, want the os not-exist error", err)
	}
}
