package bar

import (
	"context"
	"testing"

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
