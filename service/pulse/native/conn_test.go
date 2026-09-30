package native_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

func speakers() native.DeviceInfo {
	return native.DeviceInfo{
		Index: 1, Name: "alsa_output.speakers", Description: "Speakers",
		SampleSpec:  native.SampleSpec{Format: native.SampleS16LE, Channels: 2, Rate: 48000},
		ChannelMap:  native.ChannelMap{native.ChannelFrontLeft, native.ChannelFrontRight},
		OwnerModule: 4, Volume: native.CVolume{0x8000, 0x10000}, Monitor: 2,
		MonitorName: "alsa_output.speakers.monitor", Driver: "alsa", Flags: 3,
		Props:      native.PropList{"device.description": "Speakers"},
		BaseVolume: native.VolumeNorm, State: native.StateIdle, VolumeSteps: 65537,
		Card: native.InvalidIndex,
		Ports: []native.Port{
			{Name: "analog-output-speaker", Description: "Speaker", Priority: 100, Available: native.PortAvailableYes},
			{Name: "analog-output-headphones", Description: "Headphones", Priority: 200, Available: native.PortAvailableNo},
		},
		ActivePort: "analog-output-speaker",
		Formats:    []native.FormatInfo{{Encoding: native.EncodingPCM, Props: native.PropList{}}},
	}
}

func monitor() native.DeviceInfo {
	d := speakers()
	d.Index, d.Name, d.Description = 2, "alsa_output.speakers.monitor", "Monitor of Speakers"
	d.Monitor, d.MonitorName = 1, "alsa_output.speakers"
	d.Ports, d.ActivePort = nil, ""
	return d
}

func dial(t *testing.T, srv *pulsetest.Server, opts native.Options) *native.Conn {
	t.Helper()
	opts.Server = srv.Addr()
	c, err := native.Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestDialNegotiatesVersionAndIntrospects(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSink(speakers())
	srv.PutSource(monitor())
	srv.SetDefaults("alsa_output.speakers", "alsa_output.speakers.monitor")
	c := dial(t, srv, native.Options{ClientName: "wayle-test"})
	if c.Version() != native.ProtocolVersion {
		t.Errorf("negotiated %d, want min(35, %d)", c.Version(), native.ProtocolVersion)
	}

	info, err := c.ServerInfo(context.Background())
	if err != nil || info.DefaultSink != "alsa_output.speakers" || info.PackageName != "pulsetest" {
		t.Fatalf("ServerInfo = %+v, %v", info, err)
	}
	sinks, err := c.Sinks(context.Background())
	if err != nil || len(sinks) != 1 {
		t.Fatalf("Sinks = %v, %v", sinks, err)
	}
	got, want := sinks[0], speakers()
	if got.Name != want.Name || got.Description != want.Description || got.Volume[0] != 0x8000 ||
		got.MonitorName != want.MonitorName || got.State != native.StateIdle || got.Card != native.InvalidIndex ||
		len(got.Ports) != 2 || got.Ports[1].Available != native.PortAvailableNo || got.ActivePort != want.ActivePort ||
		len(got.Formats) != 1 || got.Props["device.description"] != "Speakers" {
		t.Errorf("sink = %+v", got)
	}
	src, err := c.Source(context.Background(), 2)
	if err != nil || src.Monitor != 1 || src.MonitorName != "alsa_output.speakers" {
		t.Errorf("Source(2) = %+v, %v", src, err)
	}
}

func TestDialRejectsOldServer(t *testing.T) {
	srv := pulsetest.New(t)
	srv.SetVersion(native.MinServerVersion - 1)
	_, err := native.Dial(context.Background(), native.Options{Server: srv.Addr()})
	if err == nil {
		t.Fatal("a protocol-23 server was accepted")
	}
	srv.SetVersion(native.MinServerVersion)
	c, err := native.Dial(context.Background(), native.Options{Server: srv.Addr()})
	if err != nil {
		t.Fatalf("a protocol-24 server was refused: %v", err)
	}
	if c.Version() != native.MinServerVersion {
		t.Errorf("negotiated %d, want %d", c.Version(), native.MinServerVersion)
	}
	_ = c.Close()
}

func TestDialReportsAuthFailure(t *testing.T) {
	srv := pulsetest.New(t)
	srv.FailNext(native.CmdAuth, native.ErrAccess)
	_, err := native.Dial(context.Background(), native.Options{Server: srv.Addr()})
	if !errors.Is(err, native.ErrAccess) {
		t.Fatalf("Dial = %v, want ErrAccess", err)
	}
}

func TestDialTriesEachServer(t *testing.T) {
	srv := pulsetest.New(t)
	missing := "unix:" + filepath.Join(t.TempDir(), "absent")
	c, err := native.Dial(context.Background(), native.Options{Server: missing + " " + srv.Addr()})
	if err != nil {
		t.Fatalf("Dial with a dead first server: %v", err)
	}
	_ = c.Close()
	if _, err := native.Dial(context.Background(), native.Options{Server: missing}); err == nil {
		t.Fatal("Dial of a missing socket succeeded")
	}
}

func TestServerErrorsAreTyped(t *testing.T) {
	srv := pulsetest.New(t)
	c := dial(t, srv, native.Options{})
	_, err := c.Sink(context.Background(), 99)
	if !errors.Is(err, native.ErrNoEntity) {
		t.Fatalf("Sink(99) = %v, want ErrNoEntity", err)
	}
	srv.PutSink(speakers())
	srv.FailNext(native.CmdSetSinkMute, native.ErrAccess)
	if err := c.SetSinkMute(context.Background(), 1, true); !errors.Is(err, native.ErrAccess) {
		t.Fatalf("SetSinkMute = %v, want ErrAccess", err)
	}
	if d, _ := srv.Sink(1); d.Mute {
		t.Error("a refused mute was applied")
	}
	// The connection survives a server error.
	if err := c.SetSinkMute(context.Background(), 1, true); err != nil {
		t.Fatalf("SetSinkMute after an error: %v", err)
	}
	if d, _ := srv.Sink(1); !d.Mute {
		t.Error("mute not applied")
	}
}

func TestControlCommandsReachTheServer(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSink(speakers())
	other := speakers()
	other.Index, other.Name = 5, "bt_headset"
	srv.PutSink(other)
	srv.PutSinkInput(native.StreamInfo{Index: 40, Name: "music", Device: 1, Volume: native.CVolume{1, 1}})
	c := dial(t, srv, native.Options{})
	ctx := context.Background()

	if err := c.SetSinkVolume(ctx, 1, native.CVolume{0x4000, 0x2000}); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.Volume[0] != 0x4000 || d.Volume[1] != 0x2000 {
		t.Errorf("sink volume = %v", d.Volume)
	}
	if err := c.SetSinkPort(ctx, 1, "analog-output-headphones"); err != nil {
		t.Fatal(err)
	}
	if d, _ := srv.Sink(1); d.ActivePort != "analog-output-headphones" {
		t.Errorf("port = %q", d.ActivePort)
	}
	if err := c.SetDefaultSink(ctx, "bt_headset"); err != nil {
		t.Fatal(err)
	}
	if sink, _ := srv.Defaults(); sink != "bt_headset" {
		t.Errorf("default = %q", sink)
	}
	if err := c.MoveSinkInput(ctx, 40, 5); err != nil {
		t.Fatal(err)
	}
	if st, _ := srv.SinkInput(40); st.Device != 5 {
		t.Errorf("stream on sink %d, want 5", st.Device)
	}
	if err := c.SetSinkInputMute(ctx, 40, true); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveSinkInput(ctx, 40, 77); !errors.Is(err, native.ErrNoEntity) {
		t.Errorf("move to a missing sink = %v, want ErrNoEntity", err)
	}
	if err := c.SetDefaultSink(ctx, "a\x00b"); err == nil {
		t.Error("a NUL-bearing name was sent")
	}
}

func TestSubscriptionEventsArriveInOrder(t *testing.T) {
	srv := pulsetest.New(t)
	var mu sync.Mutex
	var got []native.SubscribeEvent
	seen := make(chan struct{}, 16)
	c := dial(t, srv, native.Options{OnSubscribe: func(ev native.SubscribeEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
		seen <- struct{}{}
	}})
	if err := c.Subscribe(context.Background(), native.MaskSink|native.MaskServer); err != nil {
		t.Fatal(err)
	}
	srv.PutSink(speakers())
	srv.PutSource(monitor()) // not subscribed: never delivered
	srv.PutSink(speakers())
	srv.RemoveSink(1)
	srv.SetDefaults("", "")
	for range 4 {
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			t.Fatal("events did not arrive")
		}
	}
	want := []native.SubscribeEvent{
		{Facility: native.FacilitySink, Operation: native.OpNew, Index: 1},
		{Facility: native.FacilitySink, Operation: native.OpChange, Index: 1},
		{Facility: native.FacilitySink, Operation: native.OpRemove, Index: 1},
		{Facility: native.FacilityServer, Operation: native.OpChange, Index: native.InvalidIndex},
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("events = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDisconnectFailsTheConnection(t *testing.T) {
	srv := pulsetest.New(t)
	c := dial(t, srv, native.Options{})
	srv.Disconnect()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close on disconnect")
	}
	if c.Err() == nil {
		t.Error("Err is nil after disconnect")
	}
	if _, err := c.Sinks(context.Background()); err == nil {
		t.Error("a request on a dead connection succeeded")
	}
}

func TestMalformedFrameFailsTheConnection(t *testing.T) {
	srv := pulsetest.New(t)
	c := dial(t, srv, native.Options{})
	// A descriptor announcing a 32 MiB frame is a protocol violation.
	srv.SendRaw([]byte{0x02, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("an oversized frame did not end the connection")
	}
	if !errors.Is(c.Err(), native.ErrProtocol) {
		t.Errorf("Err = %v, want ErrProtocol", c.Err())
	}
}

func TestUnknownServerPacketsAreIgnored(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSink(speakers())
	c := dial(t, srv, native.Options{})
	// An unknown command and a reply for a tag nobody waits on.
	srv.SendFrame(native.Frame{Channel: native.ControlChannel, Payload: native.EncodePacket(200, native.ControlChannel, nil)})
	srv.SendFrame(native.Frame{Channel: native.ControlChannel, Payload: native.EncodePacket(native.CmdReply, 12345, nil)})
	// Audio on a channel with no stream.
	srv.SendFrame(native.Frame{Channel: 9, Payload: []byte{1, 2}})
	if _, err := c.Sinks(context.Background()); err != nil {
		t.Fatalf("connection broke on ignorable packets: %v", err)
	}
}

func TestMalformedReplyFailsOnlyThatRequest(t *testing.T) {
	// A hand-rolled server answers SET_CLIENT_NAME, then replies to the
	// next request with a body that does not decode as a sink list.
	dir := t.TempDir()
	path := filepath.Join(dir, "native")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		for i := 0; ; i++ {
			f, err := native.ReadFrame(nc)
			if err != nil {
				return
			}
			pkt, _ := native.DecodePacket(f.Payload)
			var w native.Writer
			switch i {
			case 0:
				w.U32(35)
			case 1:
				w.U32(1)
			case 2:
				w.U32(1) // a sink list truncated after the index
			}
			_ = native.WriteFrame(nc, native.Frame{Channel: native.ControlChannel, Payload: native.EncodePacket(native.CmdReply, pkt.Tag, w.Bytes())})
		}
	}()
	c, err := native.Dial(context.Background(), native.Options{Server: "unix:" + path})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Sinks(context.Background()); !errors.Is(err, native.ErrMalformed) {
		t.Fatalf("Sinks = %v, want ErrMalformed", err)
	}
	select {
	case <-c.Done():
		t.Fatal("a malformed reply killed the connection")
	default:
	}
}

func TestRequestTimesOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "native")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		for i := 0; ; i++ {
			f, err := native.ReadFrame(nc)
			if err != nil {
				return
			}
			if i >= 2 {
				continue // swallow every request after the handshake
			}
			pkt, _ := native.DecodePacket(f.Payload)
			var w native.Writer
			w.U32(35)
			_ = native.WriteFrame(nc, native.Frame{Channel: native.ControlChannel, Payload: native.EncodePacket(native.CmdReply, pkt.Tag, w.Bytes())})
		}
	}()
	c, err := native.Dial(context.Background(), native.Options{Server: "unix:" + path, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	if _, err := c.ServerInfo(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ServerInfo = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the default timeout did not bound the request")
	}
}

func TestRecordStreamDeliversAudioAndEnds(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSink(speakers())
	srv.PutSource(monitor())
	srv.SetDefaults("alsa_output.speakers", "alsa_output.speakers.monitor")
	c := dial(t, srv, native.Options{})

	chunks := make(chan []byte, 4)
	params := native.RecordParams{
		SampleSpec: native.SampleSpec{Format: native.SampleS16LE, Channels: 2, Rate: 44100},
		ChannelMap: native.ChannelMap{native.ChannelFrontLeft, native.ChannelFrontRight},
		Source:     "alsa_output.speakers.monitor",
		FragSize:   2048,
		Props:      native.PropList{"media.name": "cava"},
	}
	stream, err := c.CreateRecordStream(context.Background(), params, func(b []byte) { chunks <- b })
	if err != nil {
		t.Fatal(err)
	}
	recs := srv.WaitRecords(t, 1)
	rec := recs[0]
	if rec.Source != "alsa_output.speakers.monitor" || rec.SampleSpec.Rate != 44100 || rec.FragSize != 2048 ||
		!rec.AdjustLatency || rec.Props["media.name"] != "cava" {
		t.Errorf("server saw %+v", rec)
	}
	if stream.Info().SourceName != "alsa_output.speakers.monitor" || stream.Info().FragSize != 2048 {
		t.Errorf("granted %+v", stream.Info())
	}
	rec.Push([]byte{1, 2, 3, 4})
	select {
	case b := <-chunks:
		if !bytes.Equal(b, []byte{1, 2, 3, 4}) {
			t.Errorf("chunk = %v", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no audio")
	}

	rec.Kill()
	select {
	case <-stream.Ended():
	case <-time.After(5 * time.Second):
		t.Fatal("a killed stream did not end")
	}
	if !errors.Is(stream.Err(), native.ErrStreamKilled) {
		t.Errorf("Err = %v, want ErrStreamKilled", stream.Err())
	}
	if err := stream.Close(context.Background()); err != nil {
		t.Errorf("Close after kill: %v", err)
	}
}

func TestRecordStreamCloseDeletesIt(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSource(monitor())
	srv.SetDefaults("", "alsa_output.speakers.monitor")
	c := dial(t, srv, native.Options{})
	params := native.RecordParams{
		SampleSpec: native.SampleSpec{Format: native.SampleS16LE, Channels: 1, Rate: 44100},
		ChannelMap: native.ChannelMap{native.ChannelMono},
	}
	stream, err := c.CreateRecordStream(context.Background(), params, func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	if recs := srv.WaitRecords(t, 1); recs[0].Source != "" {
		t.Errorf("default-source stream sent name %q", recs[0].Source)
	}
	if err := stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.WaitRecords(t, 0)
	if !errors.Is(stream.Err(), native.ErrClosed) {
		t.Errorf("Err = %v, want ErrClosed", stream.Err())
	}
}

func TestRecordStreamErrors(t *testing.T) {
	srv := pulsetest.New(t)
	c := dial(t, srv, native.Options{})
	good := native.RecordParams{
		SampleSpec: native.SampleSpec{Format: native.SampleS16LE, Channels: 1, Rate: 44100},
		ChannelMap: native.ChannelMap{native.ChannelMono},
		Source:     "nope",
	}
	if _, err := c.CreateRecordStream(context.Background(), good, func([]byte) {}); !errors.Is(err, native.ErrNoEntity) {
		t.Errorf("missing source = %v, want ErrNoEntity", err)
	}
	bad := good
	bad.ChannelMap = nil
	if _, err := c.CreateRecordStream(context.Background(), bad, func([]byte) {}); err == nil {
		t.Error("a channel map mismatch was sent")
	}
	if _, err := c.CreateRecordStream(context.Background(), good, nil); err == nil {
		t.Error("a nil data handler was accepted")
	}
}

func TestDisconnectEndsRecordStreams(t *testing.T) {
	srv := pulsetest.New(t)
	srv.PutSource(monitor())
	c := dial(t, srv, native.Options{})
	params := native.RecordParams{
		SampleSpec: native.SampleSpec{Format: native.SampleS16LE, Channels: 1, Rate: 44100},
		ChannelMap: native.ChannelMap{native.ChannelMono},
		Source:     "alsa_output.speakers.monitor",
	}
	stream, err := c.CreateRecordStream(context.Background(), params, func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	srv.Disconnect()
	select {
	case <-stream.Ended():
	case <-time.After(5 * time.Second):
		t.Fatal("stream outlived its connection")
	}
	if stream.Err() == nil || errors.Is(stream.Err(), native.ErrStreamKilled) {
		t.Errorf("Err = %v, want the connection error", stream.Err())
	}
}
