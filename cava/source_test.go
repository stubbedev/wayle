package cava

import (
	"context"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

// audioServer is a fake PulseAudio with one sink (and its monitor)
// set as default, and a connected service.
func audioServer(t *testing.T) (*pulsetest.Server, *pulse.Service) {
	t.Helper()
	srv := pulsetest.New(t)
	spec := native.SampleSpec{Format: native.SampleS16LE, Channels: 2, Rate: 48000}
	channels := native.ChannelMap{native.ChannelFrontLeft, native.ChannelFrontRight}
	srv.PutSink(native.DeviceInfo{
		Index: 1, Name: "speakers", Description: "Speakers", SampleSpec: spec, ChannelMap: channels,
		Volume: native.CVolume{native.VolumeNorm, native.VolumeNorm}, Monitor: 2, MonitorName: "speakers.monitor",
		Props: native.PropList{}, BaseVolume: native.VolumeNorm, Card: native.InvalidIndex,
		Formats: []native.FormatInfo{{Encoding: native.EncodingPCM, Props: native.PropList{}}},
	})
	srv.PutSource(native.DeviceInfo{
		Index: 2, Name: "speakers.monitor", Description: "Monitor of Speakers", SampleSpec: spec, ChannelMap: channels,
		OwnerModule: native.InvalidIndex, Volume: native.CVolume{native.VolumeNorm, native.VolumeNorm},
		Monitor: 1, MonitorName: "speakers", Props: native.PropList{}, BaseVolume: native.VolumeNorm, Card: native.InvalidIndex,
	})
	srv.SetDefaults("speakers", "speakers.monitor")
	svc, err := pulse.Connect(context.Background(), srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return srv, svc
}

func drainUntil(t *testing.T, src *Source, n int) []float64 {
	t.Helper()
	var got []float64
	deadline := time.Now().Add(5 * time.Second)
	for len(got) < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %d samples, want %d", len(got), n)
		}
		got = append(got, src.Drained()...)
		time.Sleep(5 * time.Millisecond)
	}
	return got
}

func TestSourceRecordsTheMonitorAsRawInt16Mono(t *testing.T) {
	srv, svc := audioServer(t)
	src, err := NewSource(svc, "auto", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Start(); err != nil {
		t.Fatal(err)
	}
	defer src.Stop()

	rec := srv.WaitRecords(t, 1)[0]
	if rec.Source != "speakers.monitor" {
		t.Errorf("recording %q, want the default sink's monitor", rec.Source)
	}
	if rec.SampleSpec != (native.SampleSpec{Format: native.SampleS16LE, Channels: 1, Rate: sampleRate}) {
		t.Errorf("spec = %+v, want s16le mono at %d", rec.SampleSpec, sampleRate)
	}
	if rec.FragSize != fragmentFrames*2 {
		t.Errorf("fragsize = %d, want %d frames", rec.FragSize, fragmentFrames)
	}
	// Wait for the stream to be up before pushing into it.
	time.Sleep(20 * time.Millisecond)
	rec.Push([]byte{0x01, 0x00, 0xff, 0xff, 0x00, 0x80})
	got := drainUntil(t, src, 3)
	if got[0] != 1 || got[1] != -1 || got[2] != -32768 {
		t.Errorf("samples = %v, want raw int16 values 1, -1, -32768", got)
	}
	if again := src.Drained(); again != nil {
		t.Errorf("second drain = %v, want nothing new", again)
	}
}

func TestSourceDiscardsTheBacklogOnOverflow(t *testing.T) {
	src := &Source{capacity: 4}
	src.accumulate([]byte{1, 0, 2, 0, 3, 0})
	// Two more would make five: cava drops the stale three.
	src.accumulate([]byte{4, 0, 5, 0})
	if got := src.Drained(); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Errorf("after overflow = %v, want only the new 4, 5", got)
	}
	// One chunk alone larger than the buffer keeps its newest samples.
	src.accumulate([]byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0})
	if got := src.Drained(); len(got) != 4 || got[0] != 3 || got[3] != 6 {
		t.Errorf("oversized chunk = %v, want the newest four", got)
	}
}

func TestNewSourceRejectsBadArguments(t *testing.T) {
	_, svc := audioServer(t)
	if _, err := NewSource(nil, "auto", 16); err == nil {
		t.Error("nil capturer: want an error")
	}
	if _, err := NewSource(svc, "auto", 0); err == nil {
		t.Error("zero capacity: want an error")
	}
	if _, err := NewSource(svc, "bad\x00name", 16); err == nil {
		t.Error("NUL in the source name: want an error")
	}
	named, err := NewSource(svc, "speakers", 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := named.Start(); err != nil {
		t.Fatal(err)
	}
	defer named.Stop()
	if err := named.Start(); err == nil {
		t.Error("second Start: want an error")
	}
}
