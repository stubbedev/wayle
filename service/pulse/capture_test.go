package pulse

import (
	"bytes"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

func cavaSpec(channels uint8) CaptureSpec {
	return CaptureSpec{Name: "cava", Rate: 44100, Channels: channels, FragmentFrames: 512}
}

func startCapture(t *testing.T, s *Service, target CaptureTarget, spec CaptureSpec) (*Capture, chan []byte) {
	t.Helper()
	data := make(chan []byte, 16)
	c, err := s.Capture(target, spec, func(b []byte) { data <- b })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, data
}

func onlyRecord(t *testing.T, srv *pulsetest.Server) *pulsetest.Record {
	t.Helper()
	return srv.WaitRecords(t, 1)[0]
}

func TestCaptureRecordsTheDefaultMonitor(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	c, data := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(2))
	rec := onlyRecord(t, srv)
	if rec.Source != "speakers.monitor" {
		t.Errorf("recording %q, want the default sink's monitor", rec.Source)
	}
	if rec.SampleSpec != (native.SampleSpec{Format: native.SampleS16LE, Channels: 2, Rate: 44100}) {
		t.Errorf("spec = %+v, want S16LE 44100 stereo", rec.SampleSpec)
	}
	if rec.FragSize != 512*4 || rec.Props["media.name"] != "cava" {
		t.Errorf("fragsize %d, props %v", rec.FragSize, rec.Props)
	}
	waitFor(t, "the capture source", func() bool { return c.Source() == "speakers.monitor" })
	rec.Push([]byte{1, 0, 2, 0})
	select {
	case b := <-data:
		if !bytes.Equal(b, []byte{1, 0, 2, 0}) {
			t.Errorf("chunk = %v", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no audio reached the handler")
	}
}

func TestCaptureFollowsTheDefaultSink(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	c, _ := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(1))
	onlyRecord(t, srv)
	srv.SetDefaults("headphones", "mic")
	waitFor(t, "the capture to move", func() bool { return c.Source() == "headphones.monitor" })
	rec := onlyRecord(t, srv)
	if rec.Source != "headphones.monitor" || rec.SampleSpec.Channels != 1 {
		t.Errorf("after the switch: %+v", rec)
	}
}

func TestCaptureTargets(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)

	bySink, err := CaptureNamed("headphones")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := startCapture(t, s, bySink, cavaSpec(1))
	if rec := onlyRecord(t, srv); rec.Source != "headphones.monitor" {
		t.Errorf("a sink name recorded %q, want its monitor", rec.Source)
	}
	c.Close()
	srv.WaitRecords(t, 0)

	bySource, _ := CaptureNamed("mic")
	c, _ = startCapture(t, s, bySource, cavaSpec(1))
	if rec := onlyRecord(t, srv); rec.Source != "mic" {
		t.Errorf("a source name recorded %q", rec.Source)
	}
	c.Close()
	srv.WaitRecords(t, 0)

	c, _ = startCapture(t, s, CaptureDefaultInput(), cavaSpec(1))
	if rec := onlyRecord(t, srv); rec.Source != "mic" {
		t.Errorf("the default input recorded %q", rec.Source)
	}
	c.Close()

	if _, err := CaptureNamed(""); err == nil {
		t.Error("an empty capture name was accepted")
	}
	if _, err := CaptureNamed("a\x00b"); err == nil {
		t.Error("a NUL capture name was accepted")
	}
}

func TestCaptureWaitsForItsTarget(t *testing.T) {
	srv := fixture(t)
	srv.SetDefaults("", "mic")
	s := connect(t, srv)
	c, _ := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(1))
	time.Sleep(50 * time.Millisecond)
	if len(srv.Records()) != 0 || c.Source() != "" {
		t.Fatalf("recording with no default sink: %v", srv.Records())
	}
	srv.SetDefaults("speakers", "mic")
	if rec := onlyRecord(t, srv); rec.Source != "speakers.monitor" {
		t.Errorf("recorded %q once the default appeared", rec.Source)
	}
}

func TestCaptureReopensAfterAKill(t *testing.T) {
	captureRetry = 20 * time.Millisecond
	t.Cleanup(func() { captureRetry = time.Second })
	srv := fixture(t)
	s := connect(t, srv)
	c, _ := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(1))
	first := onlyRecord(t, srv)
	first.Kill()
	waitFor(t, "a new stream", func() bool {
		recs := srv.Records()
		return len(recs) == 1 && recs[0].Channel != first.Channel
	})
	waitFor(t, "the source", func() bool { return c.Source() == "speakers.monitor" })
}

func TestCaptureRetriesAMissingSource(t *testing.T) {
	captureRetry = 20 * time.Millisecond
	t.Cleanup(func() { captureRetry = time.Second })
	srv := fixture(t)
	s := connect(t, srv)
	late, _ := CaptureNamed("late")
	c, _ := startCapture(t, s, late, cavaSpec(1))
	time.Sleep(50 * time.Millisecond)
	if len(srv.Records()) != 0 || c.Source() != "" {
		t.Fatal("recording a source that does not exist")
	}
	srv.PutSource(pulsetest.Source(20, "late", "Late", native.InvalidIndex, ""))
	if rec := onlyRecord(t, srv); rec.Source != "late" {
		t.Errorf("recorded %q", rec.Source)
	}
}

func TestCaptureCloseDeletesTheStream(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	c, _ := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(2))
	onlyRecord(t, srv)
	c.Close()
	c.Close() // idempotent
	srv.WaitRecords(t, 0)
}

func TestCaptureRejectsBadSpecs(t *testing.T) {
	s := connect(t, fixture(t))
	for name, spec := range map[string]CaptureSpec{
		"zero rate":      {Rate: 0, Channels: 1, FragmentFrames: 512},
		"three channels": {Rate: 44100, Channels: 3, FragmentFrames: 512},
		"zero fragment":  {Rate: 44100, Channels: 1},
	} {
		if _, err := s.Capture(CaptureDefaultMonitor(), spec, func([]byte) {}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := s.Capture(CaptureDefaultMonitor(), cavaSpec(1), nil); err == nil {
		t.Error("a nil handler was accepted")
	}
}

func TestCaptureEndsWithTheService(t *testing.T) {
	srv := fixture(t)
	s := connect(t, srv)
	c, _ := startCapture(t, s, CaptureDefaultMonitor(), cavaSpec(1))
	onlyRecord(t, srv)
	srv.Disconnect()
	done := make(chan struct{})
	go func() {
		c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung after the connection died")
	}
}
