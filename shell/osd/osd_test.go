package osd

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/service/keytoggles"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/shell/treetest"
)

// fakeWindow records what the OSD asks of its surface.
type fakeWindow struct {
	cfg     app.LayerConfig
	anchors []app.Anchor
	margins []app.Margins
	layers  []app.Layer
	closed  bool
}

func (w *fakeWindow) Close()                        { w.closed = true }
func (w *fakeWindow) SetAnchor(a app.Anchor) error  { w.anchors = append(w.anchors, a); return nil }
func (w *fakeWindow) SetMargin(m app.Margins) error { w.margins = append(w.margins, m); return nil }
func (w *fakeWindow) SetLayer(l app.Layer) error    { w.layers = append(w.layers, l); return nil }

// timer is one pending After.
type timer struct {
	d  time.Duration
	fn func()
}

// harness runs the OSD synchronously: Invoke is a call, After queues.
type harness struct {
	osd     *Osd
	outputs []*app.Output
	opened  []*fakeWindow
	timers  []timer
}

func newHarness(t *testing.T, cfg *config.Config) *harness {
	t.Helper()
	h := &harness{outputs: []*app.Output{{Name: "DP-1"}, {Name: "HDMI-A-1"}}}
	h.osd = New(Deps{
		Open: func(lc app.LayerConfig) (Window, error) {
			w := &fakeWindow{cfg: lc}
			h.opened = append(h.opened, w)
			return w, nil
		},
		Outputs: func() []*app.Output { return h.outputs },
		Invoke:  func(fn func()) { fn() },
		After:   func(d time.Duration, fn func()) { h.timers = append(h.timers, timer{d, fn}) },
		Font:    treetest.Font(t),
	}, cfg)
	return h
}

// fire runs the queued timer of duration d (the first one).
func (h *harness) fire(t *testing.T, d time.Duration) {
	t.Helper()
	i := slices.IndexFunc(h.timers, func(tm timer) bool { return tm.d == d })
	if i < 0 {
		t.Fatalf("no %v timer queued (have %v)", d, h.timers)
	}
	tm := h.timers[i]
	h.timers = slices.Delete(h.timers, i, i+1)
	tm.fn()
}

func quietConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Animations.Enabled = false
	return cfg
}

func readyHarness(t *testing.T, cfg *config.Config) *harness {
	t.Helper()
	h := newHarness(t, cfg)
	h.fire(t, readyDelay)
	return h
}

// The start-up levels are not events: nothing maps inside the quiet
// period, and the first event after it does.
func TestQuietPeriodDropsEvents(t *testing.T) {
	h := newHarness(t, quietConfig())
	h.osd.Show(Slider("Speakers", VolumeIcon(40, false), 40, false))
	if len(h.opened) != 0 {
		t.Fatal("an event inside the quiet period mapped the OSD")
	}
	h.fire(t, readyDelay)
	h.osd.Show(Slider("Speakers", VolumeIcon(40, false), 40, false))
	if len(h.opened) != 1 {
		t.Fatalf("%d surfaces after the quiet period, want 1", len(h.opened))
	}
	lc := h.opened[0].cfg
	if lc.Namespace != "wayle-osd" || lc.Keyboard != app.KeyboardNone || lc.Output != h.outputs[0] {
		t.Errorf("surface %+v, want wayle-osd on the primary without keyboard", lc)
	}
	if lc.Width != 0 || lc.Height != 0 {
		t.Errorf("surface sized %dx%d, want content-sized", lc.Width, lc.Height)
	}
}

func TestDisabledOsdNeverShows(t *testing.T) {
	cfg := quietConfig()
	cfg.Osd.Enabled = false
	h := readyHarness(t, cfg)
	h.osd.Show(Slider("Speakers", "", 40, false))
	h.osd.ShowToast(widgetipc.ToastRequest{Label: new("hi")})
	if len(h.opened) != 0 {
		t.Error("a disabled OSD mapped")
	}
}

// The surface goes to osd.monitor, falling back to the primary when the
// connector is gone, and moves when the monitor changes.
func TestOsdFollowsTheConfiguredMonitor(t *testing.T) {
	cfg := quietConfig()
	cfg.Osd.Monitor = config.OsdOnConnector("HDMI-A-1")
	h := readyHarness(t, cfg)
	h.osd.Show(Slider("x", "", 1, false))
	if h.opened[0].cfg.Output != h.outputs[1] {
		t.Fatalf("opened on %s, want HDMI-A-1", h.opened[0].cfg.Output.Name)
	}
	next := quietConfig()
	next.Osd.Monitor = config.OsdOnConnector("DP-9")
	h.osd.SetConfig(next)
	if !h.opened[0].closed || len(h.opened) != 2 || h.opened[1].cfg.Output != h.outputs[0] {
		t.Errorf("a missing connector must move the open OSD to the primary")
	}
}

// A second event reuses the mapped surface and re-applies its anchor,
// margin, and layer (apply_position per event); a config change
// re-anchors it.
func TestOneSurfaceReanchored(t *testing.T) {
	h := readyHarness(t, quietConfig())
	h.osd.Show(Slider("a", "", 1, false))
	h.osd.Show(Slider("b", "", 2, false))
	if len(h.opened) != 1 {
		t.Fatalf("%d surfaces, want one reused", len(h.opened))
	}
	next := quietConfig()
	next.Osd.Position = config.OsdTopRight
	next.Styling.Scale = 2
	h.osd.SetConfig(next)
	w := h.opened[0]
	if got := w.anchors[len(w.anchors)-1]; got != app.AnchorTop|app.AnchorRight {
		t.Errorf("re-anchored to %v, want top|right", got)
	}
	if got := w.margins[len(w.margins)-1]; got != (app.Margins{Top: 300, Right: 300}) {
		t.Errorf("margins %+v, want 150px x scale 2 on the anchored edges", got)
	}
}

func TestAnchorsAndMargins(t *testing.T) {
	for pos, want := range map[config.OsdPosition]app.Anchor{
		config.OsdTopLeft:     app.AnchorTop | app.AnchorLeft,
		config.OsdTop:         app.AnchorTop,
		config.OsdTopRight:    app.AnchorTop | app.AnchorRight,
		config.OsdRight:       app.AnchorRight,
		config.OsdBottomRight: app.AnchorBottom | app.AnchorRight,
		config.OsdBottom:      app.AnchorBottom,
		config.OsdBottomLeft:  app.AnchorBottom | app.AnchorLeft,
		config.OsdLeft:        app.AnchorLeft,
	} {
		if got := anchors(pos); got != want {
			t.Errorf("%s anchors %v, want %v", pos, got, want)
		}
	}
	cfg := config.Defaults()
	cfg.Osd.Position = config.OsdBottomLeft
	cfg.Osd.Margin = config.Size{Value: 10.7, Unit: config.SizePixels}
	if got := margins(cfg); got != (app.Margins{Bottom: 10, Left: 10}) {
		t.Errorf("margins %+v, want 10.7px truncated on bottom and left only", got)
	}
}

// A toast's duration overrides osd.duration; only the latest event's
// timer dismisses, and the dismissal unmaps after the exit.
func TestDismissFollowsTheLatestEvent(t *testing.T) {
	h := readyHarness(t, quietConfig())
	h.osd.ShowToast(widgetipc.ToastRequest{Label: new("saved"), DurationMS: new(uint32(900))})
	h.osd.Show(Slider("a", "", 1, false))
	h.fire(t, 900*time.Millisecond)
	if h.opened[0].closed {
		t.Fatal("a superseded event's timer dismissed the OSD")
	}
	h.fire(t, time.Duration(config.DefaultsOsd().Duration)*time.Millisecond)
	if !h.opened[0].closed {
		t.Error("the latest event's timer did not dismiss the OSD")
	}
	h.osd.Show(Slider("b", "", 1, false))
	if len(h.opened) != 2 {
		t.Error("an event after the dismissal did not map a fresh surface")
	}
}

// The view shows the slider header and the bar for a level, the toggle
// header placed by text-align for everything else, with the classes
// the stylesheet keys on.
func TestViewFollowsTheEvent(t *testing.T) {
	cfg := quietConfig()
	cfg.Osd.TextAlign = config.OsdTextAlignEnd
	h := readyHarness(t, cfg)
	h.osd.Show(Slider("Speakers", VolumeIcon(0, true), 42.6, true))
	f := h.osd.face
	for _, l := range []*widget.Label{f.sliderLabel, f.value, f.toggleLabel} {
		if l.Color() != 0 {
			t.Errorf("label %v pins ink %#08x over the stylesheet", l.Classes(), uint32(l.Color()))
		}
	}
	if !slices.Equal(f.container.Classes(), []string{"osd", "muted", "bordered"}) {
		t.Errorf("slider classes %v", f.container.Classes())
	}
	if !f.sliderHeader.Visible() || !f.bar.Visible() || f.toggleHeader.Visible() {
		t.Error("a level shows the slider header and the bar only")
	}
	if f.sliderLabel.Text() != "Speakers" || f.value.Text() != "43%" || f.bar.Value() != 0.426 {
		t.Errorf("slider reads %q %q %v", f.sliderLabel.Text(), f.value.Text(), f.bar.Value())
	}

	h.osd.Show(toggleEvent(keytoggles.Event{Key: keytoggles.CapsLock}))
	if !slices.Equal(f.container.Classes(), []string{"osd", "toggle-off", "bordered"}) {
		t.Errorf("toggle classes %v", f.container.Classes())
	}
	if f.sliderHeader.Visible() || f.bar.Visible() || !f.toggleHeader.Visible() {
		t.Error("a toggle shows the toggle header only")
	}
	if f.toggleLabel.Text() != "\u2068Caps Lock\u2069 Off" || f.toggleHeader.HAlign() != widget.AlignEnd {
		t.Errorf("toggle reads %q aligned %v", f.toggleLabel.Text(), f.toggleHeader.HAlign())
	}

	h.osd.ShowToast(widgetipc.ToastRequest{Label: new("Done"), Class: new("success")})
	if !slices.Equal(f.container.Classes(), []string{"osd", "bordered", "success"}) {
		t.Errorf("toast classes %v", f.container.Classes())
	}
	if f.toggleIcon.Visible() || f.toggleLabel.Text() != "Done" {
		t.Error("a toast without an icon shows its label only")
	}
}

// Presets supply the label and icon; request fields override them; an
// unknown preset still shows the request's fields.
func TestToastEvent(t *testing.T) {
	osd := config.DefaultsOsd()
	osd.Presets = []config.ToastPreset{{ID: "saved", Label: new("Saved"), Icon: new("ld-check-symbolic")}}
	ev := toastEvent(widgetipc.ToastRequest{Preset: new("saved")}, osd)
	if ev.label != "Saved" || ev.icon != "ld-check-symbolic" || ev.bar {
		t.Errorf("preset toast %+v", ev)
	}
	ev = toastEvent(widgetipc.ToastRequest{Preset: new("saved"), Label: new("Stored"), Percentage: new(30.0)}, osd)
	if ev.label != "Stored" || ev.icon != "ld-check-symbolic" || !ev.bar || ev.percent != 30 {
		t.Errorf("overridden toast %+v", ev)
	}
	ev = toastEvent(widgetipc.ToastRequest{Preset: new("nope"), Label: new("x")}, osd)
	if ev.label != "x" || ev.icon != "" {
		t.Errorf("unknown preset toast %+v", ev)
	}
}

func TestIcons(t *testing.T) {
	for _, tc := range []struct {
		pct   float64
		muted bool
		want  string
	}{
		{50, true, "ld-volume-x-symbolic"},
		{0, false, "ld-volume-x-symbolic"},
		{33, false, "ld-volume-symbolic"},
		{34, false, "ld-volume-1-symbolic"},
		{66, false, "ld-volume-1-symbolic"},
		{67, false, "ld-volume-2-symbolic"},
	} {
		if got := VolumeIcon(tc.pct, tc.muted); got != tc.want {
			t.Errorf("VolumeIcon(%v, %v) = %s, want %s", tc.pct, tc.muted, got, tc.want)
		}
	}
	if MicrophoneIcon(true) != "ld-mic-off-symbolic" || MicrophoneIcon(false) != "ld-mic-symbolic" {
		t.Error("microphone icons")
	}
}

// fakeAudio scripts the default devices.
type fakeAudio struct {
	pulse.Source
	sink, source pulse.Device
}

func (f *fakeAudio) DefaultSink(context.Context) (pulse.OutputDevice, error) {
	return pulse.OutputDevice{Device: f.sink}, nil
}

func (f *fakeAudio) DefaultSource(context.Context) (pulse.InputDevice, error) {
	return pulse.InputDevice{Device: f.source}, nil
}

// A level flashes only when its rounded value or mute state moved, and
// carries the device's description.
func TestAudioEventsDedupe(t *testing.T) {
	src := &fakeAudio{
		sink:   pulse.Device{Description: "Speakers", Volume: pulse.VolumeFromPercentage(40, 2)},
		source: pulse.Device{Description: "Mic", Volume: pulse.VolumeFromPercentage(70, 1)},
	}
	var a audioEvents
	if evs := a.next(context.Background(), src); len(evs) != 2 {
		t.Fatalf("seed produced %d events, want both devices", len(evs))
	}
	src.sink.Volume = pulse.VolumeFromPercentage(40.2, 2)
	if evs := a.next(context.Background(), src); len(evs) != 0 {
		t.Errorf("a sub-percent move flashed %+v", evs)
	}
	src.source.Muted = true
	evs := a.next(context.Background(), src)
	if len(evs) != 1 || evs[0].label != "Mic" || evs[0].icon != "ld-mic-off-symbolic" || !evs[0].muted {
		t.Errorf("muting the mic flashed %+v, want one muted Mic slider", evs)
	}
}

// fakeLight scripts the backlights.
type fakeLight struct {
	brightness.Source
	devices []brightness.Device
}

func (f *fakeLight) Devices(context.Context) ([]brightness.Device, error) { return f.devices, nil }

func TestBrightnessEventsAverageAndDedupe(t *testing.T) {
	src := &fakeLight{devices: []brightness.Device{{Brightness: 20, Max: 100}, {Brightness: 60, Max: 100}}}
	var b brightnessEvents
	ev, ok := b.next(context.Background(), src)
	if !ok || ev.percent != 40 || ev.icon != BrightnessIcon || ev.label != "Brightness" {
		t.Fatalf("first level %+v %v, want the 40%% average", ev, ok)
	}
	if _, ok := b.next(context.Background(), src); ok {
		t.Error("an unchanged average flashed")
	}
	src.devices = nil
	if _, ok := b.next(context.Background(), src); ok {
		t.Error("no backlights flashed")
	}
}
