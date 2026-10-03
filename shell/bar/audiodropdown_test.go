package bar

import (
	"context"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

func TestAudioVolumeAndInputIcons(t *testing.T) {
	for _, tc := range []struct {
		pct   float64
		muted bool
		want  string
	}{
		{50, true, "ld-volume-x-symbolic"},
		{0, false, "ld-volume-x-symbolic"},
		{20, false, "ld-volume-symbolic"},
		{50, false, "ld-volume-1-symbolic"},
		{80, false, "ld-volume-2-symbolic"},
	} {
		if got := audioVolumeIcon(tc.pct, tc.muted); got != tc.want {
			t.Errorf("audioVolumeIcon(%v, %v) = %q, want %q", tc.pct, tc.muted, got, tc.want)
		}
	}
	if audioInputIcon(true) != "ld-mic-off-symbolic" || audioInputIcon(false) != "ld-mic-symbolic" {
		t.Error("input icons")
	}
}

func TestAppDisplayName(t *testing.T) {
	for _, tc := range []struct{ app, stream, want string }{
		{"Firefox", "AudioStream", "Firefox"},
		{"", "AudioStream", "AudioStream"},
		{"spotify", "AudioStream", "Spotify"},
		{"", "", ""},
		{"élan", "", "Élan"},
	} {
		if got := appDisplayName(tc.app, tc.stream); got != tc.want {
			t.Errorf("appDisplayName(%q, %q) = %q, want %q", tc.app, tc.stream, got, tc.want)
		}
	}
}

func TestStreamIcon(t *testing.T) {
	mapped, native := config.AppIconSourceMapped, config.AppIconSourceNative
	for _, tc := range []struct {
		props  map[string]string
		source config.AppIconSource
		want   string
	}{
		{map[string]string{paAppIconName: "firefox"}, mapped, "si-firefox-symbolic"},
		{map[string]string{}, mapped, ""},
		{map[string]string{paAppName: "Microsoft Edge", paAppIconName: "microsoft-edge"}, mapped, "tb-brand-edge-symbolic"},
		{map[string]string{paAppIconName: "xyzzy-unknown"}, mapped, "xyzzy-unknown-symbolic"},
		{map[string]string{paAppIconName: "my-app-symbolic"}, mapped, "my-app-symbolic"},
		{map[string]string{paAppIconName: "firefox"}, native, "firefox"},
		{map[string]string{paAppProcessBinary: "spotify"}, native, "spotify"},
		{map[string]string{paAppIconName: "custom-icon", paAppProcessBinary: "myapp"}, native, "custom-icon"},
		{map[string]string{}, native, ""},
	} {
		if got := streamIcon(tc.props, tc.source); got != tc.want {
			t.Errorf("streamIcon(%v, %s) = %q, want %q", tc.props, tc.source, got, tc.want)
		}
	}
	// A native icon the theme lacks falls back to the mapped one; one
	// it has is kept.
	props := map[string]string{paAppIconName: "firefox"}
	if got := resolveStreamIcon(props, native, func(string) bool { return false }); got != "si-firefox-symbolic" {
		t.Errorf("missing native icon = %q, want the mapped one", got)
	}
	if got := resolveStreamIcon(props, native, func(string) bool { return true }); got != "firefox" {
		t.Errorf("present native icon = %q", got)
	}
	if !isEventStream(map[string]string{paStreamRestoreID: paRoleEvent}) || isEventStream(map[string]string{}) {
		t.Error("isEventStream")
	}
}

func TestDeviceIcons(t *testing.T) {
	none := map[string]string{}
	for _, tc := range []struct {
		name, desc string
		props      map[string]string
		want       string
	}{
		{"alsa_output.pci", "Built-in Audio", map[string]string{paFormFactor: "speaker"}, "tb-device-speaker-symbolic"},
		{"alsa_output.pci", "Something", map[string]string{paFormFactor: "headphone"}, "tb-headphones-symbolic"},
		{"alsa_output.hdmi", "HDMI", none, "tb-device-tv-symbolic"},
		{"bluez_output", "AirPods Pro", none, "tb-headset-symbolic"},
		{"alsa_output.pci", "Built-in Audio", none, "tb-device-speaker-symbolic"},
		// An unknown form factor falls through to the name.
		{"alsa_output.hdmi", "x", map[string]string{paFormFactor: "toaster"}, "tb-device-tv-symbolic"},
	} {
		if got := outputDeviceIcon(tc.name, tc.desc, tc.props); got != tc.want {
			t.Errorf("outputDeviceIcon(%q, %q) = %q, want %q", tc.name, tc.desc, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name, desc string
		props      map[string]string
		want       string
	}{
		{"bluez_input", "Headset", map[string]string{paFormFactor: "headset"}, "tb-headset-symbolic"},
		{"v4l2_input", "USB Webcam", none, "tb-device-computer-camera-symbolic"},
		{"alsa_input.pci", "Built-in Audio", none, "tb-microphone-symbolic"},
	} {
		if got := inputDeviceIcon(tc.name, tc.desc, tc.props); got != tc.want {
			t.Errorf("inputDeviceIcon(%q, %q) = %q, want %q", tc.name, tc.desc, got, tc.want)
		}
	}
	ports := []pulse.DevicePort{{Name: "speaker", Description: "Speaker"}}
	if got := activePortDescription("speaker", ports); got != "Speaker" {
		t.Errorf("active port = %q", got)
	}
	if activePortDescription("", ports) != "" || activePortDescription("gone", ports) != "" {
		t.Error("no or unlisted active port: want none")
	}
}

func TestAppVolumeRowsFilterDedupeAndSort(t *testing.T) {
	streams := []pulse.AudioStream{
		{Key: pulse.StreamKey{Index: 1}, ApplicationName: "zed", PID: 7},
		{Key: pulse.StreamKey{Index: 2}, ApplicationName: "zed", PID: 7},
		{Key: pulse.StreamKey{Index: 3}, ApplicationName: "bell", Properties: map[string]string{paStreamRestoreID: paRoleEvent}},
		{Key: pulse.StreamKey{Index: 4}, Name: "anon"},
		{Key: pulse.StreamKey{Index: 5}, Name: "anon2"},
	}
	rows := appVolumeRows(streams, config.AppIconSourceMapped, func(string) bool { return true })
	var got []uint32
	for _, r := range rows {
		got = append(got, r.key.Index)
	}
	// No event stream, one row for PID 7, no-PID streams all kept,
	// sorted by display name.
	if want := []uint32{4, 5, 1}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// audioFixture is a pulsetest server with two sinks (one a headset by
// form factor), a monitor, a microphone, and a Firefox stream.
func audioFixture(t *testing.T) (*pulsetest.Server, *pulse.Service) {
	t.Helper()
	srv := pulsetest.New(t)
	srv.PutSink(pulsetest.Sink(1, "speakers", "Speakers", 2))
	headset := pulsetest.Sink(3, "bt", "Headset", 4)
	headset.Props[paFormFactor] = "headset"
	srv.PutSink(headset)
	srv.PutSource(pulsetest.Source(2, "speakers.monitor", "Monitor of Speakers", 1, "speakers"))
	srv.PutSource(pulsetest.Source(10, "mic", "Microphone", native.InvalidIndex, ""))
	srv.PutSinkInput(native.StreamInfo{
		Index: 40, Name: "Playback", OwnerModule: native.InvalidIndex, Client: 12, Device: 1,
		SampleSpec: pulsetest.StereoSpec(), ChannelMap: pulsetest.StereoMap(),
		Volume:    native.CVolume{native.VolumeNorm, native.VolumeNorm},
		Props:     native.PropList{"application.name": "firefox", "application.process.id": "4242"},
		HasVolume: true, VolumeWritable: true,
		Format: native.FormatInfo{Encoding: native.EncodingPCM, Props: native.PropList{}},
	})
	srv.SetDefaults("speakers", "mic")
	svc, err := pulse.Connect(context.Background(), srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return srv, svc
}

func TestAudioDropdownEmptyWithoutDevices(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	v := audioDropdown(ctx).(*audioView)
	if v.main.Visible() != "empty" || v.apps.Visible() != "empty" {
		t.Errorf("no service: main %q apps %q", v.main.Visible(), v.apps.Visible())
	}
	if v.output.trigger.Visible() || !v.output.noDevice.Visible() {
		t.Error("no service: want the no-device row")
	}
}

func TestAudioDropdownSectionsAndApps(t *testing.T) {
	srv, svc := audioFixture(t)
	ctx := newTestContext(t, config.Defaults())
	ctx.Pulse = svc
	v := audioDropdown(ctx).(*audioView)
	defer v.dropdownClosed()

	if v.main.Visible() != "devices" {
		t.Fatalf("main = %q", v.main.Visible())
	}
	if v.output.name.Text() != "Speakers" || v.output.icon.Name() != "tb-device-speaker-symbolic" {
		t.Errorf("output = %q %q", v.output.name.Text(), v.output.icon.Name())
	}
	if v.output.slider.Value() != 50 || v.output.muteIcon.Name() != "ld-volume-1-symbolic" {
		t.Errorf("output slider %v icon %q", v.output.slider.Value(), v.output.muteIcon.Name())
	}
	if v.input.name.Text() != "Microphone" || v.input.muteIcon.Name() != "ld-mic-symbolic" {
		t.Errorf("input = %q %q", v.input.name.Text(), v.input.muteIcon.Name())
	}
	if got := len(v.pickers[audioInput].list.Children()); got != 1 {
		t.Errorf("input picker rows = %d, want the monitor dropped", got)
	}
	if got := len(v.pickers[audioOutput].list.Children()); got != 2 {
		t.Errorf("output picker rows = %d", got)
	}
	selected := 0
	for _, c := range v.pickers[audioOutput].list.Children() {
		if c.(*widget.Button).HasClass("selected") {
			selected++
		}
	}
	if selected != 1 {
		t.Errorf("%d rows marked the default, want 1", selected)
	}
	if v.apps.Visible() != "apps" || len(v.apps.items) != 1 {
		t.Fatalf("apps page %q with %d rows", v.apps.Visible(), len(v.apps.items))
	}
	app := v.apps.items[pulse.StreamKey{Index: 40, Type: pulse.StreamPlayback}]
	if app == nil || app.value.Text() != "100%" {
		t.Fatalf("firefox row = %+v", app)
	}

	// Mute writes to the server and the follow loop reflects it.
	v.output.toggleMute()
	waitHeadless(t, "the mute", func() bool { return v.output.mute.HasClass("muted") })
	if s, _ := srv.Sink(1); !s.Mute {
		t.Error("the server sink is not muted")
	}
	if v.output.muteIcon.Name() != "ld-volume-x-symbolic" {
		t.Errorf("muted icon = %q", v.output.muteIcon.Name())
	}

	// A slider commit sets the device volume.
	v.output.slider.Knob.SetValue(30)
	waitHeadless(t, "the volume write", func() bool {
		s, _ := srv.Sink(1)
		return s.Volume[0] < 0x8000
	})

	// An app commit sets the stream volume; the row survives (no
	// rebuild on a property change).
	app.slider.Knob.SetValue(40)
	waitHeadless(t, "the app volume", func() bool {
		st, _ := srv.SinkInput(40)
		return st.Volume[0] < native.VolumeNorm
	})
	if v.apps.items[pulse.StreamKey{Index: 40, Type: pulse.StreamPlayback}] != app {
		t.Error("a volume change rebuilt the app row")
	}

	// Picking the headset returns to main and changes the default.
	v.showPage("output")
	var headsetRow *widget.Button
	for _, c := range v.pickers[audioOutput].list.Children() {
		if b := c.(*widget.Button); !b.HasClass("selected") {
			headsetRow = b
		}
	}
	headsetRow.OnClick()
	if v.pages.Visible() != "main" {
		t.Errorf("after pick page = %q", v.pages.Visible())
	}
	waitHeadless(t, "the new default", func() bool { return v.output.name.Text() == "Headset" })
	if sink, _ := srv.Defaults(); sink != "bt" {
		t.Errorf("default sink = %q", sink)
	}
	if v.output.icon.Name() != "tb-headset-symbolic" {
		t.Errorf("headset icon = %q", v.output.icon.Name())
	}

	// The stream going away shows the no-apps state.
	srv.RemoveSinkInput(40)
	waitHeadless(t, "the no-apps state", func() bool { return v.apps.Visible() == "empty" })
	if i18n.T("dropdown-audio-no-apps") == "" {
		t.Error("no-apps string missing")
	}
}

// boundsOf reads a widget's arranged rect.
func boundsOf(w widget.Widget) render.Rect {
	return w.(interface{ Bounds() render.Rect }).Bounds()
}

func midX(r render.Rect) int { return r.X + r.W/2 }
func midY(r render.Rect) int { return r.Y + r.H/2 }

// sameWidgets reports the two children lists holding the same widget
// pointers in order.
func sameWidgets(a, b []widget.Widget) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Every box the audio tree builds is constructor-spacing 0, as the
// Rust view's boxes are: the gaps come from the stylesheet's
// border-spacing and margins, never from the constructors.
func TestAudioBoxesKeepRustZeroSpacing(t *testing.T) {
	_, svc := audioFixture(t)
	ctx := newTestContext(t, config.Defaults())
	ctx.Pulse = svc
	v := audioDropdown(ctx).(*audioView)
	defer v.dropdownClosed()

	st := readAudioState(context.Background(), svc)
	optionContent := v.pickers[audioOutput].optionContent(st.outputs[0], true)
	boxes := []struct {
		what string
		w    widget.Widget
	}{
		{"root", v.Box},
		{"devices page", findByClass(v.main, "audio-devices")},
		{"volume section", v.output.Box},
		{"device trigger", findByClass(v.output, "audio-device-trigger")},
		{"device info", findByClass(v.output, "audio-device-info")},
		{"device name row", v.output.nameRow},
		{"no-device row", v.output.noDevice},
		{"app list", v.apps.list},
		{"app item", findByClass(v.apps.list, "audio-app-item")},
		{"app item header", findByClass(v.apps.list, "audio-app-header")},
		{"picker root", v.pickers[audioOutput].Box},
		{"picker header", findByClass(v.pickers[audioOutput], "picker-header")},
		{"picker list", v.pickers[audioOutput].list},
		{"option content", optionContent},
		{"option info", optionContent.Children()[1]},
	}
	for _, b := range boxes {
		spaced, ok := b.w.(interface{ Spacing() int })
		if !ok {
			t.Fatalf("%s = %T, want a box", b.what, b.w)
		}
		if got := spaced.Spacing(); got != 0 {
			t.Errorf("%s constructor spacing = %d, want 0 (the gap is CSS's)", b.what, got)
		}
	}

	// Bare of the stylesheet the trigger's children touch: with the
	// constructor spacing re-added this gap would be non-zero.
	sz := v.Measure(widgetConstraintsMax(420, 700))
	v.Arrange(render.Rect{W: sz.W, H: 700})
	trig := findByClass(v.output, "audio-device-trigger").(*widget.Box)
	tile, info := boundsOf(trig.Children()[0]), boundsOf(trig.Children()[1])
	if gap := info.X - (tile.X + tile.W); gap != 0 {
		t.Errorf("bare trigger gap = %d, want 0", gap)
	}
}

// Under the stylesheet the trigger gap is the rule's border-spacing:
// CSS owns the gaps the Rust boxes leave unset.
func TestAudioTriggerGapComesFromTheStylesheet(t *testing.T) {
	_, svc := audioFixture(t)
	ctx := styledContext(t, config.Defaults())
	ctx.Pulse = svc
	v := audioDropdown(ctx).(*audioView)
	defer v.dropdownClosed()
	ctx.Theme.Attach(v)
	sz := v.Measure(widgetConstraintsMax(420, 700))
	v.Arrange(render.Rect{W: sz.W, H: 700})
	trig := findByClass(v.output, "audio-device-trigger").(*widget.Box)
	tile, info := boundsOf(trig.Children()[0]), boundsOf(trig.Children()[1])
	if gap := info.X - (tile.X + tile.W); gap <= 0 {
		t.Errorf("styled trigger gap = %d, want the border-spacing", gap)
	}
}

// The rows valign-center what the Rust view centers: the device icon
// tile and mute button (volume_section/mod.rs:49,96), the app icon
// tile and app mute button (app_volume_item.rs:56,82), and the
// option's info column (device_item.rs:50).
func TestAudioRowsValignCenterTheRustWidgets(t *testing.T) {
	_, svc := audioFixture(t)
	ctx := newTestContext(t, config.Defaults())
	ctx.Pulse = svc
	v := audioDropdown(ctx).(*audioView)
	defer v.dropdownClosed()

	// Arrange the row taller than natural: fill children span it,
	// aligned ones keep their natural height and sit centered.
	centered := func(what string, row *widget.Box, kids ...widget.Widget) {
		t.Helper()
		nat := row.Measure(widgetConstraintsMax(400, 200))
		row.Arrange(render.Rect{W: nat.W, H: nat.H + 24})
		for _, k := range kids {
			if d := midY(boundsOf(k)) - midY(boundsOf(row)); d < -1 || d > 1 {
				t.Errorf("%s sits %dpx off vertical center", what, d)
			}
			kn := k.Measure(widgetConstraintsMax(400, 200))
			if h := boundsOf(k).H; h != kn.H {
				t.Errorf("%s spans %dpx, want its natural %d (it fills the row)", what, h, kn.H)
			}
		}
	}
	trig := findByClass(v.output, "audio-device-trigger").(*widget.Box)
	centered("the device icon tile", trig, trig.Children()[0])
	centered("the device mute button", trig, v.output.mute)
	header := findByClass(v.apps.list, "audio-app-header").(*widget.Box)
	centered("the app icon tile", header, header.Children()[0])
	centered("the app mute button", header, header.Children()[3])
	st := readAudioState(context.Background(), svc)
	option := v.pickers[audioOutput].optionContent(st.outputs[0], false)
	centered("the option info column", option, option.Children()[1])
}

// Both empty states sit centered in their stack page (the Rust
// vexpand wrappers), the app one's icon carries the sm size modifier
// and the main one does not (main_section/mod.rs has no modifier).
func TestAudioEmptyStatesCenterVertically(t *testing.T) {
	restore := widget.SetAnimationsInstant(true)
	t.Cleanup(restore)
	v := audioDropdown(newTestContext(t, config.Defaults())).(*audioView)
	sz := v.Measure(widgetConstraintsMax(420, 700))
	v.Arrange(render.Rect{W: sz.W, H: 700})

	for _, tc := range []struct {
		what string
		root widget.Widget
	}{
		{"main empty state", v.main},
		{"app empty state", v.apps},
	} {
		st := findByClass(tc.root, "empty-state")
		if st == nil {
			t.Fatalf("%s: no empty-state in the tree", tc.what)
		}
		if d := midY(boundsOf(st)) - midY(boundsOf(tc.root)); d < -1 || d > 1 {
			t.Errorf("%s sits %dpx off vertical center", tc.what, d)
		}
		if h := boundsOf(st).H; h >= boundsOf(tc.root).H {
			t.Errorf("%s fills its page (%dpx): the centering wrapper is gone", tc.what, h)
		}
	}
	if icon := findByClass(v.main, "icon"); icon != nil && icon.(interface{ HasClass(string) bool }).HasClass("sm") {
		t.Error("the main empty-state icon carries the sm modifier")
	}
	icon := findByClass(v.apps, "icon")
	if icon == nil || !icon.(interface{ HasClass(string) bool }).HasClass("sm") {
		t.Error("the app empty-state icon misses the sm modifier")
	}
}

// The no-device row is one centered icon+label pair: no spacers, the
// label carrying the audio-no-device-label class.
func TestAudioNoDeviceRowCentersWithItsLabel(t *testing.T) {
	v := audioDropdown(newTestContext(t, config.Defaults())).(*audioView)
	row := v.output.noDevice
	kids := row.Children()
	if len(kids) != 2 {
		t.Fatalf("no-device row children = %d, want the icon and the label", len(kids))
	}
	if !widget.HasClass(kids[0], "audio-no-device-icon") {
		t.Error("no-device row misses the alert icon")
	}
	label, ok := kids[1].(*widget.Label)
	if !ok || !label.HasClass("audio-no-device-label") {
		t.Error("the no-device label misses audio-no-device-label")
	}

	nat := v.output.Measure(widgetConstraintsMax(420, 200))
	v.output.Arrange(render.Rect{W: nat.W + 160, H: nat.H})
	rb, sec := boundsOf(row), boundsOf(v.output)
	if d := midX(rb) - midX(sec); d < -1 || d > 1 {
		t.Errorf("the no-device row sits %dpx off horizontal center", d)
	}
	if rb.W >= sec.W {
		t.Errorf("the row fills the section (%d >= %d): the spacer trick is back", rb.W, sec.W)
	}
}

// The picker's back button is the GhostIconButton (ghost-icon beside
// picker-back) and the option tile is the CenterBox the Rust factory
// builds — the image inside carries no invented class.
func TestAudioPickerBackAndOptionIconClasses(t *testing.T) {
	v := audioDropdown(newTestContext(t, config.Defaults())).(*audioView)
	found := findByClass(v.pickers[audioOutput], "picker-back")
	back, ok := found.(*widget.Button)
	if !ok {
		t.Fatalf("picker-back = %T, want a button", found)
	}
	if !back.HasClass("ghost-icon") {
		t.Error("the back button misses the ghost-icon class")
	}
	if findByClass(v.pickers[audioOutput], "audio-device-option-icon-img") != nil {
		t.Error("the option image carries the invented icon-img class")
	}
	content := v.pickers[audioOutput].optionContent(
		audioDevice{description: "Speakers", icon: "tb-device-speaker-symbolic"}, false)
	tile, ok := content.Children()[0].(*widget.CenterBox)
	if !ok {
		t.Fatalf("option tile = %T, want the CenterBox", content.Children()[0])
	}
	if !tile.HasClass("audio-device-option-icon") {
		t.Error("the option tile misses audio-device-option-icon")
	}
}

// The picker title keeps its natural width: the free width after it
// stays empty, as the Rust label without hexpand does.
func TestAudioPickerTitleKeepsNaturalWidth(t *testing.T) {
	v := audioDropdown(newTestContext(t, config.Defaults())).(*audioView)
	p := v.pickers[audioOutput]
	title := findByClass(p, "picker-title").(*widget.Label)
	header := findByClass(p, "picker-header").(*widget.Box)
	hh := header.Measure(widgetConstraintsMax(420, 60))
	nat := title.Measure(widgetConstraintsMax(hh.W+200, 60))
	header.Arrange(render.Rect{W: hh.W + 200, H: hh.H})
	if got := boundsOf(title).W; got != nat.W {
		t.Errorf("title width = %d, want its natural %d (it took the free width)", got, nat.W)
	}
}

// Picker rows rebuild only when the list or the default changed: a
// no-op tick — even one that only moved a device's volume, which the
// Rust DeviceInfo does not carry — keeps the row widgets (their hover
// state with them), while a real change rebuilds.
func TestAudioPickerRowsRebuildOnlyOnChange(t *testing.T) {
	srv, svc := audioFixture(t)
	ctx := newTestContext(t, config.Defaults())
	ctx.Pulse = svc
	v := audioDropdown(ctx).(*audioView)
	defer v.dropdownClosed()
	p := v.pickers[audioOutput]
	rows := p.list.Children()

	// The service learns of server mutations through its event loop,
	// so each step waits until a read reflects it.
	await := func(what string, cond func(audioState) bool) audioState {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			st := readAudioState(context.Background(), svc)
			if cond(st) {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// The same state again: nothing rebuilds.
	st := await("the settled state", func(st audioState) bool {
		return len(st.outputs) == 2 && st.defaultOut != nil && st.defaultOut.key.Index == 1
	})
	v.ctx.Invoke(func() { p.apply(st.outputs, st.defaultOut) })
	if !sameWidgets(rows, p.list.Children()) {
		t.Error("a no-op tick rebuilt the picker rows")
	}

	// A volume change alone is still a no-op.
	quiet := pulsetest.Sink(1, "speakers", "Speakers", 2)
	quiet.Volume = native.CVolume{native.VolumeNorm / 4, native.VolumeNorm / 4}
	srv.PutSink(quiet)
	st = await("the quieter sink", func(st audioState) bool {
		return len(st.outputs) == 2 && st.outputs[0].volume.AveragePercentage() < 50
	})
	v.ctx.Invoke(func() { p.apply(st.outputs, st.defaultOut) })
	if !sameWidgets(rows, p.list.Children()) {
		t.Error("a volume-only tick rebuilt the picker rows")
	}

	// A new default moves the selection: rebuild.
	srv.SetDefaults("bt", "mic")
	st = await("the new default", func(st audioState) bool {
		return st.defaultOut != nil && st.defaultOut.key.Index == 3
	})
	v.ctx.Invoke(func() { p.apply(st.outputs, st.defaultOut) })
	if sameWidgets(rows, p.list.Children()) {
		t.Error("the new default did not rebuild the rows")
	}
	if !p.hasActive || p.active != st.defaultOut.key {
		t.Errorf("active = %v/%v, want the headset", p.active, p.hasActive)
	}

	// A new device grows the list: rebuild.
	srv.PutSink(pulsetest.Sink(5, "usb", "USB DAC", 2))
	st = await("the third sink", func(st audioState) bool { return len(st.outputs) == 3 })
	v.ctx.Invoke(func() { p.apply(st.outputs, st.defaultOut) })
	if got := len(p.list.Children()); got != 3 {
		t.Errorf("picker rows = %d, want 3", got)
	}
}
