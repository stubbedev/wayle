package pulse

import (
	"context"
	"fmt"
)

// Source is the shell's seam onto the audio service: the default
// devices the volume and microphone modules, the OSD, and the audio
// dropdown render, their controls, and the monitor capture the cava
// visualizer records. *Service implements it; tests substitute fakes.
type Source interface {
	// DefaultSink returns the default output's snapshot, or
	// ErrNoDefaultDevice.
	DefaultSink(ctx context.Context) (OutputDevice, error)
	// DefaultSource returns the default input's snapshot.
	DefaultSource(ctx context.Context) (InputDevice, error)
	// Subscribe ticks on audio state changes (see Service.Subscribe).
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
	// SetVolume applies a percentage, clamped to 0-100, to every
	// channel of the default sink.
	SetVolume(ctx context.Context, percent float64) error
	// SetMuted mutes or unmutes the default sink.
	SetMuted(ctx context.Context, muted bool) error
	// SetSourceMuted mutes or unmutes the default source.
	SetSourceMuted(ctx context.Context, muted bool) error
	// OutputDevices, InputDevices, and PlaybackStreams snapshot the
	// lists the audio dropdown renders.
	OutputDevices() []OutputDevice
	InputDevices() []InputDevice
	PlaybackStreams() []AudioStream
	// SetDeviceVolume, SetDeviceMute, and SetDefault control one
	// device; SetStreamVolume and SetStreamMute one stream.
	SetDeviceVolume(ctx context.Context, key DeviceKey, v Volume) error
	SetDeviceMute(ctx context.Context, key DeviceKey, muted bool) error
	SetDefault(ctx context.Context, key DeviceKey) error
	SetStreamVolume(ctx context.Context, key StreamKey, v Volume) error
	SetStreamMute(ctx context.Context, key StreamKey, muted bool) error
	// Capture records a target until closed (see Service.Capture).
	Capture(target CaptureTarget, spec CaptureSpec, onData func([]byte)) (*Capture, error)
}

var _ Source = (*Service)(nil)

// DefaultSink implements Source.
func (s *Service) DefaultSink(context.Context) (OutputDevice, error) {
	d, ok := s.DefaultOutput()
	if !ok {
		return OutputDevice{}, fmt.Errorf("%w: output", ErrNoDefaultDevice)
	}
	return d, nil
}

// DefaultSource implements Source.
func (s *Service) DefaultSource(context.Context) (InputDevice, error) {
	d, ok := s.DefaultInput()
	if !ok {
		return InputDevice{}, fmt.Errorf("%w: input", ErrNoDefaultDevice)
	}
	return d, nil
}

// SetVolume implements Source: the DBus SetOutputVolume semantics,
// 0-100 over the device's channel count.
func (s *Service) SetVolume(ctx context.Context, percent float64) error {
	d, err := s.DefaultSink(ctx)
	if err != nil {
		return err
	}
	return s.SetDeviceVolume(ctx, d.Key, VolumeFromPercentage(clampPercent(percent), d.Volume.Channels()))
}

// SetMuted implements Source.
func (s *Service) SetMuted(ctx context.Context, muted bool) error {
	d, err := s.DefaultSink(ctx)
	if err != nil {
		return err
	}
	return s.SetDeviceMute(ctx, d.Key, muted)
}

// SetSourceMuted implements Source.
func (s *Service) SetSourceMuted(ctx context.Context, muted bool) error {
	d, err := s.DefaultSource(ctx)
	if err != nil {
		return err
	}
	return s.SetDeviceMute(ctx, d.Key, muted)
}

// clampPercent is the DBus interface's 0-100 range.
func clampPercent(p float64) float64 { return min(max(p, 0), 100) }
