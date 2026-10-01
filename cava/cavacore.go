package cava

import (
	"math"
)

// Execute is cava_execute: fold new samples into the ring, FFT the
// windowed bass and main buffers per channel, sum each bar's bins
// through the eq, and run cava's two-stage smoothing (gravity falloff
// plus integral) with the autosens feedback clamping bars into 0..1.
// Stereo samples come interleaved, left first. It returns the bar
// heights, one per bar per channel: the left bars, then the right.
//
// The ring is the C one, newest sample first, so the main FFT reads the
// newest stretch of it as cava's does; its reversed time order only
// conjugates the spectrum, whose magnitudes are all that is read.
func (p *Plan) Execute(samples []float64) []float64 {
	newSamples := min(len(samples), len(p.inputBuffer))

	silence := true
	if newSamples > 0 {
		p.framerate -= p.framerate / 64
		// The C divides in integers before the conversion.
		p.framerate += float64(p.rate*p.channels*p.frameSkip/newSamples) / 64
		p.frameSkip = 1
		copy(p.inputBuffer[newSamples:], p.inputBuffer[:len(p.inputBuffer)-newSamples])
		for n := range newSamples {
			p.inputBuffer[newSamples-n-1] = samples[n]
			if samples[n] != 0 {
				silence = false
			}
		}
	} else {
		p.frameSkip++
	}

	out := make([]float64, p.bars*p.channels)
	// In the reversed ring a stereo pair lands right first.
	for ch := range p.channels {
		at := func(n int) float64 { return p.inputBuffer[n] }
		if p.channels == 2 {
			offset := 1 - ch
			at = func(n int) float64 { return p.inputBuffer[n*2+offset] }
		}
		bass := make([]float64, p.bassSize)
		for n := range p.bassSize {
			bass[n] = p.bassMultiplier[n] * at(n)
		}
		main := make([]float64, p.fftSize)
		for n := range p.fftSize {
			main[n] = p.multiplier[n] * at(n)
		}
		bassCoeffs := p.fftBass.Coefficients(nil, bass)
		coeffs := p.fft.Coefficients(nil, main)
		for n := range p.bars {
			var sum float64
			for i := p.lowerCutoff[n]; i <= p.upperCutoff[n]; i++ {
				if n < p.bassCutoffBar {
					sum += cmag(bassCoeffs[i])
				} else {
					sum += cmag(coeffs[i])
				}
			}
			out[ch*p.bars+n] = sum * p.eq[n]
		}
	}

	if p.autosens {
		for n := range out {
			out[n] *= p.sens
		}
	}

	overshoot := false
	gravityMod := math.Pow(60/p.framerate, 2.5) * 1.54 / p.noiseReduction
	if gravityMod < 1 {
		gravityMod = 1
	}

	for n := range out {
		if out[n] < p.prev[n] && p.noiseReduction > 0.1 {
			out[n] = p.peak[n] * (1 - p.fall[n]*p.fall[n]*gravityMod)
			if out[n] < 0 {
				out[n] = 0
			}
			p.fall[n] += 0.028
		} else {
			p.peak[n] = out[n]
			p.fall[n] = 0
		}
		p.prev[n] = out[n]

		out[n] = p.mem[n]*p.noiseReduction + out[n]
		p.mem[n] = out[n]
		if p.autosens && out[n] > 1.0 {
			// The C clamps the output after the integrator stored it, so
			// the memory keeps the unclamped value and the feedback loop
			// sees the overshoot.
			overshoot = true
			out[n] = 1.0
		}
	}

	if p.autosens {
		if overshoot {
			p.sens *= 0.98
			p.sensInit = false
		} else if !silence {
			p.sens *= 1.001
			if p.sensInit {
				p.sens *= 1.1
			}
		}
	}
	return out
}

// InputSize is the sample ring's length, interleaved samples: the most
// one Execute can use, and so the capture backlog cava keeps before
// discarding.
func (p *Plan) InputSize() int { return len(p.inputBuffer) }

// Channels is the plan's channel count, 1 or 2.
func (p *Plan) Channels() int { return p.channels }

// Peaks reports the current per-bar peak values in Execute's order, the
// markers the peaks draw style renders.
func (p *Plan) Peaks() []float64 {
	out := make([]float64, len(p.peak))
	copy(out, p.peak)
	return out
}

// cmag is hypot(re, im) on a Fourier coefficient.
func cmag(c complex128) float64 {
	return math.Hypot(real(c), imag(c))
}
