package printdialog

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/internal/ipp"
)

func TestPageRanges(t *testing.T) {
	for in, want := range map[string]string{
		"1-5, 8": "0-4,7-7",
		"3":      "2-2",
		"5-2":    "1-4", // reversed reads as written forwards
		"0":      "0-0", // page numbers start at 1
		" 2 ,,4": "1-1,3-3",
	} {
		rs, ok := pageRanges(in)
		if !ok || gtkRanges(rs) != want {
			t.Errorf("pageRanges(%q) = %v %v, want %s", in, rs, ok, want)
		}
	}
	for _, in := range []string{"", "   ", "abc", "1-x", "-3", "1-2-3", "99999999999"} {
		if rs, ok := pageRanges(in); ok {
			t.Errorf("pageRanges(%q) = %v, want all pages", in, rs)
		}
	}
	if rs, _ := pageRanges("1-5, 8"); !reflect.DeepEqual(rs, [][2]int32{{1, 5}, {8, 8}}) {
		t.Errorf("1-based ranges = %v", rs)
	}
}

func TestFormSettings(t *testing.T) {
	f := form{copies: 2, pages: "1-3", landscape: true, paper: 1, grayscale: true, duplex: 2, quality: 1}
	want := []Setting{
		{"printer", "office"},
		{"n-copies", "2"},
		{"orientation", "landscape"},
		{"paper-format", "na_letter"},
		{"paper-width", "215.9"},
		{"paper-height", "279.4"},
		{"use-color", "false"},
		{"duplex", "vertical"},
		{"quality", "draft"},
		{"print-pages", "ranges"},
		{"page-ranges", "0-2"},
	}
	if got := f.settings("office"); !slices.Equal(got, want) {
		t.Errorf("settings =\n %v\nwant\n %v", got, want)
	}
	plain := form{copies: 1}.settings("x")
	if !slices.Contains(plain, Setting{"print-pages", "all"}) || !slices.Contains(plain, Setting{"paper-format", "iso_a4"}) ||
		!slices.Contains(plain, Setting{"use-color", "true"}) || !slices.Contains(plain, Setting{"orientation", "portrait"}) {
		t.Errorf("defaults = %v", plain)
	}
	if slices.ContainsFunc(plain, func(s Setting) bool { return s.Key == "page-ranges" }) {
		t.Error("all pages carried page-ranges")
	}
	if got := (form{paper: 99}).paperOf(); got.gtk != "iso_a4" {
		t.Errorf("an out-of-range paper = %s, want A4", got.gtk)
	}
}

func TestFormJobAttrs(t *testing.T) {
	g := ipp.Group{Attrs: form{copies: 3, pages: "2", landscape: true, paper: 3, grayscale: true, duplex: 1, quality: 2}.jobAttrs()}
	if c, _ := g.Int("copies"); c != 3 {
		t.Errorf("copies = %d", c)
	}
	if o, _ := g.Int("orientation-requested"); o != 4 {
		t.Errorf("orientation = %d, want landscape 4", o)
	}
	for name, want := range map[string]string{"media": "iso_a3_297x420mm", "print-color-mode": "monochrome", "sides": "two-sided-long-edge"} {
		if got := g.Text(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if q, _ := g.Int("print-quality"); q != 5 {
		t.Errorf("quality = %d, want high 5", q)
	}
	if a, ok := g.Get("page-ranges"); !ok || a.Values[0].Low != 2 || a.Values[0].Int != 2 {
		t.Errorf("page-ranges = %+v", a)
	}
	plain := ipp.Group{Attrs: form{copies: 1}.jobAttrs()}
	if _, ok := plain.Get("page-ranges"); ok || plain.Text("sides") != "one-sided" || plain.Text("print-color-mode") != "color" {
		t.Errorf("defaults = %+v", plain)
	}
	if q, _ := plain.Int("print-quality"); q != 4 {
		t.Errorf("normal quality = %d, want 4", q)
	}
}

func TestPrinterStatusAndDetail(t *testing.T) {
	for p, want := range map[ipp.Printer]string{
		{State: ipp.StateStopped, Accepting: true}:                             "Paused",
		{State: ipp.StateIdle, Accepting: false}:                               "Not accepting jobs",
		{State: ipp.StateProcessing, Accepting: true, StateMessage: "Warming"}: "Warming",
		{State: ipp.StateIdle, Accepting: true}:                                "Ready",
	} {
		if got := printerStatus(p); got != want {
			t.Errorf("printerStatus(%+v) = %q, want %q", p, got, want)
		}
	}
	if got := printerDetail("Hall", "Ready"); got != "Hall · Ready" {
		t.Errorf("detail = %q", got)
	}
	if got := printerDetail("", "Ready"); got != "Ready" {
		t.Errorf("no location = %q", got)
	}
}

func TestFileOutput(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if got := FileOutput(); got != filepath.Join(home, "output.pdf") {
		t.Errorf("no documents dir = %s", got)
	}
	if err := os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte("XDG_DOCUMENTS_DIR=\"$HOME/Docs\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := FileOutput(); got != filepath.Join(home, "Docs", "output.pdf") {
		t.Errorf("documents dir = %s", got)
	}
}
