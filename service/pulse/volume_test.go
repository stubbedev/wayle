package pulse

import (
	"errors"
	"math"
	"testing"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// near absorbs the truncation of the raw conversion (0.4 reads back
// as 0.39999...).
func near(a, b float64) bool { return math.Abs(a-b) < 1e-2 }

func TestNewVolumeClamps(t *testing.T) {
	v := NewVolume([]float64{-1, 0.5, 2, 3.5, 5, 10})
	want := []float64{0, 0.5, 2, 3.5, 4, 4}
	for i, w := range want {
		if got, _ := v.Channel(i); got != w {
			t.Errorf("channel %d = %v, want %v", i, got, w)
		}
	}
}

func TestVolumeWithAmplificationValidates(t *testing.T) {
	v, err := VolumeWithAmplification([]float64{0, 2, 4})
	if err != nil || v.Channels() != 3 {
		t.Fatalf("valid range = %v, %v", v, err)
	}
	for _, bad := range []float64{-0.1, 4.1} {
		_, err := VolumeWithAmplification([]float64{1, bad})
		var inv *InvalidVolumeError
		if !errors.As(err, &inv) || inv.Channel != 1 || inv.Volume != bad {
			t.Errorf("level %v: err = %v, want InvalidVolumeError on channel 1", bad, err)
		}
	}
}

func TestVolumeConstructors(t *testing.T) {
	if v := MonoVolume(1.5); v.Channels() != 1 {
		t.Errorf("mono channels = %d", v.Channels())
	}
	s := StereoVolume(0.8, 1.2)
	if l, _ := s.Channel(0); l != 0.8 {
		t.Errorf("left = %v", l)
	}
	if r, _ := s.Channel(1); r != 1.2 {
		t.Errorf("right = %v", r)
	}
	if _, ok := s.Channel(2); ok {
		t.Error("channel 2 of stereo exists")
	}
	if !MutedVolume(3).IsMuted() || MutedVolume(3).Channels() != 3 {
		t.Error("MutedVolume")
	}
	if !NormalVolume(2).IsNormal() {
		t.Error("NormalVolume")
	}
	p := VolumeFromPercentage(50, 2)
	if l, _ := p.Channel(1); l != 0.5 || p.Channels() != 2 {
		t.Errorf("from percentage = %v", p.Levels())
	}
}

func TestVolumeWithChannel(t *testing.T) {
	v := StereoVolume(1, 1)
	next, err := v.WithChannel(0, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := next.Channel(0); l != 0.5 {
		t.Errorf("updated channel = %v", l)
	}
	if l, _ := v.Channel(0); l != 1 {
		t.Error("WithChannel mutated the original")
	}
	clamped, _ := v.WithChannel(1, 9)
	if r, _ := clamped.Channel(1); r != 4 {
		t.Errorf("clamped channel = %v", r)
	}
	var inv *InvalidChannelError
	if _, err := v.WithChannel(5, 1); !errors.As(err, &inv) || inv.Channel != 5 {
		t.Errorf("channel 5: %v, want InvalidChannelError", err)
	}
}

func TestVolumeAverages(t *testing.T) {
	if NewVolume(nil).Average() != 0 {
		t.Error("empty average")
	}
	if !near(NewVolume([]float64{0.5, 1, 1.5, 2}).Average(), 1.25) {
		t.Error("average of four")
	}
	if !near(StereoVolume(0.5, 1).AveragePercentage(), 75) {
		t.Error("average percentage")
	}
	pct := NewVolume([]float64{0.5, 1, 1.5}).ToPercentage()
	if pct[0] != 50 || pct[1] != 100 || pct[2] != 150 {
		t.Errorf("to percentage = %v", pct)
	}
	if NewVolume([]float64{0, 0.1, 0}).IsMuted() {
		t.Error("a non-zero channel read as muted")
	}
	if NewVolume([]float64{1, 0.9}).IsNormal() {
		t.Error("a 0.9 channel read as normal")
	}
	levels := StereoVolume(1, 1).Levels()
	levels[0] = 0
	if l, _ := StereoVolume(1, 1).Channel(0); l != 1 {
		t.Error("Levels aliased the volume")
	}
}

func TestPulseConversions(t *testing.T) {
	if got := NewVolume(nil).toPulse(); len(got) != 0 {
		t.Errorf("empty = %v", got)
	}
	cv := StereoVolume(0.3, 0.8).toPulse()
	if len(cv) != 2 || !near(float64(cv[0])/float64(native.VolumeNorm), 0.3) || !near(float64(cv[1])/float64(native.VolumeNorm), 0.8) {
		t.Errorf("stereo = %v", cv)
	}
	if MonoVolume(1).toPulse()[0] != native.VolumeNorm || MonoVolume(0).toPulse()[0] != native.VolumeMuted {
		t.Error("normal/muted anchors")
	}
	if got := MonoVolume(4).toPulse()[0]; got != native.Volume(4*float64(native.VolumeNorm)) {
		t.Errorf("max = %v", got)
	}
	back := volumeFromPulse(StereoVolume(0.3, 0.8).toPulse())
	if l, _ := back.Channel(0); !near(l, 0.3) {
		t.Errorf("round trip left = %v", l)
	}
	if r, _ := back.Channel(1); !near(r, 0.8) {
		t.Errorf("round trip right = %v", r)
	}
	if v := volumeFromPulse(native.CVolume{native.VolumeNorm, native.VolumeNorm}); !v.IsNormal() || v.Channels() != 2 {
		t.Errorf("from normal = %v", v.Levels())
	}
	// A raw volume past 4.0 (PA allows up to VolumeMax) clamps.
	if v := volumeFromPulse(native.CVolume{native.VolumeMax}); !near(v.Average(), 4) {
		t.Errorf("from max = %v", v.Levels())
	}
	if v := volumeFromPulseSingle(native.VolumeNorm); !v.IsNormal() || v.Channels() != 1 {
		t.Errorf("single = %v", v.Levels())
	}
}
