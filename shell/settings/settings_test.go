package settings

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	c := newToggle(k, pathSlot(k.store, "bar.dropdown-shadow"))
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
	c := newEnumSelect(k, pathSlot(k.store, "osd.position"), meta)
	if got, want := c.Selection(), i18n.Settings().Get("enum-osd-position-bottom"); got != want {
		t.Errorf("selection %q, want the default's label %q", got, want)
	}
	c.SetSelected(0)
	if v := k.store.value("osd.position"); v != "top-left" {
		t.Errorf("value = %v, want top-left", v)
	}
	// An enum Rust leaves unlabeled shows its raw values.
	vpn, _ := config.Field("modules.network.vpn-show")
	raw := newEnumSelect(k, pathSlot(k.store, "modules.network.vpn-show"), vpn)
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
	c, err := autoEditor(k, pathSlot(k.store, "osd.duration"), meta)
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
	s := newNumber(k, pathSlot(k.store, "styling.scale"), scale.Min, scale.Max, 0.05, 2)
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
	c, _ := autoEditor(k, pathSlot(k.store, "osd.monitor"), meta)
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
	bg := newText(k, pathSlot(k.store, "styling.palette.bg"), false)
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
	key := newText(k, pathSlot(k.store, "modules.weather.visual-crossing-key"), opt.Optional)
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
	if _, err := autoEditor(k, pathSlot(k.store, "osd.presets"), meta); err == nil {
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
	c := newOptionalEnum(k, pathSlot(k.store, "animations.enter"), meta)
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
	c := newOptionalNumber(k, pathSlot(k.store, "animations.enter-duration"), 0, maxDurationMS, durationStepMS, durationFallbackMS)
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
	c := newSizeEditor(k, pathSlot(k.store, "osd.margin"), config.OsdMarginBaseRem)
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
	c := newFontEditor(k, pathSlot(k.store, "general.font-sans"))
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
	c := newColorEditor(k, pathSlot(k.store, "lock.background-color"))
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
	c := newFileEditor(k, pathSlot(k.store, "lock.background-image"))
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

func TestColorValueEditor(t *testing.T) {
	for _, tok := range config.CssTokens() {
		if _, ok := cssTokenMeta[tok]; !ok {
			t.Errorf("token %s has no label", tok)
		}
	}
	k := testKit(t, "")
	path := "modules.clock.icon-color"
	if m, ok := config.Field(path); !ok || m.Type != "ColorValue" {
		t.Fatalf("test premise: %s is %+v", path, m)
	}
	c := fieldControl(k, field(path)).(*colorValueEditor)
	idx := func(id string) int { return slices.Index(c.ids, id) }
	cur, _ := k.store.value(path).(string)
	if c.drop.Selected() != idx(cur) || c.swatch.Visible() {
		t.Fatalf("shows row %d (swatch %v) for %q", c.drop.Selected(), c.swatch.Visible(), cur)
	}
	c.drop.SetSelected(idx("red"))
	if v := k.store.value(path); v != "red" {
		t.Errorf("token pick wrote %v", v)
	}
	c.drop.SetSelected(idx(colorCustom))
	if v := k.store.value(path); v != "#ffffff" || !c.swatch.Visible() {
		t.Errorf("custom wrote %v (swatch %v), want white and the swatch", v, c.swatch.Visible())
	}
	_ = k.store.set(path, "#123456")
	c.refresh()
	if c.drop.Selected() != idx(colorCustom) || !c.swatch.Visible() {
		t.Error("a hex value does not show as custom")
	}
	c.drop.SetSelected(idx(colorTransparent))
	c.drop.SetSelected(idx(colorCustom))
	if v := k.store.value(path); v != "#ffffff" {
		t.Errorf("custom after a keyword wrote %v, want white", v)
	}
	before := k.store.value(path)
	c.picked(slices.Index(c.ids, ""))
	if k.store.value(path) != before {
		t.Error("a header row wrote")
	}
}

func TestIconEditorPicks(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	prev := iconNames
	iconNames = func() []string { return []string{"ld-battery-symbolic", "ld-bell-symbolic", "tb-cpu-symbolic"} }
	t.Cleanup(func() { iconNames = prev })
	path := "modules.clock.icon-name"
	c := newIconEditor(k, pathSlot(k.store, path))
	if cur, _ := k.store.value(path).(string); c.label.Text() != cur || c.icon.Name() != cur {
		t.Fatalf("trigger shows %q/%q, want %q", c.label.Text(), c.icon.Name(), cur)
	}
	c.btn.ClickAt(widget.Point{})
	p := c.picker
	if p == nil || f.content != widget.Widget(p.root) {
		t.Fatal("no picker opened")
	}
	p.search.Insert("LD-B")
	if len(p.shown) != 2 {
		t.Errorf("filter kept %v, want the two ld-b icons", p.shown)
	}
	p.list.OnActivate(1)
	if v := k.store.value(path); v != "ld-bell-symbolic" || f.closed != 1 {
		t.Errorf("pick wrote %v (closed %d)", v, f.closed)
	}
	// Enter picks the typed name; the clear button picks none.
	p.search.SelectAll()
	p.search.Insert("custom-icon")
	p.search.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value(path); v != "custom-icon" {
		t.Errorf("Enter wrote %v", v)
	}
	p.pick("")
	c.refresh()
	if c.label.Text() != i18n.Settings().Get("settings-icon-none") || c.icon.Name() != "ld-image-symbolic" {
		t.Errorf("cleared trigger shows %q/%q", c.label.Text(), c.icon.Name())
	}
}

func TestActionEditor(t *testing.T) {
	k := testKit(t, "")
	path := "modules.volume.left-click"
	choices := actionChoices("volume")
	_ = k.store.set(path, "")
	c := newActionEditor(k, pathSlot(k.store, path), choices)
	if c.drop.Selected() != c.noneIndex() || c.reveal.Revealed() {
		t.Fatalf("an empty action shows %d (entry %v), want None", c.drop.Selected(), c.reveal.Revealed())
	}
	c.drop.SetSelected(1)
	if v := k.store.value(path); v != choices[1].command {
		t.Errorf("preset wrote %v", v)
	}
	// Custom: the entry shows, nothing is written until typed, then
	// every edit writes.
	c.drop.SetSelected(c.customIndex())
	if !c.reveal.Revealed() || k.store.value(path) != choices[1].command {
		t.Fatalf("custom: entry %v, value %v", c.reveal.Revealed(), k.store.value(path))
	}
	c.entry.Insert("notify-send hi")
	if v := k.store.value(path); v != "notify-send hi" {
		t.Errorf("typed custom wrote %v", v)
	}
	c.refresh()
	if c.drop.Selected() != c.customIndex() || c.entry.Text() != "notify-send hi" {
		t.Error("a custom command does not refresh as custom")
	}
	// A refresh to a preset leaves custom mode; typing then writes nothing.
	_ = k.store.set(path, choices[0].command)
	c.refresh()
	if c.drop.Selected() != 0 || c.reveal.Revealed() || c.custom {
		t.Errorf("refresh to a preset shows %d (entry %v)", c.drop.Selected(), c.reveal.Revealed())
	}
	c.entry.Insert("x")
	if v := k.store.value(path); v != choices[0].command {
		t.Errorf("a hidden entry wrote %v", v)
	}
	// An unknown command opens as custom.
	_ = k.store.set(path, "my-script")
	if d := newActionEditor(k, pathSlot(k.store, path), choices); d.drop.Selected() != d.customIndex() || d.entry.Text() != "my-script" {
		t.Errorf("an unknown command opens at %d with %q", d.drop.Selected(), d.entry.Text())
	}
	c.drop.SetSelected(c.noneIndex())
	if v := k.store.value(path); v != "" {
		t.Errorf("None wrote %v", v)
	}
}

func TestStringListEditor(t *testing.T) {
	k := testKit(t, "")
	path := "modules.battery.level-icons"
	c := fieldControl(k, field(path, stringList)).(*listEditor)
	start, _ := k.store.value(path).([]any)
	if len(c.items) != len(start) || len(start) < 2 {
		t.Fatalf("%d rows for %v", len(c.items), start)
	}
	c.items[0].(textItem).Insert("x")
	got, _ := k.store.value(path).([]any)
	if got[0] != start[0].(string)+"x" {
		t.Errorf("an edit wrote %v", got)
	}
	c.move(c.items[0], 1)
	got, _ = k.store.value(path).([]any)
	if got[1] != start[0].(string)+"x" || got[0] != start[1] {
		t.Errorf("move down wrote %v", got)
	}
	c.move(c.items[len(c.items)-1], 1) // the last row cannot go down
	c.remove(c.items[0])
	if got, _ = k.store.value(path).([]any); len(got) != len(start)-1 {
		t.Errorf("remove left %v", got)
	}
	n := len(c.items)
	c.appendRow("")
	c.commit()
	if got, _ = k.store.value(path).([]any); len(got) != n+1 || got[n] != "" {
		t.Errorf("add wrote %v", got)
	}
	// An outside change rebuilds; a matching one keeps the rows.
	keep := c.items[0]
	c.refresh()
	if c.items[0] != keep {
		t.Error("an unchanged list rebuilt its rows")
	}
	_ = k.store.set(path, []any{"a"})
	c.refresh()
	if len(c.items) != 1 || c.items[0].itemValue() != "a" {
		t.Errorf("outside change gave %d rows", len(c.items))
	}
}

func TestEnumAndIconListItems(t *testing.T) {
	k := testKit(t, "")
	path := "modules.dashboard.user-session.actions"
	m, ok := config.Field(path)
	if !ok || m.Elem == nil || m.Elem.Kind != config.FieldEnum {
		t.Fatalf("test premise: %s is %+v", path, m)
	}
	c := fieldControl(k, field(path, enumList)).(*listEditor)
	c.appendRow(c.spec.blank)
	last := c.items[len(c.items)-1].(enumItem)
	last.SetSelected(2)
	got, _ := k.store.value(path).([]any)
	if got[len(got)-1] != m.Elem.Variants[2] {
		t.Errorf("enum row wrote %v", got[len(got)-1])
	}
	f := &fakePickers{}
	k.pickers = f
	prev := iconNames
	iconNames = func() []string { return []string{"ld-a-symbolic"} }
	t.Cleanup(func() { iconNames = prev })
	icons := fieldControl(k, field("modules.battery.level-icons", iconList)).(*listEditor)
	first := icons.items[0].(iconItem)
	first.btn.ClickAt(widget.Point{})
	first.picker.list.OnActivate(0)
	if got, _ := k.store.value("modules.battery.level-icons").([]any); got[0] != "ld-a-symbolic" {
		t.Errorf("icon row wrote %v", got)
	}
	if first.label.Text() != "ld-a-symbolic" {
		t.Errorf("icon row shows %q after the pick", first.label.Text())
	}
}

func TestThresholdCards(t *testing.T) {
	k := testKit(t, "")
	path := "modules.battery.thresholds"
	_ = k.store.set(path, []any{})
	c := fieldControl(k, field(path, thresholdList)).(*cardList)
	if len(c.cards) != 0 {
		t.Fatalf("%d cards for an empty list", len(c.cards))
	}
	// Add a card, bound it below 20 and color its icon.
	c.local = append(c.local, c.spec.blank())
	c.commit()
	c.rebuild()
	if len(c.cards) != 1 || c.cards[0].title.Text() != i18n.Settings().Get("settings-threshold-card-title")+" 1" {
		t.Fatalf("new card titled %q", c.cards[0].title.Text())
	}
	below := c.cards[0].controls[1].(*optionalNumber)
	below.on.SetOn(true)
	below.spin.SetValue(20)
	below.spin.KeyAction(widget.KeyUp, 0)
	items, _ := k.store.value(path).([]any)
	if m, _ := items[0].(map[string]any); m["below"] != float64(21) {
		t.Fatalf("below wrote %#v", items[0])
	}
	if got := c.cards[0].title.Text(); got != "≤ 21" {
		t.Errorf("title %q, want ≤ 21", got)
	}
	icon := c.cards[0].controls[2].(*colorValueEditor)
	if icon.drop.Selected() != slices.Index(icon.ids, colorAuto) {
		t.Error("an unset color does not show as auto")
	}
	icon.drop.SetSelected(slices.Index(icon.ids, "red"))
	items, _ = k.store.value(path).([]any)
	if m, _ := items[0].(map[string]any); m["icon-color"] != "red" || m["below"] != float64(21) {
		t.Errorf("icon color wrote %#v", items[0])
	}
	below.on.SetOn(false)
	items, _ = k.store.value(path).([]any)
	if m, _ := items[0].(map[string]any); m["below"] != nil {
		t.Errorf("the override switch kept below: %#v", m)
	}
	// An outside change of a field refreshes in place; of the count, rebuilds.
	keep := c.cards[0]
	_ = k.store.set(path, []any{map[string]any{"above": 80.0}})
	c.refresh()
	if c.cards[0] != keep || c.cards[0].title.Text() != "≥ 80" {
		t.Errorf("in-place refresh: title %q", c.cards[0].title.Text())
	}
	_ = k.store.set(path, []any{map[string]any{}, map[string]any{}})
	c.refresh()
	if len(c.cards) != 2 {
		t.Errorf("%d cards after an outside add", len(c.cards))
	}
}

func TestStringMapEditor(t *testing.T) {
	k := testKit(t, "")
	path := "modules.window-title.icon-mappings"
	_ = k.store.set(path, map[string]any{"b": "2", "a": "1"})
	c := fieldControl(k, field(path, stringMap)).(*listEditor)
	if len(c.items) != 2 || c.items[0].itemValue() != [2]string{"a", "1"} {
		t.Fatalf("rows %v, want the pairs sorted by key", c.values())
	}
	// A new row's typing never reorders or rebuilds the rows.
	c.appendRow(c.spec.blank)
	row := c.items[2].(mapItem)
	row.key.Insert("0")
	c.refresh()
	if len(c.items) != 3 || c.items[2] != row {
		t.Fatal("typing a new key rebuilt the rows")
	}
	row.value.Insert("zero")
	got, _ := k.store.value(path).(map[string]any)
	if got["0"] != "zero" || got["a"] != "1" || len(got) != 3 {
		t.Errorf("wrote %v", got)
	}
	// An empty key is dropped on write.
	c.appendRow(c.spec.blank)
	c.items[3].(mapItem).value.Insert("orphan")
	if got, _ := k.store.value(path).(map[string]any); len(got) != 3 {
		t.Errorf("an empty key was written: %v", got)
	}
	_ = k.store.set(path, map[string]any{"z": "26"})
	c.refresh()
	if len(c.items) != 1 || c.items[0].itemValue() != [2]string{"z", "26"} {
		t.Errorf("outside change gave %v", c.values())
	}
}

func TestMountPointsText(t *testing.T) {
	k := testKit(t, "")
	path := "modules.storage.mount-point"
	c := fieldControl(k, field(path, mountPoints)).(*text)
	for typed, want := range map[string]any{"/home": "/home", " /, /home ,": []any{"/", "/home"}, "  ": "/"} {
		c.SelectAll()
		c.Insert(typed)
		c.KeyAction(widget.KeyEnter, 0)
		if got := k.store.value(path); !reflect.DeepEqual(got, want) {
			t.Errorf("%q wrote %#v, want %#v", typed, got, want)
		}
	}
	_ = k.store.set(path, []any{"/a", "/b"})
	c.refresh()
	if c.Text() != "/a, /b" {
		t.Errorf("a list shows %q", c.Text())
	}
}

func TestOptionalSize(t *testing.T) {
	k := testKit(t, "")
	path := "dropdowns.audio.width"
	c := fieldControl(k, dropdownSizeRows("dropdowns.audio")[0]).(*optionalSize)
	if c.on.On() || c.size.Enabled() || k.store.value(path) != nil {
		t.Fatal("an unset width shows an override")
	}
	c.on.SetOn(true)
	if v := k.store.value(path); v != float64(1) || !c.size.Enabled() {
		t.Errorf("override on wrote %#v", v)
	}
	c.size.mode.SetSelected(sizePx)
	if v := k.store.value(path); v != "16px" {
		t.Errorf("px mode wrote %#v, want 16px (a scale of 1 at the 1rem base)", v)
	}
	c.on.SetOn(false)
	if k.store.value(path) != nil || c.size.Enabled() {
		t.Error("override off kept the width")
	}
	_ = k.store.set(path, "300px")
	c.refresh()
	if !c.on.On() || c.size.mode.Selected() != sizePx || c.size.spin.Value() != 300 {
		t.Errorf("refresh from 300px: on %v mode %d value %v", c.on.On(), c.size.mode.Selected(), c.size.spin.Value())
	}
}

func TestToastPresetCards(t *testing.T) {
	k := testKit(t, "")
	path := "osd.presets"
	c := fieldControl(k, field(path, toastPresetList)).(*cardList)
	c.local = append(c.local, c.spec.blank())
	c.commit()
	c.rebuild()
	if got, _ := k.store.value(path).([]any); len(got) != 0 || len(c.cards) != 1 {
		t.Fatalf("an id-less preset was stored (%v) or lost (%d cards)", got, len(c.cards))
	}
	id := c.cards[0].identity.(*liveText)
	id.Insert("vol")
	got, _ := k.store.value(path).([]any)
	if len(got) != 1 || got[0].(map[string]any)["id"] != "vol" {
		t.Fatalf("typing the id stored %v", got)
	}
	if _, has := got[0].(map[string]any)["label"]; has {
		t.Error("an empty label was stored")
	}
	label := c.cards[0].controls[0].(*liveText)
	label.Insert("Volume")
	got, _ = k.store.value(path).([]any)
	if got[0].(map[string]any)["label"] != "Volume" {
		t.Errorf("label stored %v", got)
	}
	// A refresh with the preset stored keeps the cards in place.
	keep := c.cards[0]
	c.refresh()
	if c.cards[0] != keep {
		t.Error("a refresh rebuilt the cards")
	}
	// A second preset with the same id is flagged, and both stored.
	c.local = append(c.local, map[string]any{"id": "vol"})
	c.commit()
	c.rebuild()
	if !c.cards[0].identity.(*liveText).HasClass("error") || !c.cards[1].identity.(*liveText).HasClass("error") {
		t.Error("duplicate ids are not flagged")
	}
	c.cards[1].identity.(*liveText).Insert("2")
	if c.cards[0].identity.(*liveText).HasClass("error") {
		t.Error("a fixed duplicate stays flagged")
	}
}

func TestThemeSelectorApplies(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	themes := config.BuiltinThemes()
	if len(themes) < 2 {
		t.Fatal("test premise: built-in themes")
	}
	spec := themePreset()
	row := newSettingRow(k, spec, fieldControl(k, spec))
	c := row.ctl.(*themeSelector)
	if c.badge.Visible() {
		t.Error("no base theme shows a badge")
	}
	c.btn.ClickAt(widget.Point{})
	if len(c.picker.all) != len(themes) {
		t.Fatalf("picker lists %d themes, want the %d built-ins", len(c.picker.all), len(themes))
	}
	pick := themes[1]
	c.picker.list.OnActivate(1)
	if v := k.store.value("styling.palette_base_theme"); v != pick.Name {
		t.Errorf("base theme %v, want %s", v, pick.Name)
	}
	if v := k.store.value("styling.palette.primary"); !strings.EqualFold(v.(string), pick.Palette.Primary) {
		t.Errorf("primary %v, want %s", v, pick.Palette.Primary)
	}
	if !c.badge.Visible() || c.badge.Text() != pick.Name || f.closed != 1 {
		t.Errorf("badge %q visible %v, closed %d", c.badge.Text(), c.badge.Visible(), f.closed)
	}
	// An action row shows no source badge or reset, even overridden.
	row.refresh()
	if row.badge.Visible() || row.reset.Enabled() {
		t.Error("the action row shows its source")
	}
}

func TestCyclingRevealFollowsTheDirectory(t *testing.T) {
	k := testKit(t, "")
	spec := cyclingReveal("wallpaper.cycling-directory",
		field("wallpaper.cycling-mode"),
		field("wallpaper.cycling-interval-mins", spin(1, 1440, 1, 0)),
		field("wallpaper.cycling-same-image"),
	)
	row := newSettingRow(k, spec, fieldControl(k, spec))
	g := row.ctl.(*revealGroup)
	if g.Revealed() || g.Progress() != 0 {
		t.Fatalf("no directory: revealed %v at %v, want hidden at once", g.Revealed(), g.Progress())
	}
	if len(g.rows) != 3 || !g.Collapse() || g.Transition() != widget.RevealSlideDown {
		t.Errorf("group: %d rows, collapse %v, transition %v", len(g.rows), g.Collapse(), g.Transition())
	}
	if row.badge.Visible() || row.reset.Enabled() {
		t.Error("the action row shows a source badge or reset")
	}
	if err := k.store.set("wallpaper.cycling-directory", "/walls"); err != nil {
		t.Fatal(err)
	}
	row.refresh()
	if !g.Revealed() {
		t.Error("setting the directory did not reveal the options")
	}
	if row.badge.Visible() {
		t.Error("the directory's override badged the action row")
	}
	// The nested rows edit their own fields.
	interval := g.rows[1].ctl.(*number)
	interval.KeyAction(widget.KeyUp, 0)
	if v := k.store.value("wallpaper.cycling-interval-mins"); v != int64(16) {
		t.Errorf("interval stored %v (%T), want 16", v, v)
	}
	row.refresh()
	if !g.rows[1].badge.Visible() {
		t.Error("a nested row's override shows no badge")
	}
	if err := k.store.set("wallpaper.cycling-directory", ""); err != nil {
		t.Fatal(err)
	}
	row.refresh()
	if g.Revealed() {
		t.Error("clearing the directory left the options revealed")
	}

	// A directory already set shows the options at once.
	k2 := testKit(t, "[wallpaper]\ncycling-directory = \"/walls\"\n")
	g2 := fieldControl(k2, spec).(*revealGroup)
	if !g2.Revealed() || g2.Progress() != 1 {
		t.Errorf("set directory: revealed %v at %v, want shown at once", g2.Revealed(), g2.Progress())
	}
}

func TestMonitorWallpaperCards(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	path := "wallpaper.monitors"
	c := fieldControl(k, field(path, monitorWallpaperList)).(*cardList)
	if len(c.cards) != 0 {
		t.Fatalf("%d cards for no monitors", len(c.cards))
	}
	c.local = append(c.local, c.spec.blank())
	c.commit()
	c.rebuild()
	got, _ := k.store.value(path).([]any)
	if len(got) != 1 || got[0].(map[string]any)["fit-mode"] != "fill" || got[0].(map[string]any)["name"] != "" {
		t.Fatalf("a new monitor stored %v, want a blank fill entry", got)
	}
	card := c.list.Children()[0].(*widget.Box)
	if !card.HasClass("monitor-card") || card.HasClass("card-form-card") {
		t.Error("the monitor card lacks its own chrome")
	}
	header := card.Children()[0].(*widget.Box)
	if l := header.Children()[0].(*widget.Label); !l.HasClass("monitor-card-label") || l.HasClass("card-form-label") {
		t.Error("the identity label lacks the list's label class")
	}
	remove := header.Children()[len(header.Children())-1].(*widget.Button)
	if !remove.HasClass("ghost-icon") || remove.HasClass("list-control-remove") || remove.TooltipText() != "" {
		t.Errorf("remove button classes/tooltip %q are card_form's", remove.TooltipText())
	}

	c.cards[0].identity.(*liveText).Insert("DP-1")
	body := c.cards[0].controls[0].(*monitorBody)
	body.path.Insert("/a.png")
	body.fit.SetSelected(slices.Index(body.fit.variants, "fit"))
	got, _ = k.store.value(path).([]any)
	if m := got[0].(map[string]any); m["name"] != "DP-1" || m["wallpaper"] != "/a.png" || m["fit-mode"] != "fit" {
		t.Fatalf("editing the card stored %v", m)
	}
	cfg := k.store.svc.Config()
	if mon, ok := cfg.Wallpaper.Monitor("DP-1"); !ok || mon.FitMode != config.FitFit || mon.Wallpaper != "/a.png" {
		t.Errorf("config monitor = %+v (%v)", mon, ok)
	}
	body.browse.ClickAt(widget.Point{})
	f.fileFn("/b.png")
	if m := k.store.value(path).([]any)[0].(map[string]any); m["wallpaper"] != "/b.png" || body.path.Text() != "/b.png" {
		t.Errorf("browse stored %v, entry %q", m["wallpaper"], body.path.Text())
	}
	keep := c.cards[0]
	if err := k.store.set(path, []any{map[string]any{"name": "DP-1", "wallpaper": "/c.png", "fit-mode": "stretch"}}); err != nil {
		t.Fatal(err)
	}
	c.refresh()
	if c.cards[0] != keep {
		t.Error("a refresh rebuilt the cards")
	}
	if body.fit.variants[body.fit.Selected()] != "stretch" || body.path.Text() != "/c.png" {
		t.Errorf("refresh shows fit %q path %q, want the stored stretch /c.png", body.fit.Selection(), body.path.Text())
	}
	// The body takes the row's spare width, not its label.
	bodyRow := c.list.Children()[0].(*widget.Box).Children()[1].(*widget.Box).Children()[0].(*widget.Box)
	bodyRow.Measure(widget.Constraints{Max: widget.Size{W: 600, H: 400}})
	bodyRow.Arrange(render.Rect{W: 600, H: 40})
	if label := bodyRow.Children()[0].(*widget.Label).Bounds(); label.W > 200 || body.Bounds().W < 400 {
		t.Errorf("label %v and body %v share the row; the body should fill it", label, body.Bounds())
	}
	add := c.Children()[1].(*widget.Button)
	if add.TooltipText() != i18n.Settings().Get("settings-monitor-add") {
		t.Errorf("add tooltip %q, want the monitor's", add.TooltipText())
	}
	remove = c.list.Children()[0].(*widget.Box).Children()[0].(*widget.Box).Children()[2].(*widget.Button)
	remove.ClickAt(widget.Point{})
	if got, _ := k.store.value(path).([]any); len(got) != 0 || len(c.cards) != 0 {
		t.Errorf("remove left %v (%d cards)", got, len(c.cards))
	}

	// card_form lists keep their own chrome.
	th := fieldControl(k, field("modules.cpu.thresholds", thresholdList)).(*cardList)
	th.local = append(th.local, th.spec.blank())
	th.rebuild()
	if !th.list.Children()[0].(*widget.Box).HasClass("card-form-card") {
		t.Error("a threshold card lost card_form's chrome")
	}
}

func TestEntriesKeepTheirGtkWidth(t *testing.T) {
	k := testKit(t, "")
	con := widget.Constraints{Max: widget.Size{W: 2000, H: 100}}
	file := newFileEditor(k, pathSlot(k.store, "lock.background-image"))
	live := newLiveText(k, pathSlot(k.store, "wallpaper.wallpaper"), "")
	for name, e := range map[string]*widget.Entry{"file": file.entry.Entry, "live": live.Entry} {
		empty := e.Measure(con).W
		if empty < widget.GTKTextWidth {
			t.Errorf("%s: an empty entry measures %d, want GTK's %d text width", name, empty, widget.GTKTextWidth)
		}
		e.SetText(strings.Repeat("/long/path", 40))
		if got := e.Measure(con).W; got != empty {
			t.Errorf("%s: a long text resized the entry from %d to %d", name, empty, got)
		}
	}
}

// addButton is a list editor's add button.
func addButton(c *listEditor) *widget.Button { return c.Children()[1].(*widget.Button) }

func TestListAddWritesOnlyAChange(t *testing.T) {
	k := testKit(t, "")
	m := fieldControl(k, field("modules.window-title.icon-mappings", stringMap)).(*listEditor)
	if m.rows.HasClass("string-list") || !m.rows.HasClass("string-map") {
		t.Error("the map's rows lack string_map's class")
	}
	addButton(m).ClickAt(widget.Point{})
	if len(m.items) != 1 || k.store.svc.Source("modules.window-title.icon-mappings") != config.SourceDefault {
		t.Errorf("a blank pair wrote the map (%d rows, source %v)", len(m.items), k.store.svc.Source("modules.window-title.icon-mappings"))
	}
	l := fieldControl(k, field("modules.notifications.blocklist", stringList)).(*listEditor)
	if !l.rows.HasClass("string-list") {
		t.Error("a list lost string-list")
	}
	addButton(l).ClickAt(widget.Point{})
	if got, _ := k.store.value("modules.notifications.blocklist").([]any); len(got) != 1 {
		t.Errorf("a blank list item stored %v, want it written", got)
	}
}

func TestWorkspaceStyleMapNumericKeys(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	path := "modules.hyprland-workspaces.workspace-map"
	_ = k.store.set(path, map[string]any{
		"10": map[string]any{"label": "ten"},
		"2":  map[string]any{"icon": "ld-globe-symbolic", "color": "#4a90d9"},
		"-1": map[string]any{},
	})
	c := fieldControl(k, field(path, workspaceStyleMap(true))).(*listEditor)
	var keys []string
	for _, it := range c.items {
		keys = append(keys, it.itemValue().(workspaceStyle).key)
	}
	if !slices.Equal(keys, []string{"-1", "2", "10"}) {
		t.Fatalf("rows %v, want numeric order", keys)
	}
	two := c.items[1].(workspaceStyleItem)
	if two.icon.label.Text() != "ld-globe-symbolic" || two.label.Text() != "" {
		t.Errorf("row 2 shows icon %q label %q", two.icon.label.Text(), two.label.Text())
	}
	// Retyping a key trims it; a key that is no number drops.
	two.key.SelectAll()
	two.key.Insert(" 3 ")
	got, _ := k.store.value(path).(map[string]any)
	if _, ok := got["3"]; !ok || len(got) != 3 {
		t.Fatalf("retyped key stored %v", got)
	}
	if s := got["3"].(map[string]any); s["icon"] != "ld-globe-symbolic" || s["color"] != "#4a90d9" {
		t.Errorf("the style moved with its key: %v", s)
	}
	c.items[0].(workspaceStyleItem).key.Insert("x")
	got, _ = k.store.value(path).(map[string]any)
	if _, ok := got["-1x"]; ok || len(got) != 2 {
		t.Errorf("a non-numeric key was stored: %v", got)
	}
	// The label writes only when set; the color through its picker.
	ten := c.items[2].(workspaceStyleItem)
	ten.label.SelectAll()
	ten.label.Backspace()
	got, _ = k.store.value(path).(map[string]any)
	if _, has := got["10"].(map[string]any)["label"]; has {
		t.Errorf("an empty label was stored: %v", got["10"])
	}
	ten.color.(*colorValueEditor).slot.set("#112233")
	got, _ = k.store.value(path).(map[string]any)
	if got["10"].(map[string]any)["color"] != "#112233" {
		t.Errorf("color stored %v", got["10"])
	}
}

func TestWorkspaceStyleMapNamedKeys(t *testing.T) {
	k := testKit(t, "")
	path := "modules.niri-workspaces.workspace-map"
	_ = k.store.set(path, map[string]any{"web": map[string]any{}, "chat": map[string]any{"label": "c"}})
	c := fieldControl(k, field(path, workspaceStyleMap(false))).(*listEditor)
	if c.items[0].itemValue().(workspaceStyle).key != "chat" {
		t.Errorf("first row %v, want names sorted", c.items[0].itemValue())
	}
	c.items[0].(workspaceStyleItem).key.SelectAll()
	c.items[0].(workspaceStyleItem).key.Backspace()
	got, _ := k.store.value(path).(map[string]any)
	if _, ok := got[""]; ok || len(got) != 1 {
		t.Errorf("an empty name was stored: %v", got)
	}
	// Named keys keep text that is no number.
	c.items[1].(workspaceStyleItem).key.Insert("2")
	got, _ = k.store.value(path).(map[string]any)
	if _, ok := got["web2"]; !ok {
		t.Errorf("named key not stored as typed: %v", got)
	}
}

func TestWorkspaceChoicesAreFocusActions(t *testing.T) {
	want := []config.WorkspaceClickKind{config.WorkspaceClickFocusThis, config.WorkspaceClickFocusNext, config.WorkspaceClickFocusPrevious, config.WorkspaceClickFocusLast}
	got := workspaceChoices()
	if len(got) != len(want) {
		t.Fatalf("%d choices", len(got))
	}
	for i, ch := range got {
		if a := config.ParseWorkspaceClickAction(ch.command); a.Kind != want[i] {
			t.Errorf("choice %q = %q parses as %v", ch.label, ch.command, a.Kind)
		}
	}
	if actionChoices("hyprland-workspaces") != nil {
		t.Error("the shell choices list the workspace modules")
	}
}

func TestActionCustomEntryCollapsesWhileHidden(t *testing.T) {
	k := testKit(t, "")
	c := newActionEditor(k, pathSlot(k.store, "modules.battery.left-click"), actionChoices("battery"))
	con := widget.Constraints{Max: widget.Size{W: 400, H: 400}}
	c.reveal.Finish()
	hidden := c.Measure(con).H
	c.drop.SetSelected(c.customIndex())
	c.reveal.Finish()
	c.InvalidateLayout()
	shown := c.Measure(con).H
	if shown <= hidden || hidden > c.drop.Measure(con).H+8 {
		t.Errorf("heights hidden %d shown %d: the hidden entry should take no space", hidden, shown)
	}
}

func TestModulePagesFollowTheRustOrder(t *testing.T) {
	var ids []string
	for _, p := range modulePages(&config.Config{}) {
		ids = append(ids, p.id)
	}
	want := []string{
		"battery", "bluetooth", "brightness", "cava", "clock", "cpu", "dashboard", "hyprland-workspaces",
		"hyprsunset", "idle-inhibit", "keybind-mode", "keyboard-input", "mango-workspaces", "media", "microphone",
		"netstat", "network", "niri-workspaces", "notification", "power", "power-profiles", "ram", "screenshot",
		"separator", "storage", "sway-workspaces", "treeman", "volume", "weather", "window-title", "world-clock",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("module pages\n%v\nwant (modules::factories, unported pages aside)\n%v", ids, want)
	}
}
