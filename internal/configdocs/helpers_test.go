package configdocs

import (
	"strings"
	"testing"
)

func TestRehostRustdoc(t *testing.T) {
	got := rehostRustdoc("summary\n\n## Common\n\n- item\n\n## Examples\n\n- `x`", 3)
	if !strings.Contains(got, "\n#### Common\n") || !strings.Contains(got, "\n#### Examples\n") || strings.Contains(got, "\n## ") {
		t.Errorf("shifted = %q", got)
	}
	if got := rehostRustdoc("# Top", 3); !strings.HasPrefix(got, "#### Top") {
		t.Errorf("a shallow heading is not clamped below: %q", got)
	}
	if got := rehostRustdoc("###### Already at six", 3); !strings.Contains(got, "###### Already at six") || strings.Contains(got, "####### ") {
		t.Errorf("depth cap = %q", got)
	}
	if got := rehostRustdoc("summary\n\n```toml\n## not a heading\n```", 3); !strings.Contains(got, "\n## not a heading\n") {
		t.Errorf("fenced code was rewritten: %q", got)
	}
	if _, _, ok := parseATXHeading("##Foo"); ok {
		t.Error("a heading without a space")
	}
	if _, _, ok := parseATXHeading("####### too deep"); ok {
		t.Error("seven hashes")
	}
	if d, body, ok := parseATXHeading("   ## Indented"); !ok || d != 2 || body != " Indented" {
		t.Errorf("indented = %d %q %v", d, body, ok)
	}
}

func TestSummaryAndBody(t *testing.T) {
	in := "Temperature sensor label. Use `\"auto\"` for automatic detection,\nor specify a label.\nRun `sensors` to see labels.\n\nSecond paragraph."
	if got := summaryLine(in); got != "Temperature sensor label. Use `\"auto\"` for automatic detection, or specify a label. Run `sensors` to see labels." {
		t.Errorf("summary = %q", got)
	}
	if summaryLine("\n\n  hello\nworld") != "hello world" || summaryLine("") != "" || summaryLine("   \n  ") != "" {
		t.Error("summary edges")
	}
	for _, single := range []string{"single paragraph\nwith a soft wrap", "just one line", "just one line\n", "just one line\n   "} {
		if hasRichBody(single) {
			t.Errorf("%q has no second paragraph", single)
		}
	}
	for _, rich := range []string{"summary\n\nmore", "summary\n\n- bullet", "summary line one\nsoft wrap\n\nsecond paragraph"} {
		if !hasRichBody(rich) {
			t.Errorf("%q has a second paragraph", rich)
		}
	}
	if got := bodyAfterSummary("summary line one\nsoft wrap\n\n## Examples\n\n- item"); got != "\n## Examples\n\n- item" {
		t.Errorf("body = %q", got)
	}
}

func TestTypeSlugAndCells(t *testing.T) {
	if typeSlug("ColorValue") != "color-value" || typeSlug("Size") != "size" {
		t.Error("slugs")
	}
	if formatNumber(float64(float32(0.35))) != "0.35" || formatNumber(0.05) != "0.05" || formatNumber(1) != "1" {
		t.Errorf("numbers: %s %s %s", formatNumber(float64(float32(0.35))), formatNumber(0.05), formatNumber(1))
	}
	known := map[string]bool{"ColorValue": true}
	if typeLink("Array_of_Nullable_ColorValue", known) != "array of [`ColorValue`](/config/types#color-value) or null" {
		t.Errorf("wrapped link = %s", typeLink("Array_of_Nullable_ColorValue", known))
	}
	if typeLink("Unknown", known) != "`Unknown`" || typeLink("uint32", known) != "u32" {
		t.Error("plain and primitive names")
	}
	if defaultCell(nil, false) != "required" || defaultCell([]any{}, true) != "`[]`" || defaultCell([]any{"a"}, true) != "`[...]`" {
		t.Error("default cells")
	}
}
