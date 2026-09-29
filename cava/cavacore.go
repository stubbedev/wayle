package cava

import (
	"math"
)

// Execute is cava_execute: fold new samples into the ring, FFT the
// windowed bass and main buffers, sum each bar's bins through the eq,
// and run cava's two-stage smoothing (gravity falloff plus integral)
// with the autosens feedback clamping bars into 0..1. It returns the
// bar heights, one per bar.
//
// The C ring stores samples time-reversed; that conjugates the spectrum
// and leaves every magnitude untouched, so this port keeps the natural
// order and reads the same magnitudes.
func (p *Plan) Execute(samples []float64) []float64 {
	out := make([]float64, p.bars)
	newSamples := len(samples)
	if newSamples > len(p.inputBuffer) {
		samples = samples[len(samples)-len(p.inputBuffer):]
		newSamples = len(p.inputBuffer)
	}

	silence := true
	if newSamples > 0 {
		p.framerate -= p.framerate / 64
		p.framerate += float64(p.rate*p.frameSkip) / float64(newSamples) / 64
		p.frameSkip = 1
		copy(p.inputBuffer, p.inputBuffer[newSamples:])
		copy(p.inputBuffer[len(p.inputBuffer)-newSamples:], samples)
		for _, s := range samples {
			if s != 0 {
				silence = false
				break
			}
		}
	} else {
		p.frameSkip++
	}

	bass := make([]float64, p.bassSize)
	for n := range p.bassSize {
		bass[n] = p.bassMultiplier[n] * p.inputBuffer[n]
	}
	main := make([]float64, p.fftSize)
	for n := range p.fftSize {
		main[n] = p.multiplier[n] * p.inputBuffer[n]
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
		out[n] = sum * p.eq[n]
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

// Peaks reports the current per-bar peak values, the markers the peaks
// draw style renders.
func (p *Plan) Peaks() []float64 {
	out := make([]float64, p.bars)
	copy(out, p.peak)
	return out
}

// cmag is hypot(re, im) on a Fourier coefficient.
func cmag(c complex128) float64 {
	return math.Hypot(real(c), imag(c))
}
