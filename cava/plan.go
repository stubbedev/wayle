// Package cava is the Go port of the audio spectrum analyzer behind
// wayle's cava bar module: crates/wayle-cava's vendored cavacore.c
// translated algorithm-for-algorithm. The plan distributes bars across
// the frequency range on a log scale (with the bass split onto a
// double-size FFT), Execute turns PCM frames into 0..1 bar heights with
// cava's gravity falloff and integral smoothing, and the autosens
// feedback keeps the output normalized. Capture is a record stream on
// the shell's PulseAudio native client (source.go).
package cava

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Plan holds the analyzer state for one bar layout: one cava_init call.
// It is not safe for concurrent use; Execute owns it from the loop
// goroutine.
type Plan struct {
	bars           int
	rate           int
	lowCutoff      int
	highCutoff     int
	fftSize        int
	bassSize       int
	bassCutoffBar  int
	lowerCutoff    []int
	upperCutoff    []int
	eq             []float64
	bassMultiplier []float64
	multiplier     []float64

	fftBass *fourier.FFT
	fft     *fourier.FFT

	inputBuffer []float64

	sens      float64
	sensInit  bool
	framerate float64
	frameSkip int

	fall []float64
	mem  []float64
	peak []float64
	prev []float64

	noiseReduction float64
	autosens       bool
}

const bassCutoffHz = 100.0

// NewPlan initializes the analyzer: bars over [lowCutoff, highCutoff]
// Hz at the given sample rate, one channel. noiseReduction is cava's
// 0..1 filter strength and autosens enables the feedback that keeps
// bars in 0..1. The validations mirror cava_init's; each failure is an
// error naming the bad value.
func NewPlan(bars, rate int, noiseReduction float64, autosens bool, lowCutoff, highCutoff int) (*Plan, error) {
	switch {
	case rate < 1 || rate > 384000:
		return nil, fmt.Errorf("cava: illegal sample rate %d", rate)
	case bars < 1:
		return nil, fmt.Errorf("cava: illegal bar count %d", bars)
	case lowCutoff < 1 || highCutoff < 1:
		return nil, fmt.Errorf("cava: cutoffs must be positive (got low %d, high %d)", lowCutoff, highCutoff)
	case lowCutoff >= highCutoff:
		return nil, fmt.Errorf("cava: high cutoff %d must exceed low cutoff %d", highCutoff, lowCutoff)
	case highCutoff > rate/2:
		return nil, fmt.Errorf("cava: high cutoff %d exceeds Nyquist %d", highCutoff, rate/2)
	}

	fftSize := 512
	switch {
	case rate > 300000:
		fftSize *= 64
	case rate > 150000:
		fftSize *= 32
	case rate > 75000:
		fftSize *= 16
	case rate > 32500:
		fftSize *= 8
	case rate > 16250:
		fftSize *= 4
	case rate > 8125:
		fftSize *= 2
	}
	if bars > fftSize/2+1 {
		return nil, fmt.Errorf("cava: %d bars exceed the %d bin maximum for rate %d", bars, fftSize/2+1, rate)
	}

	bassSize := fftSize * 2
	p := &Plan{
		bars:           bars,
		rate:           rate,
		lowCutoff:      lowCutoff,
		highCutoff:     highCutoff,
		fftSize:        fftSize,
		bassSize:       bassSize,
		lowerCutoff:    make([]int, bars+1),
		upperCutoff:    make([]int, bars+1),
		eq:             make([]float64, bars+1),
		bassMultiplier: hann(bassSize),
		multiplier:     hann(fftSize),
		fftBass:        fourier.NewFFT(bassSize),
		fft:            fourier.NewFFT(fftSize),
		inputBuffer:    make([]float64, bassSize),
		sens:           1.0,
		sensInit:       true,
		framerate:      75.0,
		frameSkip:      1,
		fall:           make([]float64, bars),
		mem:            make([]float64, bars),
		peak:           make([]float64, bars),
		prev:           make([]float64, bars),
		noiseReduction: noiseReduction,
		autosens:       autosens,
	}
	p.computeBands()
	return p, nil
}

// hann returns the Hann window multipliers for n samples.
func hann(n int) []float64 {
	m := make([]float64, n)
	for i := range m {
		m[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return m
}

// computeBands is cavacore.c's cutoff/eq pass: bars distribute
// logarithmically over the range, the bass bars run on the double-size
// FFT, and the spectrum pushes up where the log distribution clumps.
// The float32 arithmetic is load-bearing: cava computes the cutoff
// table in C floats, and the clump detection depends on that precision.
func (p *Plan) computeBands() {
	frequencyConstant := math.Log10(float64(p.lowCutoff)/float64(p.highCutoff)) /
		(1/float64(p.bars+1) - 1)

	relative := make([]float32, p.bars+1)
	cutoffFrequency := make([]float32, p.bars+1)
	minBandwidth := float32(p.rate) / float32(p.bassSize)

	p.bassCutoffBar = 0
	firstBar := true

	for n := 0; n <= p.bars; n++ {
		coefficient := -frequencyConstant +
			float64(n+1)/float64(p.bars+1)*frequencyConstant
		cutoffFrequency[n] = float32(p.highCutoff) * float32(math.Pow(10, coefficient))

		if n > 0 && cutoffFrequency[n-1] >= cutoffFrequency[n] {
			cutoffFrequency[n] = cutoffFrequency[n-1] + minBandwidth
		}

		relative[n] = cutoffFrequency[n] / float32(p.rate/2)

		if cutoffFrequency[n] < bassCutoffHz {
			p.lowerCutoff[n] = int(relative[n] * float32(p.bassSize/2))
			p.bassCutoffBar++
			if p.bassCutoffBar > 1 {
				firstBar = false
			}
			if p.lowerCutoff[n] > p.bassSize/2 {
				p.lowerCutoff[n] = p.bassSize / 2
			}
		} else {
			p.lowerCutoff[n] = int(math.Ceil(float64(relative[n] * float32(p.fftSize/2))))
			if n == p.bassCutoffBar {
				firstBar = true
				if n > 0 {
					p.upperCutoff[n-1] = int(relative[n]*float32(p.bassSize/2)) - 1
				}
			} else {
				firstBar = false
			}
			if p.lowerCutoff[n] > p.fftSize/2 {
				p.lowerCutoff[n] = p.fftSize / 2
			}
		}

		if n > 0 {
			if !firstBar {
				p.upperCutoff[n-1] = p.lowerCutoff[n] - 1

				// Push the spectrum up where the exponential clumps.
				if p.lowerCutoff[n] <= p.lowerCutoff[n-1] {
					roomForMore := false
					if n < p.bassCutoffBar {
						roomForMore = p.lowerCutoff[n-1]+1 < p.bassSize/2+1
					} else {
						roomForMore = p.lowerCutoff[n-1]+1 < p.fftSize/2+1
					}
					if roomForMore {
						p.lowerCutoff[n] = p.lowerCutoff[n-1] + 1
						p.upperCutoff[n-1] = p.lowerCutoff[n] - 1
					}
				}
			} else if p.upperCutoff[n-1] < p.lowerCutoff[n-1] {
				p.upperCutoff[n-1] = p.lowerCutoff[n-1] + 1
			}
		}

		// Recompute the actual cutoff from the final bin assignment.
		if n < p.bassCutoffBar {
			relative[n] = float32(p.lowerCutoff[n]) / float32(p.bassSize/2)
		} else {
			relative[n] = float32(p.lowerCutoff[n]) / float32(p.fftSize/2)
		}
		cutoffFrequency[n] = relative[n] * float32(p.rate/2)
	}

	// The hardcoded eq normalizes the FFT output and boosts treble.
	for n := range p.bars {
		p.eq[n] = 1 / math.Pow(2, 28)
		p.eq[n] *= math.Pow(float64(cutoffFrequency[n+1]), 0.8)
		if n < p.bassCutoffBar {
			p.eq[n] /= math.Log2(float64(p.bassSize))
		} else {
			p.eq[n] /= math.Log2(float64(p.fftSize))
		}
		p.eq[n] /= float64(p.upperCutoff[n] - p.lowerCutoff[n] + 1)
	}
}
