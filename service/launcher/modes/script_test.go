package modes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stubbedev/wayle/service/launcher"
)

func TestScriptParsesHeadersAndRows(t *testing.T) {
	p := parseOutput("\x00prompt\x1fpick\n\x00message\x1fhello\n\x00markup-rows\x1ftrue\nfirst\nsecond\x00icon\x1ffirefox\x1finfo\x1fpayload\n")
	if p.prompt == nil || *p.prompt != "pick" || p.message == nil || *p.message != "hello" || !p.markupRows {
		t.Fatalf("headers = %+v", p)
	}
	if len(p.rows) != 2 || p.rows[0].text != "first" {
		t.Fatalf("rows = %+v", p.rows)
	}
	if p.rows[1].item.Icon != launcher.IconName("firefox") || p.rows[1].item.Info == nil || *p.rows[1].item.Info != "payload" {
		t.Errorf("row options = %+v", p.rows[1].item)
	}
}

func TestRowOptionsMetaDisplayNonselectable(t *testing.T) {
	r := parseRow("entry\x00display\x1fPretty\x1fmeta\x1fhidden words\x1fnonselectable\x1ftrue")
	if r.text != "entry" || r.item.Display != "Pretty" || r.item.MatchText != "entry hidden words" ||
		!r.item.Flags.Has(launcher.FlagNonselectable) {
		t.Errorf("row = %+v", r)
	}
	// A false flag value is not set.
	if parseRow("x\x00urgent\x1ffalse").item.Flags.Has(launcher.FlagUrgent) {
		t.Error("urgent=false must not flag the row")
	}
}

func TestIconValues(t *testing.T) {
	if got := ParseIcon("/usr/share/x.png"); got != launcher.IconFile("/usr/share/x.png") {
		t.Errorf("path = %#v", got)
	}
	if got := ParseIcon("firefox"); got != launcher.IconName("firefox") {
		t.Errorf("name = %#v", got)
	}
	th, ok := ParseIcon("thumbnail:///tmp/a.png").(launcher.IconThumbnail)
	if !ok || th.Path != "/tmp/a.png" || th.Fallback == "" {
		t.Errorf("thumbnail = %#v", th)
	}
	// Comma-separated values are fallbacks; the first wins.
	if parseRow("x\x00icon\x1fa,b").item.Icon != launcher.IconName("a") {
		t.Error("the first icon of a list must win")
	}
}

func TestRangesExpand(t *testing.T) {
	if got := ParseRanges("0,2-4"); !reflect.DeepEqual(got, []uint32{0, 2, 3, 4}) {
		t.Errorf("got %v", got)
	}
	if got := ParseRanges("1,3-5,8"); !reflect.DeepEqual(got, []uint32{1, 3, 4, 5, 8}) {
		t.Errorf("got %v", got)
	}
	if got := ParseRanges("x,5-3, 7"); !reflect.DeepEqual(got, []uint32{7}) {
		t.Errorf("junk and reversed ranges are skipped: %v", got)
	}
}

func TestDataAndSwitchModeHeaders(t *testing.T) {
	p := parseOutput("\x00data\x1fstate123\n\x00switch-mode\x1fdrun\nrow\n")
	if p.data == nil || *p.data != "state123" || p.switchMode == nil || *p.switchMode != "drun" {
		t.Errorf("headers = %+v", p)
	}
}

func TestACustomDelimiter(t *testing.T) {
	// The header line ends in its own delimiter so the first row does
	// not start with the newline (script.rs splits the same way).
	p := parseOutput("\x00delim\x1f|\n|a|b|c")
	var texts []string
	for _, r := range p.rows {
		texts = append(texts, r.text)
	}
	if !reflect.DeepEqual(texts, []string{"a", "b", "c"}) {
		t.Errorf("rows = %q", texts)
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mode.sh")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScriptRoundTrip(t *testing.T) {
	ctx := context.Background()
	script := writeScript(t, "#!/bin/sh\nif [ \"$ROFI_RETV\" = 0 ]; then\n  printf '\\0prompt\\x1ftest\\n'\n  echo one\n  echo two\nelif [ \"$1\" = one ]; then\n  echo picked-one\nfi\n")
	m := NewScript("test", script)
	st := m.Load(ctx)
	if st.Prompt != "test" || len(st.Items) != 2 {
		t.Fatalf("load = %+v", st)
	}
	r, ok := m.Activate(ctx, launcher.RowTarget(0), launcher.ActivateDefault{}, "").(launcher.ActionReload)
	if !ok || r.State.Items[0].Display != "picked-one" {
		t.Fatalf("selecting one = %#v", r)
	}
	m = NewScript("test", script)
	m.Load(ctx)
	if _, ok := m.Activate(ctx, launcher.RowTarget(1), launcher.ActivateDefault{}, "").(launcher.ActionClose); !ok {
		t.Error("no output after a selection closes")
	}
}

func TestScriptEnvironmentAndHotKeys(t *testing.T) {
	ctx := context.Background()
	script := writeScript(t, "#!/bin/sh\nif [ \"$ROFI_RETV\" = 0 ]; then\n  printf '\\0data\\x1fsaved\\n'\n  printf 'row\\0info\\x1fI\\n'\nelse\n  echo \"$ROFI_RETV:$1:$ROFI_INFO:$ROFI_DATA:$ROFI_INPUT\"\nfi\n")
	m := NewScript("env", script)
	m.Load(ctx)
	// kb-custom without use-hot-keys does nothing.
	if _, ok := m.Activate(ctx, launcher.RowTarget(0), launcher.ActivateKbCustom{N: 1}, "q").(launcher.ActionNothing); !ok {
		t.Error("kb-custom must be inert without use-hot-keys")
	}
	r, ok := m.Activate(ctx, launcher.RowTarget(0), launcher.ActivateDefault{}, "q").(launcher.ActionReload)
	if !ok || r.State.Items[0].Display != "1:row:I:saved:q" {
		t.Errorf("env = %#v", r)
	}
	m2 := NewScript("env", script)
	m2.Load(ctx)
	r, _ = m2.Activate(ctx, launcher.NoRow, launcher.ActivateCustom{Text: "typed"}, "typed").(launcher.ActionReload)
	if r.State.Items[0].Display != "2:typed::saved:typed" {
		t.Errorf("custom = %q", r.State.Items[0].Display)
	}
	m3 := NewScript("env", script)
	m3.Load(ctx)
	r, _ = m3.Delete(ctx, 0).(launcher.ActionReload)
	if r.State.Items[0].Display != "3:row:I:saved:" {
		t.Errorf("delete = %q", r.State.Items[0].Display)
	}
}

func TestAMissingScriptIsAnEmptyList(t *testing.T) {
	m := NewScript("gone", filepath.Join(t.TempDir(), "nope"))
	st := m.Load(context.Background())
	if len(st.Items) != 0 || st.Prompt != "gone" {
		t.Errorf("state = %+v", st)
	}
}
