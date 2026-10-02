package bar

import (
	"context"
	"testing"

	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
	"github.com/stubbedev/wayle/service/recorder"
)

func TestMicrophoneSources(t *testing.T) {
	if got := microphoneSources(nil); len(got) != 1 || got[0] != (recorder.DeviceChoice{ID: "", Label: "Default"}) {
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
	want := []recorder.DeviceChoice{{ID: "", Label: "Default"}, {ID: "mic", Label: "Microphone"}, {ID: "usb", Label: "usb"}}
	if len(got) != len(want) {
		t.Fatalf("= %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
