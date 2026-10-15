package osd

import (
	"log"
	"math"
	"strconv"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/service/keytoggles"
)

// kind is OsdEvent's variant.
type kind uint8

const (
	kindSlider kind = iota
	kindToggle
	kindToast
)

// Event is one OSD flash (messages.rs OsdEvent): a slider (volume,
// microphone, brightness), a lock-key toggle, or a toast pushed over
// the widget socket. Build one with Slider, Toggle, or from a toast
// request.
type Event struct {
	kind  kind
	label string
	// icon is the theme icon; empty shows none (a toast without one).
	icon string
	// percent is the level; bar says the event shows it (every slider,
	// a toast with a percentage).
	percent float64
	bar     bool
	muted   bool
	active  bool
	// durationMS overrides osd.duration (a toast's --duration); 0
	// follows the config.
	durationMS uint32
	// class is a toast's extra CSS class.
	class string
}

// Slider is a level event: a device's volume or the brightness.
func Slider(label, icon string, percent float64, muted bool) Event {
	return Event{kind: kindSlider, label: label, icon: icon, percent: percent, bar: true, muted: muted}
}

// Toggle is a lock key's new state.
func Toggle(label, icon string, active bool) Event {
	return Event{kind: kindToggle, label: label, icon: icon, active: active}
}

// toggleEvent is handle_toggle_changed: the key's name and glyph.
func toggleEvent(e keytoggles.Event) Event {
	switch e.Key {
	case keytoggles.NumLock:
		return Toggle(i18n.T("osd-num-lock"), "ld-hash-symbolic", e.Active)
	case keytoggles.ScrollLock:
		return Toggle(i18n.T("osd-scroll-lock"), "ld-arrow-up-down-symbolic", e.Active)
	}
	return Toggle(i18n.T("osd-caps-lock"), "ld-a-large-small-symbolic", e.Active)
}

// toastEvent is handle_show_toast: the named preset supplies the label
// and icon, explicit request fields override them; percentage,
// duration, and class are per invocation.
func toastEvent(req widgetipc.ToastRequest, presets config.OsdConfig) Event {
	var preset config.ToastPreset
	if req.Preset != nil {
		p, ok := presets.Preset(*req.Preset)
		if !ok {
			log.Printf("toast: no preset %q", *req.Preset)
		}
		preset = p
	}
	ev := Event{kind: kindToast, label: deref(preset.Label), icon: deref(preset.Icon)}
	if req.Label != nil {
		ev.label = *req.Label
	}
	if req.Icon != nil {
		ev.icon = *req.Icon
	}
	if req.Percentage != nil {
		ev.percent, ev.bar = *req.Percentage, true
	}
	if req.DurationMS != nil {
		ev.durationMS = *req.DurationMS
	}
	if req.Class != nil {
		ev.class = *req.Class
	}
	return ev
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// classes is osd_classes: osd, then muted (a muted slider), toggle-off
// (a toggle turned off), bordered (osd.border), and a toast's class.
func classes(ev Event, border bool) []string {
	out := []string{"osd"}
	if ev.kind == kindSlider && ev.muted {
		out = append(out, "muted")
	}
	if ev.kind == kindToggle && !ev.active {
		out = append(out, "toggle-off")
	}
	if border {
		out = append(out, "bordered")
	}
	if ev.kind == kindToast && ev.class != "" {
		out = append(out, ev.class)
	}
	return out
}

// headerLabel is event_label for the toggle header: a toggle reads
// "<label> On/Off", a toast its label.
func headerLabel(ev Event) string {
	if ev.kind != kindToggle {
		return ev.label
	}
	if ev.active {
		return i18n.T("osd-toggle-on", i18n.Str("label", ev.label))
	}
	return i18n.T("osd-toggle-off", i18n.Str("label", ev.label))
}

// valueText is event_value: the rounded percentage.
func valueText(ev Event) string {
	return strconv.FormatInt(int64(math.Round(ev.percent)), 10) + "%"
}

// fraction is event_fraction: the bar's fill, clamped.
func fraction(ev Event) float64 { return min(max(ev.percent/100, 0), 1) }

// VolumeIcon is the output slider's glyph (methods.rs volume_icon).
func VolumeIcon(percent float64, muted bool) string {
	switch {
	case muted || percent <= 0:
		return "ld-volume-x-symbolic"
	case percent < 34:
		return "ld-volume-symbolic"
	case percent < 67:
		return "ld-volume-1-symbolic"
	}
	return "ld-volume-2-symbolic"
}

// MicrophoneIcon is the input slider's glyph.
func MicrophoneIcon(muted bool) string {
	if muted {
		return "ld-mic-off-symbolic"
	}
	return "ld-mic-symbolic"
}

// BrightnessIcon is the brightness slider's glyph (BRIGHTNESS_ICON).
const BrightnessIcon = "ld-sun-symbolic"
