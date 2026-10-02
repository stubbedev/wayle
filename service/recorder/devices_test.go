package recorder

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"
)

func TestV4L2CapabilitySize(t *testing.T) {
	if got := unsafe.Sizeof(v4l2Capability{}); got != 104 {
		t.Errorf("sizeof = %d, want 104", got)
	}
}

func TestMicrophoneChoices(t *testing.T) {
	if got := MicrophoneChoices(nil); !slices.Equal(got, []DeviceChoice{{"", "Default"}}) {
		t.Errorf("no sources = %v", got)
	}
	got := MicrophoneChoices([]Source{
		{Name: "speakers.monitor", Description: "Monitor of Speakers", Monitor: true},
		{Name: "mic", Description: "Microphone"},
		{Name: "usb"},
		{Description: "nameless"},
	})
	want := []DeviceChoice{{"", "Default"}, {"mic", "Microphone"}, {"usb", "usb"}}
	if !slices.Equal(got, want) {
		t.Errorf("= %v, want %v", got, want)
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
	want := []DeviceChoice{{"", "Automatic"}, {"/dev/video0", "Integrated"}, {"/dev/video2", "USB Cam"}}
	if !slices.Equal(got, want) {
		t.Fatalf("= %v, want %v", got, want)
	}
	if got := cameras(filepath.Join(sys, "missing"), "/dev", probe); len(got) != 1 {
		t.Errorf("no sysfs = %v, want only Automatic", got)
	}
	if _, ok := captureCard(filepath.Join(sys, "not-a-device")); ok {
		t.Error("a missing node probed as a camera")
	}
}

func TestChoiceIndexLabelsAndSaved(t *testing.T) {
	c := []DeviceChoice{{"", "Default"}, {"a", "A"}, {"b", "B"}}
	if ChoiceIndex(c, "b") != 2 || ChoiceIndex(c, "gone") != 0 || ChoiceIndex(c, "") != 0 {
		t.Error("ChoiceIndex")
	}
	if l := ChoiceLabels(c); !slices.Equal(l, []string{"Default", "A", "B"}) {
		t.Errorf("labels = %v", l)
	}
	if got := WithSaved(c, "gone"); len(got) != 4 || got[3] != (DeviceChoice{"gone", "gone"}) || len(c) != 3 {
		t.Errorf("WithSaved(gone) = %v (input %v)", got, c)
	}
	if got := WithSaved(c, "a"); len(got) != 3 {
		t.Errorf("an offered device was appended: %v", got)
	}
	if got := WithSaved(c, ""); len(got) != 3 {
		t.Errorf("the default was appended: %v", got)
	}
}
