package cava

import (
	"math"
	"testing"
)

func TestNewPlanValidations(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		bars, rate, low, high int
		noise                 float64
		wantErr               string
	}{
		{"defaults", 20, 44100, 50, 17000, 0.65, ""},
		{"illegal rate", 20, 0, 50, 17000, 0.65, "rate"},
		{"zero bars", 0, 44100, 50, 17000, 0.65, "bars"},
		{"low not positive", 20, 44100, 0, 17000, 0.65, "cutoff"},
		{"low above high", 20, 44100, 17000, 50, 0.65, "exceed"},
		{"above nyquist", 20, 44100, 50, 25000, 0.65, "Nyquist"},
		{"too many bars", 3000, 44100, 50, 17000, 0.65, "bars"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPlan(tc.bars, tc.rate, 1, tc.noise, true, tc.low, tc.high)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want success, got %v", err)
				}
			} else if err == nil {
				t.Fatalf("want a %q error, got nil", tc.wantErr)
			}
		})
	}
}

func TestPlanBandTable(t *testing.T) {
	plan, err := NewPlan(20, 44100, 1, 0.65, true, 50, 17000)
	if err != nil {
		t.Fatal(err)
	}
	// cavacore.c's table genuinely dips at the bass/mid boundary: the
	// mid bars run on the half-size FFT, so bin indices reset. What must
	// hold is monotonicity within each FFT region and nonempty bands.
	for n := 1; n < plan.bassCutoffBar; n++ {
		if plan.lowerCutoff[n] < plan.lowerCutoff[n-1] {
			t.Errorf("bass bar %d lower cutoff %d below bar %d's %d",
				n, plan.lowerCutoff[n], n-1, plan.lowerCutoff[n-1])
		}
	}
	for n := plan.bassCutoffBar + 1; n <= plan.bars; n++ {
		if plan.lowerCutoff[n] < plan.lowerCutoff[n-1] {
			t.Errorf("mid bar %d lower cutoff %d below bar %d's %d",
				n, plan.lowerCutoff[n], n-1, plan.lowerCutoff[n-1])
		}
	}
	for n := range plan.bars {
		if plan.upperCutoff[n] < plan.lowerCutoff[n] {
			t.Errorf("bar %d band [%d,%d] is empty", n, plan.lowerCutoff[n], plan.upperCutoff[n])
		}
	}
	if plan.bassCutoffBar == 0 {
		t.Error("no bass bars: the 50 Hz cutoff should land bars on the bass FFT")
	}
	// The exact boundary values for the default layout, so a porting
	// regression in the float32 cutoff arithmetic shows up here.
	wantBoundary := [4]int{
		plan.lowerCutoff[plan.bassCutoffBar-1], plan.upperCutoff[plan.bassCutoffBar-1],
		plan.lowerCutoff[plan.bassCutoffBar], plan.upperCutoff[plan.bassCutoffBar],
	}
	if wantBoundary != [4]int{16, 21, 12, 14} {
		t.Errorf("bass/mid boundary = %v, want [16 21 12 14] (the cavacore.c values)", wantBoundary)
	}
	for n := range plan.bars {
		if plan.eq[n] <= 0 {
			t.Errorf("eq[%d] = %v, want positive", n, plan.eq[n])
		}
	}
}

// sine fills the buffer with one cycle-per-buffer-normalized sine.
func sine(n int, hz, rate float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Sin(2 * math.Pi * hz / rate * float64(i))
	}
	return out
}

func TestExecuteFindsEnergyInTheRightBand(t *testing.T) {
	plan, err := NewPlan(20, 44100, 1, 0, true, 50, 17000)
	if err != nil {
		t.Fatal(err)
	}
	const toneHz = 1000.0
	// Prime the ring with the tone, then measure a settled frame.
	for range 8 {
		for range 512 {
			plan.Execute(sine(plan.bassSize, toneHz, 44100)[:512])
		}
		bars := plan.Execute(sine(512, toneHz, 44100))
		best, bestV := 0, 0.0
		for i, v := range bars {
			if v > bestV {
				best, bestV = i, v
			}
		}
		low := float64(plan.lowerCutoff[best]) * 44100 / float64(plan.fftSize)
		high := float64(plan.upperCutoff[best]+1) * 44100 / float64(plan.fftSize)
		if toneHz < low || toneHz >= high {
			t.Fatalf("tone at %d Hz landed in band %d [%v, %v) Hz", int(toneHz), best, low, high)
		}
		if bestV <= 0 {
			t.Fatalf("loudest band %d carries no energy", best)
		}
	}
}

func TestExecuteSilenceStaysFlat(t *testing.T) {
	plan, err := NewPlan(10, 44100, 1, 0.65, true, 50, 17000)
	if err != nil {
		t.Fatal(err)
	}
	bars := plan.Execute(make([]float64, plan.bassSize))
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("silence: bar %d = %v, want 0", i, v)
		}
	}
}

func TestExecuteAutosensClampsToOne(t *testing.T) {
	plan, err := NewPlan(10, 44100, 1, 0.65, true, 50, 17000)
	if err != nil {
		t.Fatal(err)
	}
	// A full-scale square wave drives every band hard; the feedback must
	// pull the bars back into 0..1.
	loud := make([]float64, plan.bassSize)
	for i := range loud {
		loud[i] = math.Sin(2*math.Pi*float64(i)/24) * 100
	}
	for range 2000 {
		for range 8 {
			bars := plan.Execute(loud)
			for _, v := range bars {
				if v < 0 || v > 1.0+1e-9 {
					t.Fatalf("autosens let a bar reach %v", v)
				}
			}
		}
	}
}

func TestNoiseReductionFallsOffAfterSilence(t *testing.T) {
	plan, err := NewPlan(10, 44100, 1, 0.65, true, 50, 17000)
	if err != nil {
		t.Fatal(err)
	}
	tone := sine(plan.bassSize, 440, 44100)
	for i := range tone {
		tone[i] *= 1000 // loud enough that autosens clamps and stabilizes
	}
	clamped := false
	var peakValue float64
	for range 300 {
		for _, v := range plan.Execute(tone) {
			peakValue = max(peakValue, v)
			clamped = clamped || v >= 1.0
		}
	}
	if !clamped {
		t.Fatal("loud tone never clamped: autosens did not stabilize the bars")
	}
	// After the tone stops: 16 frames flush the 8192-sample ring, and
	// the tail transiently re-clamps while the integrator drains. What
	// defines falloff is the decay: by frame 100 the bars must be well
	// below the clamped peak, and they keep decaying from there.
	var after float64
	for i := range 250 {
		bars := plan.Execute(make([]float64, 512))
		if i >= 100 {
			for _, v := range bars {
				after = max(after, v)
			}
		}
	}
	if after >= peakValue/2 {
		t.Errorf("bars did not fall off: after silence %v >= peak/2 %v", after, peakValue/2)
	}
}
