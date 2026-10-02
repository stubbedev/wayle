package config

import (
	"strings"
	"testing"
)

func TestTOMLPrettyMatchesTheRustSerializer(t *testing.T) {
	root := newTable()
	root.set("imports", []any{})
	root.set("name", "x")
	bar := newTable()
	bar.set("scale", float32(1))
	bar.set("list", []any{"a", "b"})
	bar.set("one", []any{int64(1)})
	root.set("bar", bar)
	modules := newTable()
	clock := newTable()
	clock.set("format", "%H")
	modules.set("clock", clock)
	root.set("modules", modules)
	layouts := []any{newTable(), newTable()}
	layouts[0].(*table).set("monitor", "*")
	layouts[1].(*table).set("monitor", "DP-1")
	root.set("layout", layouts)
	got, err := tomlPretty(root)
	if err != nil {
		t.Fatal(err)
	}
	want := `imports = []
name = "x"

[bar]
scale = 1.0
list = [
    "a",
    "b",
]
one = [1]

[modules.clock]
format = "%H"

[[layout]]
monitor = "*"

[[layout]]
monitor = "DP-1"
`
	if got != want {
		t.Errorf("pretty =\n%s\nwant\n%s", got, want)
	}
	if _, err := tomlPretty([]any{"x"}); err == nil {
		t.Error("an array is not a document")
	}
}

func TestTOMLStringEncodings(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:       `"plain"`,
		`has "quote"`: `'has "quote"'`,
		`back\slash`:  `'back\slash'`,
		"line\nbreak": "\"\"\"\nline\nbreak\"\"\"",
		`it's "both"`: `"""it's "both""""`,
		"tab\there":   `"tab\there"`,
		"bell\x07":    `"bell\u0007"`,
		"":            `""`,
	} {
		if got := tomlString(in); got != want {
			t.Errorf("%q -> %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"bare-key_1": "bare-key_1", "a.b": `"a.b"`, "": `""`, `q"`: `'q"'`} {
		if got := tomlKey(in); got != want {
			t.Errorf("key %q -> %s, want %s", in, got, want)
		}
	}
}

func TestTOMLFloatsAndInline(t *testing.T) {
	for v, want := range map[float64]string{1: "1.0", 0.5: "0.5", 0: "0.0", -2: "-2.0"} {
		if got := tomlFloat(v, 64); got != want {
			t.Errorf("%v -> %s, want %s", v, got, want)
		}
	}
	if got := tomlFloat(float64(float32(0.35)), 32); got != "0.35" {
		t.Errorf("f32 0.35 -> %s", got)
	}
	inner := newTable()
	inner.set("a", int64(1))
	inner.set("b", []any{"x", "y"})
	if got := TOMLInline(inner); got != `{ a = 1, b = ["x", "y"] }` {
		t.Errorf("inline table = %s", got)
	}
	if got := TOMLInline(newTable()); got != "{}" {
		t.Errorf("empty inline table = %s", got)
	}
}

func TestDefaultTOMLStartsWithImportsAndCarriesEverySection(t *testing.T) {
	out := DefaultTOML()
	if !strings.HasPrefix(out, "imports = []\n\n[general]\n") {
		t.Errorf("default begins %q", out[:min(len(out), 60)])
	}
	for _, header := range []string{"[bar]\n", "[[bar.layout]]\n", "[dropdowns.audio]\n", "[modules.battery]\n", "[osd]\n", "[animations]\n", "[animations.osd]\n"} {
		if !strings.Contains(out, header) {
			t.Errorf("default lacks %q", header)
		}
	}
	if !strings.Contains(out, "padding = 0.35\n") {
		t.Error("f32 defaults print in their own width in `config default`")
	}
	if strings.Contains(out, "extends") {
		t.Error("None fields are omitted from TOML")
	}
}

func TestParseCLIValue(t *testing.T) {
	for raw, want := range map[string]any{"true": true, "5": int64(5), "1.5": 1.5, `"q"`: "q", "plain words": "plain words"} {
		if got := ParseCLIValue(raw); got != want {
			t.Errorf("%q -> %#v, want %#v", raw, got, want)
		}
	}
	if list, ok := ParseCLIValue(`["a", "b"]`).([]any); !ok || len(list) != 2 {
		t.Error("an array parses as an array")
	}
}

// ParseTOML reads back what TOMLDocument writes, in the tree shapes
// SetByPath takes, and refuses what is no TOML.
func TestParseTOMLRoundTrips(t *testing.T) {
	doc := map[string]any{"custom": []any{
		map[string]any{"id": "cpu", "command": "echo 1", "interval-ms": int64(5000)},
		map[string]any{"id": "mem", "ratio": 0.5},
	}}
	text, err := TOMLDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "[[custom]]") {
		t.Errorf("document %q lacks the array tables", text)
	}
	got, err := ParseTOML(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	list, ok := got["custom"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("custom = %#v, want a two-item []any", got["custom"])
	}
	if m := list[0].(map[string]any); m["id"] != "cpu" || m["interval-ms"] != int64(5000) {
		t.Errorf("first module %#v", m)
	}
	if _, err := ParseTOML("custom = [unterminated"); err == nil {
		t.Error("a broken document parsed")
	}
	svc := Load(t.TempDir(), DiscardDiagnostics)
	defer svc.Close()
	parsed, _ := ParseTOML("[[custom]]\nid = \"x\"\ncommand = \"true\"\n")
	if err := svc.SetByPath("modules.custom", parsed["custom"]); err != nil {
		t.Errorf("SetByPath refused the parsed modules: %v", err)
	}
	if len(svc.Config().Custom) != 1 || svc.Config().Custom[0].Id != "x" {
		t.Errorf("modules %+v", svc.Config().Custom)
	}
}
