package pulse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMeanPercentAveragesChannels(t *testing.T) {
	listing := "Volume: front-left: 45054 /  69% / -10.00 dB,   front-right: 45054 /  69% / -10.00 dB\n"
	if got := meanPercent(listing); got < 68.9 || got > 69.1 {
		t.Errorf("mean = %v, want 69", got)
	}
	if got := meanPercent("front-left: 0% / -inf dB"); got != 0 {
		t.Errorf("single channel = %v, want 0", got)
	}
	if got := meanPercent("no percentages here"); got != 0 {
		t.Errorf("no match = %v, want 0", got)
	}
}

// fakePactlScript turns a directory into a PATH where `pactl` is a
// script replying with canned output per subcommand.
func fakePactlScript(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pactl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDefaultSinkReadsShortListAndVolume(t *testing.T) {
	fakePactlScript(t, `case "$1" in
get-default-sink) echo "alsa_output.pci-0000_00_1f.3.analog-stereo" ;;
list) printf 'Sink #45\n\tState: RUNNING\n\tName: alsa_output.pci-0000_00_1f.3.analog-stereo\n\tDescription: Built-in Audio Analog Stereo\n\tDriver: PipeWire\nSink #99\n\tName: other\n' ;;
get-sink-volume) echo "Volume: front-left: 45054 /  69% / -10.00 dB,   front-right: 45054 /  69% / -10.00 dB" ;;
get-sink-mute) echo "Mute: no" ;;
*) echo "unexpected: $*" >&2; exit 1 ;;
esac`)
	p := New()
	dev, err := p.DefaultSink(context.Background())
	if err != nil {
		t.Fatalf("DefaultSink: %v", err)
	}
	if dev.Name != "alsa_output.pci-0000_00_1f.3.analog-stereo" {
		t.Errorf("name = %q", dev.Name)
	}
	if dev.Desc != "Built-in Audio Analog Stereo" {
		t.Errorf("description = %q", dev.Desc)
	}
	if dev.Volume < 68.9 || dev.Volume > 69.1 {
		t.Errorf("volume = %v, want 69", dev.Volume)
	}
	if dev.Muted {
		t.Error("muted = true, want false")
	}
}

func TestDefaultSinkMuted(t *testing.T) {
	fakePactlScript(t, `case "$1" in
get-default-sink) echo "alsa_output.monitor" ;;
get-sink-volume) echo "Volume: front-left: 0% / -inf dB" ;;
get-sink-mute) echo "Mute: yes" ;;
*) exit 1 ;;
esac`)
	p := New()
	dev, err := p.DefaultSink(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !dev.Muted {
		t.Error("muted = false, want true")
	}
	if dev.Volume != 0 {
		t.Errorf("volume = %v, want 0", dev.Volume)
	}
}

func TestSetVolumeClamps(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	fakePactlScript(t, "case \"$1\" in\nset-sink-volume) echo \"$3\" >> "+calls+" ;;\nesac")
	p := New()
	if err := p.SetVolume(context.Background(), 900); err != nil {
		t.Fatal(err)
	}
	if err := p.SetVolume(context.Background(), -5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "100%\n0%\n" {
		t.Errorf("volume calls = %q, want clamped 100%% then 0%%", data)
	}
}

func TestSetMutedPassesState(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	fakePactlScript(t, "case \"$1\" in\nset-sink-mute) echo \"$3\" >> "+calls+" ;;\nesac")
	p := New()
	if err := p.SetMuted(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := p.SetMuted(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1\n0\n" {
		t.Errorf("mute calls = %q, want 1 then 0", data)
	}
}
