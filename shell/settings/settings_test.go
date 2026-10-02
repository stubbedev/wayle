package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// testKit is a kit over a config directory holding configTOML, its
// loop invoke running inline.
func testKit(t *testing.T, configTOML string) *kit {
	t.Helper()
	dir := t.TempDir()
	if configTOML != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(configTOML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	svc := config.Load(dir, config.DiscardDiagnostics)
	t.Cleanup(svc.Close)
	return &kit{face: face, store: store{svc}, invoke: func(fn func()) { fn() }}
}

// runtimeFile is runtime.toml's content.
func runtimeFile(t *testing.T, k *kit) string {
	t.Helper()
	b, err := os.ReadFile(k.store.svc.RuntimePath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestSourceBadges(t *testing.T) {
	k := testKit(t, "[bar]\ndropdown-shadow = false\ndropdown-autohide = true\n")
	t.Run("default shows nothing", func(t *testing.T) {
		if got := sourceOf(k.store, "bar.dropdown-freeze-label"); got != (sourceInfo{}) {
			t.Errorf("default = %+v, want no badge", got)
		}
	})
	t.Run("a config value off the default is config", func(t *testing.T) {
		got := sourceOf(k.store, "bar.dropdown-shadow")
		if !got.badge || got.class != "info" || got.reset || got.label != i18n.Settings().Get("settings-source-config") {
			t.Errorf("config = %+v, want the info badge and no reset", got)
		}
	})
	t.Run("a config value equal to the default shows nothing", func(t *testing.T) {
		if d, _ := config.DefaultValue("bar.dropdown-autohide"); d != true {
			t.Fatalf("test premise: dropdown-autohide defaults to %v", d)
		}
		if got := sourceOf(k.store, "bar.dropdown-autohide"); got.badge || got.reset {
			t.Errorf("config at default = %+v, want no badge", got)
		}
	})
	t.Run("a runtime value alone is custom, over config an override", func(t *testing.T) {
		_ = k.store.set("bar.dropdown-freeze-label", true)
		if got := sourceOf(k.store, "bar.dropdown-freeze-label"); got.class != "success" || !got.reset || !got.badge {
			t.Errorf("runtime only = %+v, want the custom badge with reset", got)
		}
		_ = k.store.set("bar.dropdown-shadow", true)
		got := sourceOf(k.store, "bar.dropdown-shadow")
		if got.class != "warning" || !got.reset || got.tooltip != i18n.Settings().Attr("settings-source-override", "description") {
			t.Errorf("runtime over config = %+v, want the override badge with reset", got)
		}
	})
}

func TestToggleWritesAndPersistsAndRefreshesSilently(t *testing.T) {
	k := testKit(t, "")
	c := newToggle(k, "bar.dropdown-shadow")
	if !c.On() {
		t.Fatal("the switch does not show the default (on)")
	}
	c.SetOn(false)
	if v := k.store.value("bar.dropdown-shadow"); v != false {
		t.Errorf("value after toggling = %v, want false", v)
	}
	if !strings.Contains(runtimeFile(t, k), "dropdown-shadow = false") {
		t.Errorf("runtime.toml = %q, want the override saved", runtimeFile(t, k))
	}
	// An external reset refreshes the switch without writing back.
	k.store.reset("bar.dropdown-shadow")
	c.refresh()
	if !c.On() || k.store.svc.Source("bar.dropdown-shadow") != config.SourceDefault {
		t.Error("a refresh wrote the value back as an override")
	}
}

func TestEnumSelectLabelsAndWrites(t *testing.T) {
	k := testKit(t, "")
	meta, _ := config.Field("osd.position")
	c := newEnumSelect(k, "osd.position", meta)
	if got, want := c.Selection(), i18n.Settings().Get("enum-osd-position-bottom"); got != want {
		t.Errorf("selection %q, want the default's label %q", got, want)
	}
	c.SetSelected(0)
	if v := k.store.value("osd.position"); v != "top-left" {
		t.Errorf("value = %v, want top-left", v)
	}
	// An enum Rust leaves unlabeled shows its raw values.
	vpn, _ := config.Field("modules.network.vpn-show")
	raw := newEnumSelect(k, "modules.network.vpn-show", vpn)
	if raw.Selection() != "auto" {
		t.Errorf("unlabeled selection %q, want the raw value", raw.Selection())
	}
	// A refresh does not write.
	k.store.reset("osd.position")
	c.refresh()
	if k.store.svc.Source("osd.position") != config.SourceDefault || c.Selected() != 5 {
		t.Errorf("refresh: source %v selected %d", k.store.svc.Source("osd.position"), c.Selected())
	}
}

func TestNumberWritesWholeNumbersAndClamps(t *testing.T) {
	k := testKit(t, "")
	meta, _ := config.Field("osd.duration")
	c, err := autoEditor(k, "osd.duration", meta)
	if err != nil {
		t.Fatal(err)
	}
	n := c.(*number)
	if n.Value() != 2500 {
		t.Fatalf("value %v, want the default 2500", n.Value())
	}
	n.KeyAction(widget.KeyUp, 0)
	if v := k.store.value("osd.duration"); v != int64(2501) {
		t.Errorf("stored %#v, want int64 2501", v)
	}
	scale, _ := config.Field("styling.scale")
	s := newNumber(k, "styling.scale", scale.Min, scale.Max, 0.05, 2)
	s.SelectAll()
	s.Insert("9")
	s.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value("styling.scale"); v != float64(3) {
		t.Errorf("scale stored %#v, want clamped to 3", v)
	}
}

func TestTextCommitsOnEnterWithTheDirtyBadge(t *testing.T) {
	k := testKit(t, "")
	meta, _ := config.Field("osd.monitor")
	c, _ := autoEditor(k, "osd.monitor", meta)
	tx := c.(*text)
	if tx.Text() != "primary" || tx.badge.Visible() {
		t.Fatalf("initial %q badge %v", tx.Text(), tx.badge.Visible())
	}
	tx.SelectAll()
	tx.Insert("DP-1")
	if !tx.badge.Visible() || k.store.svc.Source("osd.monitor") != config.SourceDefault {
		t.Fatal("typing did not mark the entry unsaved, or wrote early")
	}
	tx.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value("osd.monitor"); v != "DP-1" || tx.badge.Visible() {
		t.Errorf("after Enter value %v badge %v", v, tx.badge.Visible())
	}
	// A value the field refuses keeps the badge up and writes nothing.
	bg := newText(k, "styling.palette.bg", false)
	bg.SelectAll()
	bg.Insert("#zz")
	bg.KeyAction(widget.KeyEnter, 0)
	if !bg.badge.Visible() || k.store.svc.Source("styling.palette.bg") != config.SourceDefault {
		t.Error("a refused commit cleared the unsaved badge or wrote")
	}
	// A refresh is not an edit.
	tx.refresh()
	if tx.badge.Visible() {
		t.Error("a refresh raised the unsaved badge")
	}
	// Clearing an optional drops its override.
	opt, _ := config.Field("modules.weather.visual-crossing-key")
	key := newText(k, "modules.weather.visual-crossing-key", opt.Optional)
	key.Insert("abc")
	key.KeyAction(widget.KeyEnter, 0)
	if k.store.svc.Source("modules.weather.visual-crossing-key") != config.SourceRuntime {
		t.Fatal("the key was not set")
	}
	key.SelectAll()
	key.Backspace()
	key.KeyAction(widget.KeyEnter, 0)
	if k.store.svc.Source("modules.weather.visual-crossing-key") != config.SourceDefault {
		t.Error("an emptied optional kept its override")
	}
}

func TestSliderCommitsPercentages(t *testing.T) {
	k := testKit(t, "")
	c := fieldControl(k, field("bar.dropdown-opacity", percentage)).(*slider)
	if c.Value() != 100 || c.Label().Text() != "100%" {
		t.Fatalf("slider %v %q, want the default 100%%", c.Value(), c.Label().Text())
	}
	c.Knob.SetValue(42.4)
	if v := k.store.value("bar.dropdown-opacity"); v != int64(42) {
		t.Errorf("stored %#v, want int64 42", v)
	}
}

func TestFieldControlRefusesWhatItCannotEdit(t *testing.T) {
	k := testKit(t, "")
	meta, _ := config.Field("osd.presets")
	if _, err := autoEditor(k, "osd.presets", meta); err == nil {
		t.Error("a list got an auto editor")
	}
	defer func() {
		if recover() == nil {
			t.Error("a row naming no field built")
		}
	}()
	fieldControl(k, field("osd.nope"))
}

func TestRowResetButtonFollowsTheOverride(t *testing.T) {
	k := testKit(t, "")
	spec := field("bar.dropdown-shadow")
	row := newSettingRow(k, spec, fieldControl(k, spec))
	if row.reset.Enabled() || row.badge.Visible() || !strings.Contains(row.reset.InlineStyle(), "opacity: 0") {
		t.Fatal("a default row shows a live reset or a badge")
	}
	row.ctl.(*toggle).SetOn(false)
	row.refresh()
	if !row.reset.Enabled() || !row.badge.Visible() || !row.badge.HasClass("success") || row.reset.InlineStyle() != "" {
		t.Fatal("an override did not arm the reset and the badge")
	}
	row.reset.ClickAt(widget.Point{})
	row.refresh()
	if row.reset.Enabled() || k.store.svc.Source("bar.dropdown-shadow") != config.SourceDefault || !row.ctl.(*toggle).On() {
		t.Error("reset did not clear the override and refresh the control")
	}
	if !strings.Contains(runtimeFile(t, k), "") || strings.Contains(runtimeFile(t, k), "dropdown-shadow") {
		t.Errorf("runtime.toml still holds the reset value: %q", runtimeFile(t, k))
	}
}

// Every page the layout lists is complete: its keys are in the
// settings FTL and every row builds.
func TestEveryPageBuilds(t *testing.T) {
	k := testKit(t, "")
	tr := i18n.Settings()
	ids := map[string]bool{}
	for _, s := range layout(k.store.svc.Config()) {
		if !tr.Has(s.key) {
			t.Errorf("section key %s missing", s.key)
		}
		for _, p := range s.pages {
			if ids[p.id] {
				t.Errorf("page id %s twice", p.id)
			}
			ids[p.id] = true
			for _, key := range []string{p.navKey, p.header} {
				if !tr.Has(key) {
					t.Errorf("%s: key %s missing", p.id, key)
				}
			}
			if strings.HasPrefix(tr.Attr(p.header, "breadcrumb"), "No localization") {
				t.Errorf("%s: no breadcrumb", p.id)
			}
			for _, sec := range p.sections {
				if !tr.Has(sec.title) {
					t.Errorf("%s: section title %s missing", p.id, sec.title)
				}
				for _, r := range sec.rows {
					if !tr.Has(r.labelKey()) {
						t.Errorf("%s: row %s label %s missing", p.id, r.path, r.labelKey())
					}
				}
			}
			if page := buildPage(k, p); len(page.rows) == 0 {
				t.Errorf("%s: no rows", p.id)
			}
		}
	}
	if len(ids) == 0 {
		t.Fatal("the layout lists no page")
	}
}

// fakeAfter queues the delayed calls for the test to run.
type fakeAfter struct{ queued []func() }

func (f *fakeAfter) after(_ time.Duration, fn func()) { f.queued = append(f.queued, fn) }

func (f *fakeAfter) run() {
	q := f.queued
	f.queued = nil
	for _, fn := range q {
		fn()
	}
}

func TestWindowNavigatesAndDropsOldPages(t *testing.T) {
	k := testKit(t, "")
	f := &fakeAfter{}
	w := newWindow(k, k.store.svc.Config(), f.after)
	first := w.pageID
	if first == "" || !w.sidebar.items[first].HasClass("active") {
		t.Fatalf("opened on %q, want the first page active", first)
	}
	// Two pages, so a switch has somewhere to go.
	w.sections = append(w.sections, navSection{key: "settings-nav-system", pages: []pageSpec{{
		id: "extra", navKey: "settings-nav-general", header: "settings-page-general",
		sections: []sectionSpec{{title: "settings-section-general", rows: fields("osd.enabled")}},
	}}})
	w.sidebar.items["extra"] = widget.NewButton(widget.NewSpacer(0, 0), 0, 0)
	w.sidebar.navigate("extra")
	if w.pageID != "extra" || !w.sidebar.items["extra"].HasClass("active") || w.sidebar.items[first].HasClass("active") {
		t.Fatalf("navigate: page %q", w.pageID)
	}
	if len(w.names) != 2 {
		t.Fatalf("stack pages %v, want both during the cross-fade", w.names)
	}
	// Back before the cleanup: the stale cleanups keep the current page.
	w.sidebar.navigate(first)
	f.run()
	if len(w.names) != 1 || w.stack.Visible() != w.names[0] || w.pageID != first {
		t.Errorf("after cleanup pages %v visible %q", w.names, w.stack.Visible())
	}
	// Selecting the shown page rebuilds nothing.
	page := w.page
	w.show(first)
	if w.page != page {
		t.Error("re-selecting the current page rebuilt it")
	}
}

func TestSidebarSectionsCollapse(t *testing.T) {
	k := testKit(t, "")
	s := newSidebar(k, layout(k.store.svc.Config()), "")
	key := "settings-nav-bar-section"
	s.toggleSection(key)
	if v := s.sections[key]; v.items.Visible() || !v.header.HasClass("collapsed") {
		t.Error("a toggled section stayed open")
	}
	s.toggleSection(key)
	if v := s.sections[key]; !v.items.Visible() || v.header.HasClass("collapsed") {
		t.Error("a second toggle did not reopen it")
	}
	s.toggleSection("nope") // unknown: ignored
}

func TestSidebarDragIsClampedToTheScaledLimit(t *testing.T) {
	k := testKit(t, "[styling]\nscale = 2.0\n")
	w := newWindow(k, k.store.svc.Config(), (&fakeAfter{}).after)
	if got := w.sidebarLimit(); got != 800 {
		t.Fatalf("limit = %d, want 25rem at scale 2 (800)", got)
	}
	frame := func() {
		w.paned.Measure(widget.Constraints{Max: widget.Size{W: 2000, H: 600}})
		w.paned.Arrange(render.Rect{W: 2000, H: 600})
	}
	frame()
	w.paned.SetPosition(1200)
	frame()
	if w.paned.Position() > 800 {
		t.Errorf("position %d past the limit", w.paned.Position())
	}
	w.paned.SetPosition(300)
	frame()
	if w.paned.Position() != 300 {
		t.Errorf("position %d, want 300 kept under the limit", w.paned.Position())
	}
	// A new scale moves the cap.
	if err := k.store.set("styling.scale", 1.0); err != nil {
		t.Fatal(err)
	}
	w.refresh()
	w.paned.SetPosition(1200)
	frame()
	if w.paned.Position() != 400 {
		t.Errorf("position %d after the scale fell to 1, want the 400 cap", w.paned.Position())
	}
}

func TestResetAllConfirmation(t *testing.T) {
	k := testKit(t, "")
	_ = k.store.set("osd.enabled", false)
	var got []string
	content := confirmContent(k, resetAllConfirm(), func(r string) { got = append(got, r) })
	var buttons []*widget.Button
	var walk func(widget.Widget)
	walk = func(w widget.Widget) {
		if b, ok := w.(*widget.Button); ok {
			buttons = append(buttons, b)
		}
		if c, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, kid := range c.Children() {
				walk(kid)
			}
		}
	}
	walk(content)
	if len(buttons) != 2 || !buttons[0].HasClass("secondary") || !buttons[1].HasClass("danger") {
		t.Fatalf("buttons %v, want cancel then the danger confirm", buttons)
	}
	buttons[0].ClickAt(widget.Point{})
	buttons[1].ClickAt(widget.Point{})
	if len(got) != 2 || got[0] != responseCancel || got[1] != responseConfirm {
		t.Errorf("responses %v", got)
	}
	k.store.resetAll()
	if k.store.svc.Source("osd.enabled") != config.SourceDefault {
		t.Error("reset-all kept an override")
	}
	if _, err := os.Stat(k.store.svc.RuntimePath()); !os.IsNotExist(err) {
		t.Errorf("runtime.toml survived reset-all: %v", err)
	}
}
