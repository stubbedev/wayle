package pulse

import (
	"fmt"
	"log"
	"slices"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// Volume limits (volume/types.rs): 1.0 is 100%, above 2.0 may damage
// hardware, 4.0 is the ceiling every constructor clamps to.
const (
	volumeSafeMax = 2.0
	volumeMax     = 4.0
)

// Volume is a per-channel linear volume (volume/types.rs's Volume):
// 0.0 is silent, 1.0 normal, up to 4.0 amplified. It is immutable;
// the With* methods return a changed copy.
type Volume struct {
	levels []float64
}

// NewVolume clamps each channel into 0..4, logging the clamps and the
// levels past the safe 2.0, as Volume::new does.
func NewVolume(levels []float64) Volume {
	out := make([]float64, len(levels))
	for i, v := range levels {
		out[i] = clampLevel(v)
	}
	return Volume{levels: out}
}

func clampLevel(v float64) float64 {
	switch {
	case v > volumeMax:
		log.Printf("pulse: volume %v clamped to maximum (4.0); use values <= 2.0 for safe operation", v)
		return volumeMax
	case v > volumeSafeMax:
		log.Printf("pulse: volume %v exceeds safe limit (2.0); audio damage possible at high amplification", v)
	case v < 0:
		log.Printf("pulse: negative volume %v clamped to 0.0", v)
		return 0
	}
	return v
}

// InvalidVolumeError rejects an out-of-range level (volume/error.rs).
type InvalidVolumeError struct {
	Channel int
	Volume  float64
}

func (e *InvalidVolumeError) Error() string {
	return fmt.Sprintf("invalid volume %v for channel %d (must be 0.0-4.0)", e.Volume, e.Channel)
}

// InvalidChannelError rejects a channel index the volume lacks.
type InvalidChannelError struct {
	Channel int
}

func (e *InvalidChannelError) Error() string {
	return fmt.Sprintf("invalid channel index %d", e.Channel)
}

// VolumeWithAmplification validates instead of clamping
// (Volume::with_amplification): any level outside 0..4 is an error.
func VolumeWithAmplification(levels []float64) (Volume, error) {
	for i, v := range levels {
		if v < 0 || v > volumeMax {
			return Volume{}, &InvalidVolumeError{Channel: i, Volume: v}
		}
	}
	return Volume{levels: slices.Clone(levels)}, nil
}

// MonoVolume is a one-channel volume.
func MonoVolume(v float64) Volume { return NewVolume([]float64{v}) }

// StereoVolume is a left/right volume.
func StereoVolume(left, right float64) Volume { return NewVolume([]float64{left, right}) }

// MutedVolume is channels at 0.0.
func MutedVolume(channels int) Volume { return NewVolume(make([]float64, channels)) }

// NormalVolume is channels at 1.0.
func NormalVolume(channels int) Volume {
	levels := make([]float64, channels)
	for i := range levels {
		levels[i] = 1
	}
	return NewVolume(levels)
}

// VolumeFromPercentage spreads one percentage (100 = 1.0) over
// channels.
func VolumeFromPercentage(percent float64, channels int) Volume {
	levels := make([]float64, channels)
	for i := range levels {
		levels[i] = percent / 100
	}
	return NewVolume(levels)
}

// Channel returns one channel's level.
func (v Volume) Channel(i int) (float64, bool) {
	if i < 0 || i >= len(v.levels) {
		return 0, false
	}
	return v.levels[i], true
}

// WithChannel returns a copy with one channel set (clamped like
// NewVolume); a missing channel is an InvalidChannelError.
func (v Volume) WithChannel(i int, level float64) (Volume, error) {
	if i < 0 || i >= len(v.levels) {
		return v, &InvalidChannelError{Channel: i}
	}
	out := slices.Clone(v.levels)
	out[i] = clampLevel(level)
	return Volume{levels: out}, nil
}

// Channels is the channel count.
func (v Volume) Channels() int { return len(v.levels) }

// Levels copies the per-channel levels.
func (v Volume) Levels() []float64 { return slices.Clone(v.levels) }

// Average is the channel mean, 0 for no channels.
func (v Volume) Average() float64 {
	if len(v.levels) == 0 {
		return 0
	}
	var sum float64
	for _, l := range v.levels {
		sum += l
	}
	return sum / float64(len(v.levels))
}

// ToPercentage is each channel as a percentage.
func (v Volume) ToPercentage() []float64 {
	out := make([]float64, len(v.levels))
	for i, l := range v.levels {
		out[i] = l * 100
	}
	return out
}

// AveragePercentage is the channel mean as a percentage.
func (v Volume) AveragePercentage() float64 { return v.Average() * 100 }

// IsMuted reports every channel at 0.0.
func (v Volume) IsMuted() bool {
	for _, l := range v.levels {
		if l != 0 {
			return false
		}
	}
	return true
}

// IsNormal reports every channel at 1.0.
func (v Volume) IsNormal() bool {
	for _, l := range v.levels {
		if l != 1 {
			return false
		}
	}
	return true
}

// Equal compares channel by channel.
func (v Volume) Equal(o Volume) bool { return slices.Equal(v.levels, o.levels) }

// volumeFromPulse is conversion/volume.rs's from_pulse: raw / NORM
// per channel, through NewVolume's clamp.
func volumeFromPulse(cv native.CVolume) Volume {
	levels := make([]float64, len(cv))
	for i, raw := range cv {
		levels[i] = float64(raw) / float64(native.VolumeNorm)
	}
	return NewVolume(levels)
}

// volumeFromPulseSingle is from_pulse_single, for base volumes.
func volumeFromPulseSingle(raw native.Volume) Volume {
	return NewVolume([]float64{float64(raw) / float64(native.VolumeNorm)})
}

// toPulse is to_pulse: level * NORM per channel, truncated.
func (v Volume) toPulse() native.CVolume {
	out := make(native.CVolume, len(v.levels))
	for i, l := range v.levels {
		out[i] = native.Volume(l * float64(native.VolumeNorm))
	}
	return out
}
