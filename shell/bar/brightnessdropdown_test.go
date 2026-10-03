package bar

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/shell/treetest"
	"github.com/stubbedev/wayle/shell/widgets"
)

// brightnessOne is the only widget of type T carrying class under
// root, failing the test otherwise.
func brightnessOne[T widget.Widget](t *testing.T, root widget.Widget, class string) T {
	t.Helper()
	for _, w := range treetest.All[T](root) {
		if widget.HasClass(w, class) {
			return w
		}
	}
	var zero T
	t.Fatalf("no %T with class %q under the tree", zero, class)
	return zero
}

// brightnessArrangeStyled attaches the bar stylesheet and lays the
// dropdown out at w, so the cascade's margins, paddings, and
// border-spacing take part in the bounds the assertions read.
func brightnessArrangeStyled(t *testing.T, ctx ModuleContext, v styledRoot, w int) {
	t.Helper()
	sz := v.Measure(widget.Constraints{Max: widget.Size{W: w, H: 1 << 14}})
	v.Arrange(render.Rect{W: w, H: max(sz.H, 1)})
	ctx.Theme.Attach(v)
	restore := widget.SetAnimationsInstant(true)
	t.Cleanup(restore)
	sz = v.Measure(widget.Constraints{Max: widget.Size{W: w, H: 1 << 14}})
	v.Arrange(render.Rect{W: max(sz.W, 1), H: max(sz.H, 1)})
}

// The device list sits bare in the stack — Rust has no
// ScrolledWindow; the panel grows instead.
func TestBrightnessListIsNotInAScroll(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	source := &fakeBrightnessSource{devices: []brightness.Device{
		{Name: "intel_backlight", Type: brightness.TypeFirmware, Brightness: 50, Max: 100},
	}}
	ctx.Brightness = source
	v := brightnessDropdown(ctx).(*brightnessView)
	defer v.dropdownClosed()
	if v.body.Visible() != "devices" {
		t.Fatalf("page %q, want devices", v.body.Visible())
	}
	if n := len(treetest.All[*widget.Scroll](v)); n != 0 {
		t.Errorf("%d scrolled windows in the tree; the Rust list is bare", n)
	}
	pages := treetest.WithClass(v, "brightness-devices")
	if len(pages) != 1 || pages[0] != widget.Widget(v.list) {
		t.Fatal("the devices page is not the classed list box itself")
	}
	if n := len(v.list.Children()); n != 1 {
		t.Errorf("the list holds %d items, want the one device", n)
	}
}

// The empty state follows mod.rs:96-111: the icon carries the sm size
// class, and the description wraps, centers, and caps at 32
// characters — only the description; the title stays as the template
// builds it.
func TestBrightnessEmptyStateIconAndDescription(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	ctx.Brightness = &fakeBrightnessSource{}
	v := brightnessDropdown(ctx).(*brightnessView)
	defer v.dropdownClosed()
	if v.body.Visible() != "empty" {
		t.Fatalf("page %q, want empty", v.body.Visible())
	}
	empty := treetest.WithClass(v, "brightness-empty")
	if len(empty) != 1 || !widget.HasClass(empty[0], "empty-state") {
		t.Fatalf("empty state classes = %v, want brightness-empty + empty-state",
			len(empty))
	}
	icon := brightnessOne[*widget.Icon](t, v, "icon")
	if !icon.HasClass("sm") {
		t.Error("the empty icon lacks the sm size class (icon-2xl)")
	}
	desc := brightnessOne[*widget.Label](t, v, "description")
	if !desc.Wrap() || desc.Alignment() != render.AlignCenter || desc.MaxWidthChars() != 32 {
		t.Errorf("description wrap=%v align=%v max=%d, want wrapped, centered, 32",
			desc.Wrap(), desc.Alignment(), desc.MaxWidthChars())
	}
	if title := brightnessOne[*widget.Label](t, v, "title"); title.Wrap() || title.MaxWidthChars() != 0 {
		t.Error("the title picked up the description's wrap tuning")
	}
}

// The device card keeps Rust's spacing-0 boxes — the header's gap is
// the cascade's border-spacing — and its children ride valign center
// at their natural heights.
func TestBrightnessDeviceHeaderSpacingAndValign(t *testing.T) {
	ctx := styledContext(t, config.Defaults())
	source := &fakeBrightnessSource{devices: []brightness.Device{
		{Name: "intel_backlight", Type: brightness.TypeFirmware, Brightness: 50, Max: 100},
		{Name: "ddcci5", Type: brightness.TypeDDC, Brightness: 10, Max: 100},
	}}
	ctx.Brightness = source
	v := brightnessDropdown(ctx).(*brightnessView)
	defer v.dropdownClosed()
	brightnessArrangeStyled(t, ctx, v, 320)

	if got := v.Spacing(); got != 0 {
		t.Errorf("root spacing = %d, want 0", got)
	}
	for _, class := range []string{
		"brightness-devices", "brightness-device", "brightness-device-header",
		"brightness-device-info",
	} {
		ws := treetest.WithClass(v, class)
		if len(ws) == 0 {
			t.Fatalf("no .%s in the tree", class)
		}
		for _, w := range ws {
			if got := w.(*widget.Box).Spacing(); got != 0 {
				t.Errorf(".%s spacing = %d, want 0 (CSS owns the gaps)", class, got)
			}
		}
	}

	header := brightnessOne[*widget.Box](t, v, "brightness-device-header")
	kids := header.Children()
	if len(kids) != 2 {
		t.Fatalf("header children = %d, want the tile and the info", len(kids))
	}
	hb, tile, info := header.Bounds(), alignedBounds(t, kids[0]), alignedBounds(t, kids[1])
	if tile.X+tile.W >= info.X {
		t.Errorf("no border-spacing gap between the icon tile and the info: %v then %v", tile, info)
	}
	mid := hb.Y + hb.H/2
	big := widget.Constraints{Max: widget.Size{W: 1 << 14, H: 1 << 14}}
	for name, k := range map[string]widget.Widget{"icon tile": kids[0], "info": kids[1]} {
		r := alignedBounds(t, k)
		if d := alignDelta(r.Y+r.H/2, mid); d > 1 {
			t.Errorf("%s midpoint off by %d (header mid %d)", name, d, mid)
		}
		// A plain append would stretch the child over the row; valign
		// center keeps its natural height.
		if nat := k.Measure(big).H; hb.H > nat+1 && r.H != nat {
			t.Errorf("%s stretched to %d in a %d header, want its natural %d",
				name, r.H, hb.H, nat)
		}
	}
}

func TestFriendlyDeviceName(t *testing.T) {
	internal := i18n.T("dropdown-brightness-device-internal")
	external := i18n.T("dropdown-brightness-device-external")
	keyboard := i18n.T("dropdown-brightness-device-keyboard")
	for _, tc := range []struct {
		raw  string
		kind brightness.Type
		want string
	}{
		{"intel_backlight", brightness.TypeRaw, internal},
		{"amdgpu_bl0", brightness.TypeRaw, internal},
		{"acpi_video0", brightness.TypeRaw, internal},
		{"nvidia_0", brightness.TypeRaw, internal},
		{"panel", brightness.TypeFirmware, internal},
		{"panel", brightness.TypePlatform, internal},
		{"ddcci5", brightness.TypeDDC, external},
		{"dev-i2c-3", brightness.TypeRaw, external},
		// The keyboard check runs before the backlight one.
		{"tpacpi::kbd_backlight", brightness.TypePlatform, keyboard},
		{"Keyboard", brightness.TypeRaw, keyboard},
		// Nothing matches: the prettified identifier, separators collapsed.
		{"my_panel-x  two", brightness.TypeRaw, "My Panel X Two"},
		{"élan", brightness.TypeRaw, "Élan"},
	} {
		if got := friendlyDeviceName(tc.raw, tc.kind); got != tc.want {
			t.Errorf("friendlyDeviceName(%q, %v) = %q, want %q", tc.raw, tc.kind, got, tc.want)
		}
	}
}

func TestDeviceSubtitleOnlyWithSeveralDevices(t *testing.T) {
	if sub, ok := deviceSubtitle("intel_backlight", brightness.TypeFirmware, false); ok || sub != "" {
		t.Errorf("single device subtitle = %q, %v; want none", sub, ok)
	}
	if sub, ok := deviceSubtitle("intel_backlight", brightness.TypeFirmware, true); !ok || sub != "intel_backlight · firmware" {
		t.Errorf("multi subtitle = %q, %v", sub, ok)
	}
	for kind, want := range map[brightness.Type]string{
		brightness.TypeRaw: "raw", brightness.TypePlatform: "platform",
		brightness.TypeFirmware: "firmware", brightness.TypeDDC: "external",
	} {
		if got := backlightTypeLabel(kind); got != want {
			t.Errorf("backlightTypeLabel(%v) = %q, want %q", kind, got, want)
		}
	}
}

// brightnessItemMeta reports whether a device card carries the
// subtitle line.
func brightnessItemMeta(card widget.Widget) bool {
	header := card.(*widget.Box).Children()[0].(*widget.Box)
	info := header.Children()[1].(*widget.Box)
	return len(info.Children()) == 2
}

func TestBrightnessDropdownEmptyWithoutDevices(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := brightnessDropdown(ctx).(*brightnessView)
	if v.body.Visible() != "empty" {
		t.Errorf("no service: page %q, want empty", v.body.Visible())
	}
	ctx.Brightness = &fakeBrightnessSource{}
	v = brightnessDropdown(ctx).(*brightnessView)
	defer v.dropdownClosed()
	if v.body.Visible() != "empty" || len(v.list.Children()) != 0 {
		t.Errorf("no devices: page %q, %d items", v.body.Visible(), len(v.list.Children()))
	}
}

func TestBrightnessDropdownItemsSubtitlesAndCommit(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	source := &fakeBrightnessSource{devices: []brightness.Device{
		{Name: "intel_backlight", Type: brightness.TypeFirmware, Brightness: 50, Max: 100},
	}}
	ctx.Brightness = source
	v := brightnessDropdown(ctx).(*brightnessView)
	defer v.dropdownClosed()
	if v.body.Visible() != "devices" || len(v.list.Children()) != 1 {
		t.Fatalf("page %q with %d items, want one device", v.body.Visible(), len(v.list.Children()))
	}
	if brightnessItemMeta(v.list.Children()[0]) {
		t.Error("a lone device shows a subtitle")
	}
	slider := v.sliders["intel_backlight"]
	if slider.Value() != 50 {
		t.Errorf("slider = %v, want 50", slider.Value())
	}
	slider.Knob.SetValue(70)
	waitHeadless(t, "the commit write", func() bool { return len(source.recorded()) == 1 })
	if got := source.recorded()[0]; got != (brightnessSet{"intel_backlight", 70}) {
		t.Errorf("set = %+v", got)
	}

	source.setDevices([]brightness.Device{
		{Name: "intel_backlight", Type: brightness.TypeFirmware, Brightness: 50, Max: 100},
		{Name: "ddcci5", Type: brightness.TypeDDC, Brightness: 10, Max: 100},
	})
	v.apply(v.read(t.Context()))
	if len(v.list.Children()) != 2 || !brightnessItemMeta(v.list.Children()[0]) {
		t.Error("two devices: want two items with subtitles")
	}
}

// A level change moves the existing slider (no rebuild); a changed
// device set rebuilds; the follow loop stops on close.
func TestBrightnessDropdownFollowsLevels(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	source := &fakeBrightnessSource{
		devices: []brightness.Device{{Name: "intel_backlight", Brightness: 20, Max: 100}},
		ticks:   make(chan struct{}, 1),
	}
	ctx.Brightness = source
	v := brightnessDropdown(ctx).(*brightnessView)
	slider := onHeadlessLoop(func() *widgets.DebouncedSlider { return v.sliders["intel_backlight"] })

	source.setDevices([]brightness.Device{{Name: "intel_backlight", Brightness: 80, Max: 100}})
	source.ticks <- struct{}{}
	waitHeadless(t, "the level update", func() bool { return slider.Value() == 80 })
	if onHeadlessLoop(func() *widgets.DebouncedSlider { return v.sliders["intel_backlight"] }) != slider {
		t.Error("a level change rebuilt the slider")
	}
	if len(source.recorded()) != 0 {
		t.Error("a backend level change wrote the device back")
	}

	source.setDevices(nil)
	source.ticks <- struct{}{}
	waitHeadless(t, "the empty page", func() bool { return v.body.Visible() == "empty" })

	onHeadlessLoop(func() bool { v.dropdownClosed(); return true })
	source.setDevices([]brightness.Device{{Name: "intel_backlight", Brightness: 5, Max: 100}})
	select {
	case source.ticks <- struct{}{}:
	default:
	}
	// A tick after close never lands: the page stays empty.
	time.Sleep(50 * time.Millisecond)
	if onHeadlessLoop(func() string { return v.body.Visible() }) != "empty" {
		t.Fatal("a tick after close rebuilt the closed dropdown")
	}
}
