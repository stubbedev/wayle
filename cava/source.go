package cava

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/stubbedev/wayle/service/pulse"
)

// sampleRate is cava's fixed capture rate (service.rs's
// DEFAULT_SAMPLERATE); the plan is built for it.
const sampleRate = 44100

// fragmentFrames is libcava's per-read chunk (PER_READ_CHUNK_SIZE in
// ffi/wrappers/audio_input.rs).
const fragmentFrames = 512

// Capturer is the record-stream seam; *pulse.Service implements it.
type Capturer interface {
	Capture(target pulse.CaptureTarget, spec pulse.CaptureSpec, onData func([]byte)) (*pulse.Capture, error)
}

// Source records the configured PulseAudio source as 16-bit mono PCM
// on the shell's native client, the counterpart of libcava's pulse
// input: samples are handed over as raw int16 values, and a pending
// buffer that would overflow is discarded whole, as cava's input
// buffering does.
type Source struct {
	capturer Capturer
	target   pulse.CaptureTarget
	capacity int

	capture *pulse.Capture

	mu   sync.Mutex
	pend []float64
}

// NewSource records source ("auto" or empty: the default sink's
// monitor, following default changes; otherwise a device by name, a
// sink recording its monitor). capacity is the analyzer's input size.
func NewSource(capturer Capturer, source string, capacity int) (*Source, error) {
	if capturer == nil {
		return nil, errors.New("cava: no audio server connection")
	}
	if capacity <= 0 {
		return nil, errors.New("cava: non-positive input capacity")
	}
	target := pulse.CaptureDefaultMonitor()
	if source != "" && source != "auto" {
		named, err := pulse.CaptureNamed(source)
		if err != nil {
			return nil, err
		}
		target = named
	}
	return &Source{capturer: capturer, target: target, capacity: capacity}, nil
}

// Start opens the record stream.
func (s *Source) Start() error {
	if s.capture != nil {
		return errors.New("cava: source already running")
	}
	capture, err := s.capturer.Capture(s.target, pulse.CaptureSpec{
		Name:           "cava",
		Rate:           sampleRate,
		Channels:       1,
		FragmentFrames: fragmentFrames,
	}, s.accumulate)
	if err != nil {
		return err
	}
	s.capture = capture
	return nil
}

// accumulate converts one chunk of s16le frames.
func (s *Source) accumulate(data []byte) {
	n := len(data) / 2
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pend)+n > s.capacity {
		// cava drops the whole backlog rather than analyze stale audio.
		s.pend = s.pend[:0]
		if n > s.capacity {
			data = data[(n-s.capacity)*2:]
			n = s.capacity
		}
	}
	for i := range n {
		s.pend = append(s.pend, float64(int16(binary.LittleEndian.Uint16(data[i*2:]))))
	}
}

// Drained hands over every sample captured since the last call.
func (s *Source) Drained() []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pend) == 0 {
		return nil
	}
	out := make([]float64, len(s.pend))
	copy(out, s.pend)
	s.pend = s.pend[:0]
	return out
}

// Stop closes the record stream.
func (s *Source) Stop() {
	if s.capture != nil {
		s.capture.Close()
		s.capture = nil
	}
}
