package bar

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/shell/widgets"
)

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
