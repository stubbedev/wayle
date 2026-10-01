package cava

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// oracleSignal is testdata/oracle.c's signal_at: a rising chirp on the
// left, a falling one on the right, under a beating amplitude.
func oracleSignal(ch int, i int64) float64 {
	t := float64(i) / 44100.0
	f := 80.0 + 3000.0*t
	if ch == 1 {
		f = 4000.0 - 3000.0*t
	}
	amp := 8000.0 * (0.6 + 0.4*math.Sin(2.0*math.Pi*1.3*t))
	return amp * math.Sin(2.0*math.Pi*f*t)
}

// readOracle loads one fixture: a frame of bars per line.
func readOracle(t *testing.T, name string) [][]float64 {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var frames [][]float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row []float64
		for field := range strings.FieldsSeq(sc.Text()) {
			v, err := strconv.ParseFloat(field, 64)
			if err != nil {
				t.Fatal(err)
			}
			row = append(row, v)
		}
		frames = append(frames, row)
	}
	return frames
}

// runOracle replays oracle.c's driver on the Go plan: chunks of 512
// interleaved frames, every fifth delivery empty.
func runOracle(t *testing.T, p *Plan, channels, frames int) [][]float64 {
	t.Helper()
	const chunk = 512
	var out [][]float64
	var i int64
	for f := range frames {
		n := chunk
		if f%5 == 4 {
			n = 0
		}
		in := make([]float64, 0, n*channels)
		for range n {
			for c := range channels {
				in = append(in, oracleSignal(c, i))
			}
			i++
		}
		out = append(out, p.Execute(in))
	}
	return out
}

// TestExecuteMatchesTheCOracle holds the port to cavacore.c frame for
// frame, mono and stereo, through autosens, gravity, the integral, and
// starved frames.
func TestExecuteMatchesTheCOracle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bars     int
		channels int
	}{
		{"oracle_mono.txt", 20, 1},
		{"oracle_stereo.txt", 10, 2},
	} {
		want := readOracle(t, tc.name)
		p, err := NewPlan(tc.bars, 44100, tc.channels, 0.77, true, 50, 10000)
		if err != nil {
			t.Fatal(err)
		}
		got := runOracle(t, p, tc.channels, len(want))
		for f := range want {
			if len(got[f]) != len(want[f]) {
				t.Fatalf("%s frame %d: %d values, want %d", tc.name, f, len(got[f]), len(want[f]))
			}
			for b := range want[f] {
				if d := math.Abs(got[f][b] - want[f][b]); d > 1e-9*math.Max(1, math.Abs(want[f][b])) {
					t.Fatalf("%s frame %d bar %d = %.12g, want %.12g", tc.name, f, b, got[f][b], want[f][b])
				}
			}
		}
	}
}
