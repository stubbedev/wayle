package bar

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestParseCustomOutputPlain(t *testing.T) {
	parsed := parseCustomOutput(" 42\x1b[0m\n")
	if parsed.raw != "42\x1b[0m" || parsed.json != nil || parsed.text != nil {
		t.Errorf("plain = %+v", parsed)
	}
	if got := parsed.templateContext()["output"]; got != "42\x1b[0m" {
		t.Errorf("output var = %q", got)
	}
}

func TestParseCustomOutputJSON(t *testing.T) {
	parsed := parseCustomOutput(`{"text":"Hello","alt":"muted","percentage":150,"tooltip":"tip","class":"warn","state":"on","n":3,"output":"ignored"}`)
	if *parsed.text != "Hello" || *parsed.alt != "muted" || *parsed.tooltip != "tip" || !slices.Equal(parsed.class, []string{"warn"}) {
		t.Errorf("reserved fields = %+v", parsed)
	}
	if *parsed.percentage != 100 {
		t.Errorf("percentage = %d, want clamped to 100", *parsed.percentage)
	}
	ctx := parsed.templateContext()
	if ctx["state"] != "on" || ctx["n"] != int64(3) {
		t.Errorf("context = %v", ctx)
	}
	// The raw output wins over a JSON "output" field.
	if ctx["output"] == "ignored" {
		t.Error("output var was overwritten by a JSON field")
	}
	if got := parseCustomOutput(`{"class":["a","b"]}`).class; !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("class array = %v", got)
	}
}

// serde decodes the reserved fields together: one of the wrong type
// leaves every one unset, while the JSON still feeds the template.
func TestParseCustomOutputReservedFieldsAreAllOrNothing(t *testing.T) {
	for _, bad := range []string{`{"text":5,"alt":"x"}`, `{"percentage":75.5,"alt":"x"}`, `{"percentage":300,"alt":"x"}`, `{"class":[1],"alt":"x"}`, `{"class":{},"alt":"x"}`} {
		p := parseCustomOutput(bad)
		if p.alt != nil || p.text != nil || p.percentage != nil {
			t.Errorf("%s: reserved fields = %+v, want none", bad, p)
		}
		if p.json == nil {
			t.Errorf("%s: the JSON was dropped", bad)
		}
	}
	// An array is JSON without reserved fields; the context is only
	// the output.
	p := parseCustomOutput(`[1, 2]`)
	if p.json == nil || len(p.templateContext()) != 1 {
		t.Errorf("array = %+v", p)
	}
}

func TestParseCustomOutputBrokenOrHugeJSONFallsBack(t *testing.T) {
	if p := parseCustomOutput("{oops"); p.json != nil || p.raw != "{oops" {
		t.Errorf("broken = %+v", p)
	}
	huge := `{"text":"` + strings.Repeat("x", maxCustomJSONBytes) + `"}`
	if p := parseCustomOutput(huge); p.json != nil || p.text != nil {
		t.Error("JSON over 64 KiB was parsed")
	}
}

func TestFormatCustomLabelTextWins(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	if got := formatCustomLabel(def, parseCustomOutput(`{"text":"override"}`)); got != "override" {
		t.Errorf("= %q, want the text field", got)
	}
	plain := parseCustomOutput("42%")
	if got := formatCustomLabel(def, plain); got != "42%" {
		t.Errorf("= %q, want the raw output", got)
	}
	def.Format = "{{ state | upper }}: {{ output }} {{ n + 1 }}"
	if got := formatCustomLabel(def, parseCustomOutput(`{"state":"on","n":1}`)); got != `ON: {"state":"on","n":1} 2` {
		t.Errorf("= %q, want the rendered template", got)
	}
	def.Format = "{{ missing }}x"
	if got := formatCustomLabel(def, plain); got != "x" {
		t.Errorf("= %q, want undefined to render empty", got)
	}
	def.Format = "{{ missing.attr }}x"
	if got := formatCustomLabel(def, plain); got != "" {
		t.Errorf("= %q, want a failed render to show nothing", got)
	}
}

func TestResolveCustomIconPriority(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	def.IconName = "static"
	names := []string{"low", "mid", "high"}
	icons := map[string]string{"muted": "mute-icon", "default": "default-icon"}
	def.IconNames, def.IconMap = &names, &icons
	for out, want := range map[string]string{
		`{"alt":"muted","percentage":90}`: "mute-icon",
		`{"alt":"other","percentage":0}`:  "low",
		`{"percentage":50}`:               "mid",
		`{"percentage":100}`:              "high",
		`{"alt":"other"}`:                 "default-icon",
	} {
		if got := resolveCustomIcon(def, parseCustomOutput(out)); got != want {
			t.Errorf("%s = %q, want %q", out, got, want)
		}
	}
	def.IconMap = nil
	if got := resolveCustomIcon(def, parseCustomOutput("x")); got != "static" {
		t.Errorf("fallback = %q", got)
	}
}

func TestResolveCustomColorsAndClasses(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	if _, ok := resolveCustomColors(def, parseCustomOutput(`{"alt":"x"}`)); ok {
		t.Error("colors without a color-map")
	}
	red, _ := config.ParseColorValue("red")
	blue, _ := config.ParseColorValue("blue")
	cm := map[string]config.StateColors{"hot": {LabelColor: &red}, "default": {IconColor: &blue}}
	def.ColorMap = &cm
	hot, _ := resolveCustomColors(def, parseCustomOutput(`{"alt":"hot"}`))
	if *hot.LabelColor != red || *hot.IconColor != def.IconColor {
		t.Errorf("hot = %+v", hot)
	}
	cold, _ := resolveCustomColors(def, parseCustomOutput(`{"alt":"cold"}`))
	if *cold.IconColor != blue || *cold.LabelColor != def.LabelColor {
		t.Errorf("default state = %+v", cold)
	}
	format := "state-{{ alt }} warn"
	def.ClassFormat = &format
	if got := resolveCustomClasses(def, parseCustomOutput(`{"alt":"hot","class":["warn","x"]}`)); !slices.Equal(got, []string{"warn", "x", "state-hot"}) {
		t.Errorf("classes = %v", got)
	}
}

func TestFormatCustomTooltip(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	if _, ok := formatCustomTooltip(def, parseCustomOutput("x")); ok {
		t.Error("a tooltip without tooltip-format or JSON tooltip")
	}
	tf := "out: {{ output }}"
	def.TooltipFormat = &tf
	if tip, ok := formatCustomTooltip(def, parseCustomOutput("x")); !ok || tip != "out: x" {
		t.Errorf("tooltip-format = %q %v", tip, ok)
	}
	if tip, _ := formatCustomTooltip(def, parseCustomOutput(`{"tooltip":"json"}`)); tip != "json" {
		t.Errorf("JSON tooltip = %q", tip)
	}
}

// newTestCustom builds the module without the loop: commands run and
// their output applies through the headless Invoke.
func newTestCustom(t *testing.T, def config.CustomModuleDefinition) *customModule {
	t.Helper()
	ctx := newTestContext(t, config.Defaults())
	ctx.CustomUpdates = newCustomUpdates()
	ctx.gen = newMountGen()
	t.Cleanup(ctx.gen.retire)
	m := &customModule{ctx: ctx, def: def}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = widget.NewThemeIcon("x", 16)
	m.life, m.stop = context.WithCancel(ctx.Life())
	t.Cleanup(m.Stop)
	ctx.CustomUpdates.register(m)
	m.setButton(newBarButton(ctx, m.icon, m.label))
	return m
}

func TestCustomCommandOutputApplies(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	def.Id = "t"
	m := newTestCustom(t, def)
	m.runCommand(`printf '{"text":"hi","class":"c1"}'; exit 3`)
	// A nonzero exit still delivers its output (tokio output()).
	waitForText(t, m.label, "hi")
	waitHeadless(t, "the JSON class", func() bool { return m.btn.HasClass("c1") })
	m.runCommand(`printf plain`)
	waitForText(t, m.label, "plain")
	waitHeadless(t, "the old class to go", func() bool { return !m.btn.HasClass("c1") })
	// The output outlives the module: a rebuilt one starts from it.
	if got := m.lastOutput(); got != "plain" {
		t.Errorf("remembered = %q", got)
	}
}

func TestCustomHideIfEmptyHidesTheButton(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	def.HideIfEmpty = true
	m := newTestCustom(t, def)
	m.apply("false")
	if m.btn.Visible() {
		t.Error("false output left the button visible")
	}
	m.apply("1")
	if !m.btn.Visible() {
		t.Error("1 output left the button hidden")
	}
}

func TestCustomOnActionFollowsShellBindingsOnly(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	onAction := "printf done"
	def.OnAction = &onAction
	m := newTestCustom(t, def)
	m.followAction(config.ClickAction{Kind: config.ClickDropdown, Dropdown: "x"}, false)
	time.Sleep(50 * time.Millisecond)
	if got := onHeadlessLoop(m.label.Text); got == "done" {
		t.Fatal("a dropdown binding ran on-action")
	}
	m.followAction(config.ClickAction{Kind: config.ClickShell, Command: "true"}, true)
	m.followAction(config.ClickAction{Kind: config.ClickShell, Command: "true"}, true)
	waitForText(t, m.label, "done")
}

func TestCustomUpdatesReachEveryInstance(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	def.Id = "shared"
	a := newTestCustom(t, def)
	b := newTestCustom(t, def)
	b.ctx.CustomUpdates = a.ctx.CustomUpdates
	a.ctx.CustomUpdates.register(b)
	a.ctx.CustomUpdates.dispatch("shared", "pushed")
	if a.label.Text() != "pushed" || b.label.Text() != "pushed" {
		t.Errorf("a = %q, b = %q; both bars show the push", a.label.Text(), b.label.Text())
	}
	a.ctx.CustomUpdates.dispatch("other", "nope")
	if a.label.Text() != "pushed" {
		t.Error("an update for another id applied")
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
	def := config.DefaultsCustomModuleDefinition()
	def.Id = "cpu-temp"
	cmd := "echo 42"
	def.Command = &cmd
	cfg.Custom = []config.CustomModuleDefinition{def}

	got, ok := customDefinition("custom-cpu-temp", cfg)
	if !ok || got.Id != "cpu-temp" {
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
	if len(c.Custom) != 1 || c.Custom[0].Id != "x" || c.Custom[0].IntervalMs != 100 || !c.Custom[0].HideIfEmpty {
		t.Errorf("custom = %+v", c.Custom)
	}
	for _, bad := range []string{
		"[[modules.custom]]\ncommand = \"echo hi\"\n",
		"[[modules.custom]]\nid = \"a\"\nmode = \"stream\"\n",
		"[[modules.custom]]\nid = \"a\"\ninterval-ms = -5\n",
		"[[modules.custom]]\nid = \"a\"\nlabel-max-length = -1\n",
	} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}

func TestCustomLabelMaxLengthCapsTheButton(t *testing.T) {
	def := config.DefaultsCustomModuleDefinition()
	def.LabelMaxLength = 3
	parsed := parseCustomOutput("abcdef")
	if got := formatCustomLabel(def, parsed); got != "abcdef" {
		t.Errorf("label text = %q, want it whole (the button caps the width)", got)
	}
	if got := def.Button().LabelMaxLength; got != 3 {
		t.Errorf("button label-max-length = %d, want the definition's 3", got)
	}
}
