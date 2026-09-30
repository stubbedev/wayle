package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stubbedev/wayle/internal/dbusx/dbustest"
	"github.com/stubbedev/wayle/service/pulse"
)

// fakeMixer is a PulseAudio server with a fixed device set.
type fakeMixer struct {
	devices  map[pulse.Kind][]pulse.Endpoint
	defaults map[pulse.Kind]string
	setErr   error
}

func (f *fakeMixer) Endpoints(_ context.Context, kind pulse.Kind) ([]pulse.Endpoint, error) {
	return f.devices[kind], nil
}

func (f *fakeMixer) DefaultName(_ context.Context, kind pulse.Kind) (string, error) {
	return f.defaults[kind], nil
}

func (f *fakeMixer) find(kind pulse.Kind, name string) *pulse.Endpoint {
	for i := range f.devices[kind] {
		if f.devices[kind][i].Name == name {
			return &f.devices[kind][i]
		}
	}
	return nil
}

func (f *fakeMixer) SetEndpointVolume(_ context.Context, kind pulse.Kind, name string, percent float64) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.find(kind, name).Volume = percent
	return nil
}

func (f *fakeMixer) SetEndpointMute(_ context.Context, kind pulse.Kind, name string, muted bool) error {
	f.find(kind, name).Muted = muted
	return nil
}

func (f *fakeMixer) SetDefault(_ context.Context, kind pulse.Kind, name string) error {
	f.defaults[kind] = name
	return nil
}

func newFakeMixer() *fakeMixer {
	return &fakeMixer{
		devices: map[pulse.Kind][]pulse.Endpoint{
			pulse.Output: {
				{Index: 1, Name: "hdmi", Description: "HDMI Audio", Volume: 40, State: "Idle"},
				{Index: 2, Name: "speakers", Description: "Built-in Speakers", Volume: 62.5, State: "Running"},
			},
			pulse.Input: {
				{Index: 3, Name: "mic", Description: "Microphone", Volume: 80, Muted: true},
			},
		},
		defaults: map[pulse.Kind]string{pulse.Output: "speakers", pulse.Input: "mic"},
	}
}

func serveAudio(t *testing.T, mixer pulse.Mixer) {
	t.Helper()
	release, err := pulse.ServeDaemon(dbustest.Conn(t), mixer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
}

func TestAudioCommands(t *testing.T) {
	dbustest.SessionBus(t)
	mixer := newFakeMixer()
	serveAudio(t, mixer)

	steps := []struct {
		args []string
		want string
	}{
		{[]string{"audio", "output-volume"}, "Volume: 62%\n"},
		{[]string{"audio", "output-volume", "+5"}, "Volume: 68%\n"},
		{[]string{"audio", "output-volume", "-100"}, "Volume: 0%\n"},
		{[]string{"audio", "output-volume", "150"}, "Volume: 100%\n"},
		{[]string{"audio", "output-mute"}, "Muted\n"},
		{[]string{"audio", "output-volume"}, "Volume: 100% (muted)\n"},
		{[]string{"audio", "output-mute"}, "Unmuted\n"},
		{[]string{"audio", "input-volume"}, "Volume: 80% (muted)\n"},
		{[]string{"audio", "input-volume", "-30"}, "Volume: 50% (muted)\n"},
		{[]string{"audio", "input-mute"}, "Unmuted\n"},
		{[]string{"audio", "sinks"}, "Audio outputs:\n  HDMI Audio\n  Built-in Speakers *\n"},
		{[]string{"audio", "sources"}, "Audio inputs:\n  Microphone *\n"},
		{[]string{"audio", "status"}, "Volume: 100%\nDefault output: speakers\nOutputs: 2, Inputs: 1\n"},
	}
	for _, s := range steps {
		stdout, stderr, code := runCaptured(t, false, s.args...)
		if code != 0 || stdout != s.want {
			t.Errorf("%v: code %d stdout %q stderr %q, want %q", s.args, code, stdout, stderr, s.want)
		}
	}
}

func TestAudioErrors(t *testing.T) {
	dbustest.SessionBus(t)
	if _, stderr, code := runCaptured(t, false, "audio", "status"); code != 1 || stderr != "Error: Audio service not running. Start wayle shell first.\n" {
		t.Errorf("not running: code %d %q", code, stderr)
	}
	mixer := newFakeMixer()
	serveAudio(t, mixer)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"audio", "output-volume", "loud"}, "Error: Invalid volume level: loud\n"},
		{[]string{"audio", "output-volume", "+a"}, "Error: Invalid volume delta: +a\n"},
		{[]string{"audio", "input-volume", "-"}, "Error: Invalid volume delta: -\n"},
	}
	for _, c := range cases {
		if _, stderr, code := runCaptured(t, false, c.args...); code != 1 || stderr != c.want {
			t.Errorf("%v: code %d %q, want %q", c.args, code, stderr, c.want)
		}
	}
	mixer.setErr = errors.New("pactl: boom")
	if _, stderr, _ := runCaptured(t, false, "audio", "output-volume", "10"); stderr != "Error: Failed to set volume: pactl: boom\n" {
		t.Errorf("set failure: %q", stderr)
	}
	mixer.defaults[pulse.Input] = ""
	if _, stderr, _ := runCaptured(t, false, "audio", "input-mute"); stderr != "Error: Failed to toggle input mute: No default input device\n" {
		t.Errorf("no default: %q", stderr)
	}
	// Properties degrade to zero values without a default, as in Rust.
	if stdout, _, _ := runCaptured(t, false, "audio", "input-volume"); stdout != "Volume: 0%\n" {
		t.Errorf("no default volume: %q", stdout)
	}
}

func TestAudioEmptyLists(t *testing.T) {
	dbustest.SessionBus(t)
	serveAudio(t, &fakeMixer{devices: map[pulse.Kind][]pulse.Endpoint{}, defaults: map[pulse.Kind]string{}})
	if stdout, _, _ := runCaptured(t, false, "audio", "sinks"); stdout != "No audio sinks found\n" {
		t.Errorf("sinks: %q", stdout)
	}
	if stdout, _, _ := runCaptured(t, false, "audio", "sources"); stdout != "No audio sources found\n" {
		t.Errorf("sources: %q", stdout)
	}
}
