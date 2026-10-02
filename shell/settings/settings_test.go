package settings

import (
	"errors"
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
	"github.com/stubbedev/wayle/styling"
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

// A field of a value struct writes, reads, unsets and resets through
// its leaf (Rust's field projections).
func TestValueStructFields(t *testing.T) {
	k := testKit(t, "[animations.osd]\nenter = \"zoom\"\n")
	if got := k.store.value("animations.osd.enter"); got != "zoom" {
		t.Fatalf("enter = %v, want the config's zoom", got)
	}
	if err := k.store.set("animations.osd.enter-duration", int64(300)); err != nil {
		t.Fatal(err)
	}
	if err := k.store.set("animations.osd.enter", "fade"); err != nil {
		t.Fatal(err)
	}
	rv, _ := k.store.svc.RuntimeValue("animations.osd")
	if m, _ := rv.(map[string]any); m["enter"] != "fade" || m["enter-duration"] != int64(300) {
		t.Fatalf("runtime leaf = %#v, want both fields", rv)
	}
	if src := sourceOf(k.store, "animations.osd.enter"); src.class != "warning" || !src.reset {
		t.Errorf("a set field over config = %+v, want the override badge", src)
	}
	// with_field_reset: the field takes the baseline's, the rest stays.
	k.store.reset("animations.osd.enter")
	if k.store.value("animations.osd.enter") != "zoom" || k.store.value("animations.osd.enter-duration") != int64(300) {
		t.Errorf("after reset enter %v duration %v, want zoom and 300 kept",
			k.store.value("animations.osd.enter"), k.store.value("animations.osd.enter-duration"))
	}
	k.store.unset("animations.osd.enter-duration")
	if v := k.store.value("animations.osd.enter-duration"); v != nil {
		t.Errorf("an unset field reads %v, want none", v)
	}
	if src := sourceOf(k.store, "animations.osd.enter-duration"); src.badge {
		t.Errorf("an unset field badges: %+v", src)
	}
	// A field reset with no runtime value writes nothing.
	k.store.reset("animations.lock.enter")
	if strings.Contains(runtimeFile(t, k), "animations.lock") {
		t.Error("a reset without an override wrote one")
	}
	if !strings.Contains(runtimeFile(t, k), "[animations.osd]") {
		t.Errorf("runtime.toml = %q, want the struct saved", runtimeFile(t, k))
	}
}

func TestOptionalEnumInheritsOrPicks(t *testing.T) {
	k := testKit(t, "")
	meta, _ := config.Field("animations.enter")
	c := newOptionalEnum(k, "animations.enter", meta)
	if c.Selected() != 0 || c.Selection() != i18n.Settings().Get("settings-inherit") {
		t.Fatalf("unset shows %q", c.Selection())
	}
	c.SetSelected(2)
	if v := k.store.value("animations.enter"); v != meta.Variants[1] {
		t.Errorf("picked %v, want %s", v, meta.Variants[1])
	}
	c.SetSelected(0)
	if k.store.svc.Source("animations.enter") != config.SourceDefault {
		t.Error("inherit kept the override")
	}
	_ = k.store.set("animations.enter", meta.Variants[3])
	c.refresh()
	if c.Selected() != 4 {
		t.Errorf("refresh selected %d, want the set variant (4)", c.Selected())
	}
}

func TestOptionalNumberOverrideSwitch(t *testing.T) {
	k := testKit(t, "")
	c := newOptionalNumber(k, "animations.enter-duration", 0, maxDurationMS, durationStepMS, 0, durationFallbackMS)
	if c.on.On() || c.spin.Enabled() {
		t.Fatal("an unset duration shows an active override")
	}
	c.on.SetOn(true)
	if v := k.store.value("animations.enter-duration"); v != int64(durationFallbackMS) || !c.spin.Enabled() {
		t.Fatalf("override on wrote %v (spin live %v), want the fallback", v, c.spin.Enabled())
	}
	c.spin.KeyAction(widget.KeyUp, 0)
	if v := k.store.value("animations.enter-duration"); v != int64(durationFallbackMS+durationStepMS) {
		t.Errorf("a step wrote %v", v)
	}
	c.on.SetOn(false)
	if k.store.svc.Source("animations.enter-duration") != config.SourceDefault || c.spin.Enabled() {
		t.Error("override off kept the value or the live spin")
	}
	_ = k.store.set("animations.enter-duration", int64(450))
	c.refresh()
	if !c.on.On() || c.spin.Value() != 450 || k.store.value("animations.enter-duration") != int64(450) {
		t.Errorf("refresh shows on=%v %v", c.on.On(), c.spin.Value())
	}
}

func TestSizeEditorModes(t *testing.T) {
	k := testKit(t, "")
	c := newSizeEditor(k, "osd.margin", config.OsdMarginBaseRem)
	if c.mode.Selected() != sizeScale || c.spin.Value() != 1 {
		t.Fatalf("default margin shows mode %d value %v, want scale 1", c.mode.Selected(), c.spin.Value())
	}
	c.mode.SetSelected(sizePx)
	if v := k.store.value("osd.margin"); v != "150px" || c.spin.Value() != 150 {
		t.Errorf("to px wrote %v (spin %v), want the base's 150px", v, c.spin.Value())
	}
	c.mode.SetSelected(sizeScale)
	if v := k.store.value("osd.margin"); v != float64(1) {
		t.Errorf("back to scale wrote %#v, want 1", v)
	}
	_ = k.store.set("osd.margin", "40px")
	c.refresh()
	if c.mode.Selected() != sizePx || c.spin.Value() != 40 {
		t.Errorf("refresh from 40px: mode %d value %v", c.mode.Selected(), c.spin.Value())
	}
	if k.store.value("osd.margin") != "40px" {
		t.Error("a refresh wrote")
	}
}

// fakePickers records what the editors asked for and answers on cue.
type fakePickers struct {
	anchor, content widget.Widget
	closed          int
	colorIn         render.Color
	colorFn         func(render.Color)
	fileFn          func(string)
}

func (f *fakePickers) popover(anchor, content widget.Widget) func() {
	f.anchor, f.content = anchor, content
	return func() { f.closed++ }
}

func (f *fakePickers) color(initial render.Color, fn func(render.Color)) {
	f.colorIn, f.colorFn = initial, fn
}

func (f *fakePickers) openFile(fn func(string)) { f.fileFn = fn }

func TestFontEditorPicksAFamily(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	prev := fontFamilies
	fontFamilies = func() ([]string, error) { return []string{"Cantarell", "DejaVu Sans", "Noto Sans"}, nil }
	t.Cleanup(func() { fontFamilies = prev })
	c := newFontEditor(k, "general.font-sans")
	if want, _ := k.store.value("general.font-sans").(string); c.label.Text() != want {
		t.Fatalf("label %q, want the configured family %q", c.label.Text(), want)
	}
	c.btn.ClickAt(widget.Point{})
	if f.anchor != widget.Widget(c.btn) || f.content == nil {
		t.Fatal("the button opened no picker")
	}
	picker := f.content.(*widget.Box)
	// The picker's search narrows the list case-insensitively.
	p := c.picker
	if p == nil || p.root != picker {
		t.Fatal("the picker tree is not the popover content")
	}
	// A new filter resets the list (rows, scroll and selection).
	p.list.Select(2)
	p.search.Insert("sans")
	if p.list.Selected() != -1 {
		t.Error("filtering kept the stale list")
	}
	if len(p.shown) != 2 || p.shown[0] != "DejaVu Sans" {
		t.Errorf("filtered %v, want the two Sans families", p.shown)
	}
	p.list.OnActivate(1)
	if v := k.store.value("general.font-sans"); v != "Noto Sans" || f.closed != 1 {
		t.Errorf("picked %v (closed %d), want Noto Sans and the popover closed", v, f.closed)
	}
	c.refresh()
	if c.label.Text() != "Noto Sans" {
		t.Errorf("label %q after the pick", c.label.Text())
	}
	p.search.SelectAll()
	p.search.Backspace()
	if len(p.shown) != 3 {
		t.Errorf("an empty search shows %d, want all", len(p.shown))
	}
}

func TestColorEditorWritesTheDialogsPick(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	c := newColorEditor(k, "lock.background-color")
	c.ClickAt(widget.Point{})
	if f.colorFn == nil {
		t.Fatal("no dialog opened")
	}
	cur, _ := k.store.value("lock.background-color").(string)
	if want, _ := styling.ParseHex(cur); f.colorIn != want {
		t.Errorf("dialog opened at %#08x, want the current %s", uint32(f.colorIn), cur)
	}
	f.colorFn(render.RGB(0x12, 0x34, 0x56))
	if v := k.store.value("lock.background-color"); v != "#123456" {
		t.Errorf("opaque pick wrote %v", v)
	}
	f.colorFn(render.RGBA(0xff, 0, 0, 0x80))
	if v := k.store.value("lock.background-color"); v != "#ff000080" {
		t.Errorf("translucent pick wrote %v, want #rrggbbaa", v)
	}
	c.refresh()
	if !strings.Contains(c.swatch.InlineStyle(), "#ff000080") {
		t.Errorf("swatch style %q", c.swatch.InlineStyle())
	}
}

func TestFileEditorBrowses(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	c := newFileEditor(k, "lock.background-image")
	c.entry.Insert("/typed")
	if !c.entry.badge.Visible() {
		t.Fatal("typing did not raise the unsaved badge")
	}
	c.browse()
	f.fileFn("/home/me/wall.png")
	if v := k.store.value("lock.background-image"); v != "/home/me/wall.png" || c.entry.badge.Visible() {
		t.Errorf("browse wrote %v (badge %v)", v, c.entry.badge.Visible())
	}
	c.refresh()
	if c.entry.Text() != "/home/me/wall.png" {
		t.Errorf("entry %q after the pick", c.entry.Text())
	}
}

func TestGreeterApplyStagesTheAllowedKeys(t *testing.T) {
	k := testKit(t, "")
	_ = k.store.set("greeter.cursor-size", int64(32))
	var staged string
	prev := spawnGreeterApply
	spawnGreeterApply = func(path string) error {
		b, err := os.ReadFile(path)
		staged = string(b)
		return err
	}
	t.Cleanup(func() { spawnGreeterApply = prev })
	if err := applyGreeter(k.store); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(staged, "[greeter]") || !strings.Contains(staged, "cursor-size = 32") || !strings.Contains(staged, "show-clock = ") {
		t.Errorf("staged:\n%s", staged)
	}
	if strings.Contains(staged, "cursor-theme-explicit") {
		t.Error("a key outside the allowlist was staged")
	}
	// A failed spawn shows on the button.
	spawnGreeterApply = func(string) error { return errors.New("no pkexec") }
	footer := greeterApplyFooter(k).(*widget.Box)
	var btn *widget.Button
	for _, kid := range footer.Children() {
		if b, ok := kid.(*widget.Button); ok {
			btn = b
		}
	}
	btn.ClickAt(widget.Point{})
	if !strings.Contains(widget.DumpTree(footer, nil), greeterApplyFailed) {
		t.Error("a failed apply did not show on the button")
	}
}
