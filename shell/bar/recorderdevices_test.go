package bar

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
)

func TestV4L2CapabilitySize(t *testing.T) {
	if got := unsafe.Sizeof(v4l2Capability{}); got != 104 {
		t.Errorf("sizeof = %d, want 104", got)
	}
}

func TestMicrophoneSources(t *testing.T) {
	if got := microphoneSources(nil); len(got) != 1 || got[0] != (deviceChoice{"", "Default"}) {
		t.Errorf("no service = %v", got)
	}
	srv := pulsetest.New(t)
	srv.PutSink(pulsetest.Sink(1, "speakers", "Speakers", 2))
	srv.PutSource(pulsetest.Source(2, "speakers.monitor", "Monitor of Speakers", 1, "speakers"))
	srv.PutSource(pulsetest.Source(10, "mic", "Microphone", native.InvalidIndex, ""))
	srv.PutSource(pulsetest.Source(11, "usb", "", native.InvalidIndex, ""))
	svc, err := pulse.Connect(context.Background(), srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	got := microphoneSources(svc)
	want := []deviceChoice{{"", "Default"}, {"mic", "Microphone"}, {"usb", "usb"}}
	if len(got) != len(want) {
		t.Fatalf("= %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestCameras(t *testing.T) {
	sys := t.TempDir()
	for _, n := range []string{"video2", "video0", "video1", "video10", "media0"} {
		if err := os.Mkdir(filepath.Join(sys, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cards := map[string]string{"/dev/video0": "Integrated", "/dev/video2": "USB Cam", "/dev/video10": "Integrated"}
	probe := func(path string) (string, bool) { c, ok := cards[path]; return c, ok }
	got := cameras(sys, "/dev", probe)
	want := []deviceChoice{{"", "Automatic"}, {"/dev/video0", "Integrated"}, {"/dev/video2", "USB Cam"}}
	if len(got) != len(want) {
		t.Fatalf("= %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if got := cameras(filepath.Join(sys, "missing"), "/dev", probe); len(got) != 1 {
		t.Errorf("no sysfs = %v, want only Automatic", got)
	}
	if _, ok := captureCard(filepath.Join(sys, "not-a-device")); ok {
		t.Error("a missing node probed as a camera")
	}
}

func TestChoiceIndex(t *testing.T) {
	c := []deviceChoice{{"", "Default"}, {"a", "A"}, {"b", "B"}}
	if choiceIndex(c, "b") != 2 || choiceIndex(c, "gone") != 0 || choiceIndex(c, "") != 0 {
		t.Error("choiceIndex")
	}
	if l := choiceLabels(c); len(l) != 3 || l[1] != "A" {
		t.Errorf("labels = %v", l)
	}
}
