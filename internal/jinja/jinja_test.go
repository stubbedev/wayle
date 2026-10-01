package jinja

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestMatchesMinijinja replays testdata/corpus.jsonl against the
// outputs minijinja 2.15.1 (the Rust shell's build: builtins and serde,
// default features off) produced for it in corpus.golden.jsonl: the same
// text, or an error where minijinja errors. Regenerate the golden file
// with the oracle program described in testdata/README.
func TestMatchesMinijinja(t *testing.T) {
	cases := readLines(t, "testdata/corpus.jsonl")
	golden := readLines(t, "testdata/corpus.golden.jsonl")
	if len(cases) != len(golden) {
		t.Fatalf("%d cases, %d golden results", len(cases), len(golden))
	}
	for i, line := range cases {
		var c struct {
			T string         `json:"t"`
			C map[string]any `json:"c"`
		}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&c); err != nil {
			t.Fatalf("case %d: %v", i+1, err)
		}
		var want struct {
			Ok  *string `json:"ok"`
			Err bool    `json:"err"`
		}
		if err := json.Unmarshal([]byte(golden[i]), &want); err != nil {
			t.Fatalf("golden %d: %v", i+1, err)
		}
		got, err := Render(c.T, jsonNumbers(c.C))
		switch {
		case want.Err && err == nil:
			t.Errorf("case %d %q: rendered %q, minijinja errors", i+1, c.T, got)
		case !want.Err && err != nil:
			t.Errorf("case %d %q: %v, minijinja renders %q", i+1, c.T, err, *want.Ok)
		case !want.Err && got != *want.Ok:
			t.Errorf("case %d %q:\n got  %q\n want %q", i+1, c.T, got, *want.Ok)
		}
	}
}

// jsonNumbers turns JSON's float64 numbers back into integers where
// serde_json would: a number written without a fraction or exponent.
// encoding/json loses that, so the corpus marks integers by value.
func jsonNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = jsonNumbers(item)
		}
	case []any:
		for i, item := range x {
			x[i] = jsonNumbers(item)
		}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	}
	return v
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestEnvFunctionsAndRenderOr(t *testing.T) {
	var env Env
	env.AddFunction("shout", func(args []any, kwargs map[string]any) (any, error) {
		s := asString(args[0])
		if truthy(kwargs["loud"]) {
			s = upper(s) + "!"
		}
		return s, nil
	})
	got, err := env.Render(`{{ shout("hi", loud=true) }} {{ shout(name) }}`, map[string]any{"name": "x"})
	if err != nil || got != "HI! x" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	// The plain Render has no such function: calling undefined fails.
	if _, err := Render(`{{ shout("hi") }}`, nil); err == nil {
		t.Error("an unregistered function was callable")
	}
	// RenderOr is unwrap_or_default: a failed render is "".
	if got := RenderOr(`{{ missing.attr }}`, nil); got != "" {
		t.Errorf("RenderOr on an error = %q", got)
	}
	if got := RenderOr(`{{ n }}%`, map[string]any{"n": 7}); got != "7%" {
		t.Errorf("RenderOr = %q", got)
	}
}

func TestGoValuesNormalize(t *testing.T) {
	ctx := map[string]any{
		"u8": uint8(3), "f32": float32(1.5), "strs": []string{"a", "b"},
		"m": map[string]int{"k": 2}, "ptr": new(int), "nilptr": (*int)(nil),
	}
	got, err := Render(`{{ u8 + 1 }} {{ f32 }} {{ strs | join }} {{ m.k * 2 }} {{ ptr }} {{ nilptr }}`, ctx)
	if err != nil || got != "4 1.5 ab 4 0 none" {
		t.Fatalf("= %q, %v", got, err)
	}
	if _, err := Render(`x`, []int{1}); err == nil {
		t.Error("a non-map context was accepted")
	}
}
