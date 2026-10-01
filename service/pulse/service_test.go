package pulse

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

func playback(index, sinkIndex uint32) native.StreamInfo {
	return native.StreamInfo{
		Index: index, Name: "Playback", OwnerModule: native.InvalidIndex, Client: 12, Device: sinkIndex,
		SampleSpec: pulsetest.StereoSpec(), ChannelMap: pulsetest.StereoMap(), Volume: native.CVolume{native.VolumeNorm, native.VolumeNorm},
		BufferLatency: 2000, DeviceLatency: 3000, ResampleMethod: "speex-float-1", Driver: "protocol-native.c",
		Props: native.PropList{
			"application.name": "Firefox", "application.process.id": "4242",
			"application.process.binary": "firefox", "application.icon_name": "firefox",
			"media.title": "Song", "media.artist": "Band",
		},
		HasVolume: true, VolumeWritable: true,
		Format: native.FormatInfo{Encoding: native.EncodingPCM, Props: native.PropList{}},
	}
}

// fixture is a server with two sinks, their monitors, a microphone,
// one playback and one recording stream.
func fixture(t *testing.T) *pulsetest.Server {
	t.Helper()
	srv := pulsetest.New(t)
	srv.PutSink(pulsetest.Sink(1, "speakers", "Speakers", 2))
	srv.PutSink(pulsetest.Sink(3, "headphones", "Headphones", 4))
	srv.PutSource(pulsetest.Source(2, "speakers.monitor", "Monitor of Speakers", 1, "speakers"))
	srv.PutSource(pulsetest.Source(4, "headphones.monitor", "Monitor of Headphones", 3, "headphones"))
	srv.PutSource(pulsetest.Source(10, "mic", "Microphone", native.InvalidIndex, ""))
	srv.PutSinkInput(playback(40, 1))
	rec := playback(50, 10)
	rec.Props = native.PropList{"application.name": "OBS", "application.process.id": "not-a-pid"}
	rec.Corked = true
	srv.PutSourceOutput(rec)
	srv.SetDefaults("speakers", "mic")
	return srv
}

func connect(t *testing.T, srv *pulsetest.Server) *Service {
	t.Helper()
	s, err := Connect(context.Background(), srv.Addr())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// waitFor polls until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConnectDiscoversEverything(t *testing.T) {
	s := connect(t, fixture(t))

	outs := s.OutputDevices()
	if len(outs) != 2 || outs[0].Name != "speakers" || outs[1].Name != "headphones" {
		t.Fatalf("outputs = %v", outs)
	}
	sp := outs[0]
	if sp.Key != (DeviceKey{Index: 1, Type: DeviceOutput}) || sp.Description != "Speakers" ||
		sp.MonitorSource != 2 || sp.MonitorSourceName != "speakers.monitor" || sp.State != StateRunning ||
		!near(sp.Volume.AveragePercentage(), 50) || sp.Volume.Channels() != 2 || !sp.BaseVolume.IsNormal() ||
		sp.ActivePort != "speaker" || sp.Latency != 0 {
		t.Errorf("speakers = %+v", sp)
	}
	if !sp.Ports[0].Available || sp.Ports[1].Available {
		t.Errorf("port availability = %+v (unknown counts as available, no does not)", sp.Ports)
	}
	if card, ok := sp.Card.Get(); !ok || card != 0 {
		t.Errorf("card = %v, %v", card, ok)
	}
	if len(sp.Formats) != 1 || sp.Formats[0].Encoding != "PCM" {
		t.Errorf("formats = %+v", sp.Formats)
	}

	ins := s.InputDevices()
	if len(ins) != 3 {
		t.Fatalf("inputs = %v", ins)
	}
	if !ins[0].IsMonitor() || ins[0].MonitorOfSinkName != "speakers" {
		t.Errorf("monitor source = %+v", ins[0])
	}
	mic := ins[2]
	if mic.IsMonitor() || mic.MonitorOfSinkName != "" || mic.State != StateSuspended {
		t.Errorf("mic = %+v", mic)
	}
	if _, ok := mic.Card.Get(); ok {
		t.Error("an invalid card index read as set")
	}

	play := s.PlaybackStreams()
	if len(play) != 1 {
		t.Fatalf("playback = %v", play)
	}
	p := play[0]
	if p.Key != (StreamKey{Index: 40, Type: StreamPlayback}) || p.ApplicationName != "Firefox" || p.PID != 4242 ||
		p.Binary != "firefox" || p.Media.IconName != "firefox" || p.Media.Title != "Song" || p.Media.Artist != "Band" ||
		p.DeviceIndex != 1 || p.State != StreamRunning || p.BufferLatency != 2*time.Millisecond || p.Format != "PCM" {
		t.Errorf("playback = %+v", p)
	}
	recs := s.RecordingStreams()
	if len(recs) != 1 || recs[0].ApplicationName != "OBS" || recs[0].PID != 0 || recs[0].State != StreamCorked {
		t.Errorf("recording = %+v (a non-numeric pid reads as unset)", recs)
	}

	out, ok := s.DefaultOutput()
	if !ok || out.Name != "speakers" {
		t.Errorf("default output = %v, %v", out.Name, ok)
	}
	in, ok := s.DefaultInput()
	if !ok || in.Name != "mic" {
		t.Errorf("default input = %v, %v", in.Name, ok)
	}
}

func TestSnapshotsDoNotAliasState(t *testing.T) {
	s := connect(t, fixture(t))
	d, _ := s.OutputDevice(1)
	d.Properties["device.description"] = "mutated"
	d.Ports[0].Name = "mutated"
	again, _ := s.OutputDevice(1)
	if again.Properties["device.description"] != "Speakers" || again.Ports[0].Name != "speaker" {
		t.Error("a snapshot shared maps or slices with the service")
	}
}

func TestLookupsReportMissing(t *testing.T) {
	s := connect(t, fixture(t))
	var nf *DeviceNotFoundError
	if _, err := s.OutputDevice(10); !errors.As(err, &nf) || nf.Key.Type != DeviceOutput {
		t.Errorf("OutputDevice(10) = %v (10 is a source)", err)
	}
	if _, err := s.InputDevice(10); err != nil {
		t.Errorf("InputDevice(10) = %v", err)
	}
	var snf *StreamNotFoundError
	if _, err := s.AudioStream(StreamKey{Index: 40, Type: StreamRecord}); !errors.As(err, &snf) {
		t.Errorf("record stream 40 = %v, want not found (40 is playback)", err)
	}
}

func TestConnectFailures(t *testing.T) {
	if _, err := Connect(context.Background(), "unix:"+t.TempDir()+"/none"); err == nil {
		t.Fatal("Connect to a missing socket succeeded")
	}
	srv := fixture(t)
	srv.FailNext(native.CmdGetSourceInfoList, native.ErrAccess)
	if _, err := Connect(context.Background(), srv.Addr()); !errors.Is(err, native.ErrAccess) {
		t.Fatalf("Connect with a failing discovery = %v, want ErrAccess", err)
	}
	waitFor(t, "the failed client to disconnect", func() bool { return srv.Clients() == 0 })
}

func TestEventsFoldIntoState(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ticks, stop, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// A changed sink is re-queried.
	loud := pulsetest.Sink(1, "speakers", "Speakers", 2)
	loud.Volume = native.CVolume{native.VolumeNorm, native.VolumeNorm}
	loud.Mute = true
	srv.PutSink(loud)
	select {
	case <-ticks:
	case <-time.After(5 * time.Second):
		t.Fatal("no tick after a sink change")
	}
	waitFor(t, "the new volume", func() bool {
		d, _ := s.OutputDevice(1)
		return d.Volume.IsNormal() && d.Muted
	})

	// New and removed objects.
	srv.PutSink(pulsetest.Sink(7, "hdmi", "HDMI", 8))
	waitFor(t, "hdmi to appear", func() bool { _, err := s.OutputDevice(7); return err == nil })
	srv.RemoveSink(3)
	waitFor(t, "headphones to go", func() bool { _, err := s.OutputDevice(3); return err != nil })
	srv.PutSinkInput(playback(41, 7))
	waitFor(t, "stream 41", func() bool { return len(s.PlaybackStreams()) == 2 })
	srv.RemoveSinkInput(40)
	waitFor(t, "stream 40 to go", func() bool {
		streams := s.PlaybackStreams()
		return len(streams) == 1 && streams[0].Key.Index == 41
	})
	srv.RemoveSourceOutput(50)
	waitFor(t, "the recording stream to go", func() bool { return len(s.RecordingStreams()) == 0 })

	// A server change re-reads the defaults.
	srv.SetDefaults("hdmi", "speakers.monitor")
	waitFor(t, "the new defaults", func() bool {
		out, ok := s.DefaultOutput()
		in, ok2 := s.DefaultInput()
		return ok && ok2 && out.Name == "hdmi" && in.Name == "speakers.monitor"
	})
}

func TestStaleEventsAreDropped(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	// A change for an object that is already gone (the query answers
	// no such entity) leaves the state alone, and folding continues.
	srv.Emit(native.SubscribeEvent{Facility: native.FacilitySink, Operation: native.OpChange, Index: 99})
	srv.Emit(native.SubscribeEvent{Facility: native.FacilitySinkInput, Operation: native.OpNew, Index: 99})
	srv.Emit(native.SubscribeEvent{Facility: native.FacilitySink, Operation: native.OpRemove, Index: 99})
	srv.PutSink(pulsetest.Sink(7, "hdmi", "HDMI", 8))
	waitFor(t, "hdmi", func() bool { _, err := s.OutputDevice(7); return err == nil })
	if len(s.OutputDevices()) != 3 || len(s.PlaybackStreams()) != 1 {
		t.Errorf("stale events changed the state: %d outputs, %d streams", len(s.OutputDevices()), len(s.PlaybackStreams()))
	}
}

func TestDefaultResolvesByName(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	// The server names a sink the client has not discovered: no
	// default, not the previous one.
	srv.SetDefaults("ghost", "mic")
	waitFor(t, "the ghost default", func() bool { _, ok := s.DefaultOutput(); return !ok })
	if _, err := s.DefaultSink(context.Background()); !errors.Is(err, ErrNoDefaultDevice) {
		t.Errorf("DefaultSink = %v, want ErrNoDefaultDevice", err)
	}
	srv.PutSink(pulsetest.Sink(9, "ghost", "Ghost", 11))
	waitFor(t, "the ghost to resolve", func() bool {
		d, ok := s.DefaultOutput()
		return ok && d.Name == "ghost"
	})
	srv.SetDefaults("", "")
	waitFor(t, "no defaults", func() bool { _, ok := s.DefaultInput(); return !ok })
}

func TestDeviceControlsReachTheServer(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ctx := context.Background()
	out := DeviceKey{Index: 1, Type: DeviceOutput}
	mic := DeviceKey{Index: 10, Type: DeviceInput}

	if err := s.SetDeviceVolume(ctx, out, StereoVolume(0.25, 0.5)); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.Volume[0] != 0x4000 || d.Volume[1] != 0x8000 {
		t.Errorf("per-channel volume on the wire = %v", d.Volume)
	}
	if err := s.SetDeviceVolume(ctx, mic, MonoVolume(0.5)); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Source(10); len(d.Volume) != 1 || d.Volume[0] != 0x8000 {
		t.Errorf("source volume = %v", d.Volume)
	}
	if err := s.SetDeviceMute(ctx, mic, true); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Source(10); !d.Mute {
		t.Error("source mute not applied")
	}
	if err := s.SetDevicePort(ctx, out, "headphones"); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.ActivePort != "headphones" {
		t.Errorf("port = %q", d.ActivePort)
	}
	if err := s.SetDefault(ctx, DeviceKey{Index: 3, Type: DeviceOutput}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefault(ctx, DeviceKey{Index: 2, Type: DeviceInput}); err != nil {
		t.Fatal(err)
	}
	if sinkName, sourceName := srv.Defaults(); sinkName != "headphones" || sourceName != "speakers.monitor" {
		t.Errorf("defaults = %q, %q", sinkName, sourceName)
	}
	// The change comes back through the subscription.
	waitFor(t, "the new default", func() bool { d, ok := s.DefaultOutput(); return ok && d.Name == "headphones" })
}

func TestDeviceControlsRejectUnknownKeys(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ctx := context.Background()
	before := len(srv.Calls())
	missing := DeviceKey{Index: 10, Type: DeviceOutput} // 10 is a source
	for name, err := range map[string]error{
		"volume":  s.SetDeviceVolume(ctx, missing, MonoVolume(1)),
		"mute":    s.SetDeviceMute(ctx, missing, true),
		"port":    s.SetDevicePort(ctx, missing, "x"),
		"default": s.SetDefault(ctx, missing),
	} {
		if _, ok := errors.AsType[*DeviceNotFoundError](err); !ok {
			t.Errorf("%s: %v, want DeviceNotFoundError", name, err)
		}
	}
	if err := s.SetDeviceVolume(ctx, DeviceKey{Index: 1, Type: DeviceOutput}, NewVolume(nil)); !errors.Is(err, ErrEmptyVolume) {
		t.Errorf("empty volume = %v", err)
	}
	if got := srv.Calls()[before:]; len(got) != 0 {
		t.Errorf("rejected controls still sent %v", got)
	}
	srv.FailNext(native.CmdSetSinkMute, native.ErrAccess)
	if err := s.SetDeviceMute(ctx, DeviceKey{Index: 1, Type: DeviceOutput}, true); !errors.Is(err, native.ErrAccess) {
		t.Errorf("server refusal = %v, want ErrAccess", err)
	}
}

func TestStreamControls(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ctx := context.Background()
	play := StreamKey{Index: 40, Type: StreamPlayback}
	rec := StreamKey{Index: 50, Type: StreamRecord}

	if err := s.SetStreamVolume(ctx, play, StereoVolume(0.5, 0.5)); err != nil {
		t.Fatal(err)
	}
	if st, _ := srv.SinkInput(40); st.Volume[0] != 0x8000 {
		t.Errorf("stream volume = %v", st.Volume)
	}
	if err := s.SetStreamMute(ctx, rec, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := srv.SourceOutput(50); !st.Mute {
		t.Error("recording mute not applied")
	}
	if err := s.MoveStream(ctx, play, DeviceKey{Index: 3, Type: DeviceOutput}); err != nil {
		t.Fatal(err)
	}
	if st, _ := srv.SinkInput(40); st.Device != 3 {
		t.Errorf("stream on sink %d, want 3", st.Device)
	}
	waitFor(t, "the move to fold in", func() bool {
		st, _ := s.AudioStream(play)
		return st.DeviceIndex == 3
	})
	if err := s.MoveStream(ctx, rec, DeviceKey{Index: 2, Type: DeviceInput}); err != nil {
		t.Fatal(err)
	}

	if err := s.MoveStream(ctx, play, DeviceKey{Index: 10, Type: DeviceInput}); err == nil {
		t.Error("a playback stream moved to a source")
	}
	var nf *DeviceNotFoundError
	if err := s.MoveStream(ctx, play, DeviceKey{Index: 99, Type: DeviceOutput}); !errors.As(err, &nf) {
		t.Errorf("move to a missing sink = %v", err)
	}
	var snf *StreamNotFoundError
	if err := s.SetStreamMute(ctx, StreamKey{Index: 99, Type: StreamPlayback}, true); !errors.As(err, &snf) {
		t.Errorf("mute of a missing stream = %v", err)
	}
	if err := s.SetStreamVolume(ctx, play, NewVolume(nil)); !errors.Is(err, ErrEmptyVolume) {
		t.Errorf("empty stream volume = %v", err)
	}
}

func TestSeamControlsTheDefaults(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ctx := context.Background()
	if err := s.SetVolume(ctx, 150); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.Volume[0] != native.VolumeNorm || d.Volume[1] != native.VolumeNorm {
		t.Errorf("SetVolume(150) = %v, want clamped to 100%% on both channels", d.Volume)
	}
	if err := s.SetVolume(ctx, -10); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.Volume[0] != 0 {
		t.Errorf("SetVolume(-10) = %v, want 0", d.Volume)
	}
	if err := s.SetMuted(ctx, true); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); !d.Mute {
		t.Error("SetMuted did not mute the default sink")
	}
	if err := s.SetSourceMuted(ctx, true); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Source(10); !d.Mute {
		t.Error("SetSourceMuted did not mute the default source")
	}
	if d, _ := srv.Source(2); d.Mute {
		t.Error("SetSourceMuted touched a non-default source")
	}

	srv.SetDefaults("", "")
	waitFor(t, "no defaults", func() bool { _, ok := s.DefaultOutput(); return !ok })
	for name, err := range map[string]error{
		"volume": s.SetVolume(ctx, 50),
		"mute":   s.SetMuted(ctx, false),
		"source": s.SetSourceMuted(ctx, false),
	} {
		if !errors.Is(err, ErrNoDefaultDevice) {
			t.Errorf("%s without a default = %v", name, err)
		}
	}
}

func TestSubscribeLifetime(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	ticks, _, err := s.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waitClosed(t, ticks, "context cancel")

	ticks, stop, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop() // idempotent
	waitClosed(t, ticks, "stop")

	ticks, _, err = s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv.Disconnect()
	waitClosed(t, ticks, "disconnect")
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close on disconnect")
	}
	if _, _, err := s.Subscribe(context.Background()); err == nil {
		t.Error("Subscribe on a dead service succeeded")
	}
	// The last state stays readable, frozen, like the Rust properties.
	if len(s.OutputDevices()) != 2 {
		t.Error("state vanished with the connection")
	}
}

// waitClosed drains ticks until the channel closes.
func waitClosed(t *testing.T, ticks <-chan struct{}, why string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ticks:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("ticks did not close after %s", why)
		}
	}
}

func TestTicksCoalesce(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	ticks, stop, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i := range 20 {
		d := pulsetest.Sink(1, "speakers", "Speakers", 2)
		d.Volume = native.CVolume{native.Volume(i), native.Volume(i)}
		srv.PutSink(d)
	}
	waitFor(t, "the last change", func() bool {
		d, _ := s.OutputDevice(1)
		return slices.Equal(d.Volume.toPulse(), native.CVolume{19, 19})
	})
	if n := len(ticks); n > 1 {
		t.Errorf("%d ticks queued, want at most one", n)
	}
}
