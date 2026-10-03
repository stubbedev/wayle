package bar

import (
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/shell/widgets"
)

// brightnessDeviceIcon is DEVICE_ICON.
const brightnessDeviceIcon = "ld-sun-symbolic"

// friendlyDeviceName is friendly_device_name: a display name for a raw
// sysfs backlight identifier.
func friendlyDeviceName(raw string, kind brightness.Type) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "kbd") || strings.Contains(lower, "keyboard"):
		return i18n.T("dropdown-brightness-device-keyboard")
	case strings.HasPrefix(lower, "ddcci") || strings.Contains(lower, "ddc") || strings.Contains(lower, "i2c"):
		return i18n.T("dropdown-brightness-device-external")
	case strings.Contains(lower, "backlight"),
		strings.HasPrefix(lower, "intel_"),
		strings.HasPrefix(lower, "amdgpu"),
		strings.HasPrefix(lower, "acpi_video"),
		strings.HasPrefix(lower, "nvidia"),
		kind == brightness.TypeFirmware || kind == brightness.TypePlatform:
		return i18n.T("dropdown-brightness-device-internal")
	}
	return prettifyIdentifier(raw)
}

// prettifyIdentifier is prettify: the words between '_', '-', and ' ',
// each with its first rune uppercased, joined by spaces.
func prettifyIdentifier(raw string) string {
	words := strings.FieldsFunc(raw, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, w := range words {
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[size:]
	}
	return strings.Join(words, " ")
}

// backlightTypeLabel is backlight_type_label.
func backlightTypeLabel(kind brightness.Type) string {
	switch kind {
	case brightness.TypeDDC:
		return "external"
	case brightness.TypePlatform:
		return "platform"
	case brightness.TypeFirmware:
		return "firmware"
	}
	return "raw"
}

// deviceSubtitle is device_subtitle: "name · type", only when several
// devices are listed.
func deviceSubtitle(name string, kind brightness.Type, multi bool) (string, bool) {
	if !multi {
		return "", false
	}
	return name + " · " + backlightTypeLabel(kind), true
}

// brightnessView is the brightness dropdown: a header, then a slider
// card per backlight or the empty state. The device list rebuilds only
// when the set of devices changes; a level change moves the slider in
// place (sync_single_device), which the slider ignores mid-drag.
type brightnessView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	body    *widget.Stack
	list    *widget.Box
	devices []brightness.Device
	sliders map[string]*widgets.DebouncedSlider

	once   sync.Once
	cancel context.CancelFunc
}

func brightnessDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &brightnessView{ctx: ctx, font: font, px: px, cancel: func() {}}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "brightness-dropdown")
	v.Append(dropdownHeader(font, px, brightnessDeviceIcon, i18n.T("dropdown-brightness-title")), false)
	v.list = widget.NewBox(widget.Column, 10, 0)
	v.list.AddClass("brightness-devices")
	empty := emptyState(font, px, brightnessDeviceIcon, i18n.T("dropdown-brightness-empty-title"), i18n.T("dropdown-brightness-empty-description"))
	empty.AddClass("brightness-empty")
	v.body = widget.NewStack()
	v.body.Add("devices", dropdownScroll(v.list, ""))
	v.body.Add("empty", empty)
	v.Append(v.body, true)
	v.apply(v.read(context.Background()))
	v.follow()
	return v
}

// read snapshots the devices; a failed enumeration is no devices.
func (v *brightnessView) read(ctx context.Context) []brightness.Device {
	if v.ctx.Brightness == nil {
		return nil
	}
	devices, err := v.ctx.Brightness.Devices(ctx)
	if err != nil {
		log.Printf("brightness: %v", err)
		return nil
	}
	return devices
}

// apply shows a device snapshot: a rebuild when the names or types
// changed (sync_devices), otherwise the levels pushed into the sliders.
func (v *brightnessView) apply(devices []brightness.Device) {
	same := slices.EqualFunc(devices, v.devices, func(a, b brightness.Device) bool {
		return a.Name == b.Name && a.Type == b.Type
	})
	v.devices = devices
	if same && v.sliders != nil {
		for _, d := range devices {
			v.sliders[d.Name].Set(d.Percentage())
		}
		return
	}
	v.list.Clear()
	v.sliders = make(map[string]*widgets.DebouncedSlider, len(devices))
	if len(devices) == 0 {
		v.body.Show("empty")
		return
	}
	multi := len(devices) > 1
	for _, d := range devices {
		v.list.Append(v.item(d, multi), false)
	}
	v.body.Show("devices")
}

// item is BrightnessDeviceItem: the icon, the name over the optional
// subtitle, and the labeled slider committing to the device.
func (v *brightnessView) item(d brightness.Device, multi bool) widget.Widget {
	card := widget.NewBox(widget.Column, 6, 0)
	card.AddClass("brightness-device")
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("brightness-device-header")
	icon := widget.NewThemeIcon(brightnessDeviceIcon, int(v.px*1.2))
	icon.SetTint(v.ctx.Style.fg)
	icon.AddClass("brightness-device-icon")
	header.Append(icon, false)
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("brightness-device-info")
	title := widget.NewLabel(v.font, v.px, friendlyDeviceName(d.Name, d.Type), v.ctx.Style.fg)
	title.AddClass("brightness-device-name")
	title.SetEllipsize(widget.EllipsizeEnd)
	info.Append(title, false)
	if sub, ok := deviceSubtitle(d.Name, d.Type, multi); ok {
		meta := widget.NewLabel(v.font, v.px*0.85, sub, tokenColor(v.ctx.Style.palette, config.TokenFgMuted))
		meta.AddClass("brightness-device-meta")
		meta.SetEllipsize(widget.EllipsizeEnd)
		info.Append(meta, false)
	}
	header.Append(info, true)
	card.Append(header, false)

	slider := widgets.NewDebouncedSlider(d.Percentage(), v.font, v.px*0.9, v.ctx.Style.fg, v.ctx.Invoke)
	slider.AddClass("brightness-slider-row")
	slider.Knob.AddClass("brightness-slider")
	name := d.Name
	slider.OnCommit = func(pct float64) { v.commit(name, pct) }
	v.sliders[name] = slider
	card.Append(slider, false)
	return card
}

// commit is commit_brightness: the write runs off the loop, as the
// Rust command future does.
func (v *brightnessView) commit(name string, pct float64) {
	src := v.ctx.Brightness
	go func() {
		if err := src.Set(context.Background(), name, pct); err != nil {
			log.Printf("brightness: set %s: %v", name, err)
		}
	}()
}

// follow re-reads the devices on every service tick until the
// dropdown closes.
func (v *brightnessView) follow() {
	if v.ctx.Brightness == nil {
		return
	}
	life, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	followTicks(v.ctx, life, "brightness", v.ctx.Brightness.Subscribe, v.read, v.apply)
}

// dropdownClosed implements dropdownCloser.
func (v *brightnessView) dropdownClosed() { v.once.Do(func() { v.cancel() }) }
