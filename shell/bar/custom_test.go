package bar

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestParseCustomOutputPlain(t *testing.T) {
	parsed := parseCustomOutput(" 42\x1b[0m\n")
	if parsed.raw != "42\x1b[0m" {
		t.Errorf("raw = %q", parsed.raw)
	}
	if parsed.vars["output"] != "42\x1b[0m" {
		t.Errorf("output var = %q", parsed.vars["output"])
	}
	if parsed.text != "" {
		t.Errorf("plain output set text = %q", parsed.text)
	}
}

func TestParseCustomOutputJSON(t *testing.T) {
	parsed := parseCustomOutput(`{"text":"Hello","alt":"muted","percentage":150,"tooltip":"tip","state":"on","output":"ignored"}`)
	if parsed.text != "Hello" || parsed.alt != "muted" || parsed.tooltip != "tip" {
		t.Errorf("reserved fields = %+v", parsed)
	}
	if parsed.percentage != 100 {
		t.Errorf("percentage = %d, want clamped to 100", parsed.percentage)
	}
	if parsed.vars["state"] != "on" {
		t.Errorf("state var = %q", parsed.vars["state"])
	}
	// The raw output stays available even when JSON carries its own.
	if parsed.vars["output"] == "ignored" {
		t.Error("output var was overwritten by a JSON field")
	}
}

func TestParseCustomOutputBrokenJSONFallsBack(t *testing.T) {
	parsed := parseCustomOutput("{oops")
	if parsed.vars["output"] != "{oops" {
		t.Errorf("output var = %q, want the raw text", parsed.vars["output"])
	}
}

func TestFormatCustomLabelTextWins(t *testing.T) {
	def := config.DefaultsCustomModule()
	parsed := parseCustomOutput(`{"text":"override","output":"raw"}`)
	if got := formatCustomLabel(def, parsed); got != "override" {
		t.Errorf("= %q, want the text field", got)
	}
	plain := parseCustomOutput("42%")
	if got := formatCustomLabel(def, plain); got != "42%" {
		t.Errorf("= %q, want the raw output", got)
	}
	def.Format = "{{ state }}: {{ output }}"
	parsed2 := parseCustomOutput(`{"state":"on","output":"1"}`)
	// The output variable is always the raw command output, even when
	// the JSON carries its own "output" field.
	if got := formatCustomLabel(def, parsed2); got != `on: {"state":"on","output":"1"}` {
		t.Errorf("= %q, want the rendered format", got)
	}
	// Unknown variables render empty.
	def.Format = "{{ missing }}x"
	if got := formatCustomLabel(def, plain); got != "x" {
		t.Errorf("= %q, want the empty substitution", got)
	}
}

func TestRenderTemplateWhitespaceInsensitive(t *testing.T) {
	vars := map[string]string{"a": "1"}
	if got := renderTemplate("{{a}}-{{ a }}", vars); got != "1-1" {
		t.Errorf("= %q", got)
	}
	if got := renderTemplate("{{ a.b }}", vars); got != "" {
		t.Errorf("nested path over a scalar = %q, want empty", got)
	}
}

func TestShouldHideCustom(t *testing.T) {
	if shouldHideCustom("1", true) {
		t.Error("1 should not hide")
	}
	for _, out := range []string{"", "0", "false", "False"} {
		if !shouldHideCustom(out, true) {
			t.Errorf("%q should hide", out)
		}
	}
	if shouldHideCustom("", false) {
		t.Error("hide-if-empty off must never hide")
	}
}

func TestCustomDefinitionLookup(t *testing.T) {
	cfg := config.Defaults()
	def := config.DefaultsCustomModule()
	def.ID = "cpu-temp"
	def.Command = "echo 42"
	cfg.Custom = []config.CustomModuleConfig{def}

	got, ok := customDefinition("custom-cpu-temp", cfg)
	if !ok || got.ID != "cpu-temp" {
		t.Errorf("lookup = %+v ok=%v", got, ok)
	}
	if _, ok := customDefinition("clock", cfg); ok {
		t.Error("plain module names must not match the custom- prefix")
	}
	if _, ok := customDefinition("custom-missing", cfg); ok {
		t.Error("unknown id matched")
	}
}

func TestApplyCustomDefinitionsValidates(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	good := "[[modules.custom]]\nid = \"x\"\ncommand = \"echo hi\"\ninterval-ms = 100\nhide-if-empty = true\n"
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(c.Custom) != 1 || c.Custom[0].ID != "x" || c.Custom[0].IntervalMs != 100 || !c.Custom[0].HideIfEmpty {
		t.Errorf("custom = %+v", c.Custom)
	}
	for _, bad := range []string{
		"[[modules.custom]]\ncommand = \"echo hi\"\n",
		"[[modules.custom]]\nid = \"a\"\n[[modules.custom]]\nid = \"a\"\n",
		"[[modules.custom]]\nid = \"a\"\nmode = \"stream\"\n",
		"[[modules.custom]]\nid = \"a\"\ninterval-ms = -5\n",
		"[[modules.custom]]\nid = \"a\"\nlabel-max-length = -1\n",
		"[[modules.custom]]\nid = \"a\"\nleft-click = \"brightness:nope\"\n",
	} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}

func TestCustomTruncatesLabel(t *testing.T) {
	def := config.DefaultsCustomModule()
	def.LabelMaxLength = 3
	parsed := parseCustomOutput("abcdef")
	got := truncateLabel(formatCustomLabel(def, parsed), def.LabelMaxLength)
	if strings.HasSuffix(got, "abcdef") {
		t.Errorf("= %q, want truncated", got)
	}
}
