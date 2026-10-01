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

// Source records the configured PulseAudio source as 16-bit PCM, mono
// or interleaved stereo,
// on the shell's native client, the counterpart of libcava's pulse
// input: samples are handed over as raw int16 values, and a pending
// buffer that would overflow is discarded whole, as cava's input
// buffering does.
type Source struct {
	capturer Capturer
	target   pulse.CaptureTarget
	channels int
	samples  *samples

	capture *pulse.Capture
}

// Input is what the analyzer reads from: a capture started and
// stopped, its samples drained each frame.
type Input interface {
	Start() error
	Drained() []float64
	Stop()
}

// samples is libcava's cava_in buffer: interleaved samples waiting for
// the analyzer, at most capacity of them.
type samples struct {
	channels int
	capacity int

	mu   sync.Mutex
	pend []float64
}

func newSamples(channels, capacity int) *samples {
	return &samples{channels: channels, capacity: capacity - capacity%channels}
}

// push is write_to_cava_input_buffers: a chunk that would overflow
// discards the backlog first, and one larger than the buffer keeps its
// newest whole frames.
func (s *samples) push(chunk []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pend)+len(chunk) > s.capacity {
		// cava drops the whole backlog rather than analyze stale audio.
		s.pend = s.pend[:0]
		if len(chunk) > s.capacity {
			chunk = chunk[len(chunk)-s.capacity:]
		}
	}
	s.pend = append(s.pend, chunk...)
}

// silence is reset_output_buffers: a full buffer of zeros.
func (s *samples) silence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pend = append(s.pend[:0], make([]float64, s.capacity)...)
}

// drained hands over every sample since the last call.
func (s *samples) drained() []float64 {
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

// s16 decodes whole frames of s16le PCM.
func s16(data []byte, channels int) []float64 {
	n := len(data) / (2 * channels) * channels
	out := make([]float64, n)
	for i := range n {
		out[i] = float64(int16(binary.LittleEndian.Uint16(data[i*2:])))
	}
	return out
}

// NewSource records source ("auto" or empty: the default sink's
// monitor, following default changes; otherwise a device by name, a
// sink recording its monitor). channels is 1 or 2, capacity the
// analyzer's input size in interleaved samples.
func NewSource(capturer Capturer, source string, channels, capacity int) (*Source, error) {
	if capturer == nil {
		return nil, errors.New("cava: no audio server connection")
	}
	if channels < 1 || channels > 2 {
		return nil, errors.New("cava: the capture is mono or stereo")
	}
	if capacity < channels {
		return nil, errors.New("cava: input capacity below one frame")
	}
	target := pulse.CaptureDefaultMonitor()
	if source != "" && source != "auto" {
		named, err := pulse.CaptureNamed(source)
		if err != nil {
			return nil, err
		}
		target = named
	}
	return &Source{capturer: capturer, target: target, channels: channels, samples: newSamples(channels, capacity)}, nil
}

// Start opens the record stream.
func (s *Source) Start() error {
	if s.capture != nil {
		return errors.New("cava: source already running")
	}
	capture, err := s.capturer.Capture(s.target, pulse.CaptureSpec{
		Name:           "cava",
		Rate:           sampleRate,
		Channels:       uint8(s.channels),
		FragmentFrames: fragmentFrames,
	}, s.accumulate)
	if err != nil {
		return err
	}
	s.capture = capture
	return nil
}

// accumulate converts one chunk of s16le frames; a backlog trimmed to
// capacity drops whole frames, so stereo stays left-right paired.
func (s *Source) accumulate(data []byte) { s.samples.push(s16(data, s.channels)) }

// Drained hands over every sample captured since the last call.
func (s *Source) Drained() []float64 { return s.samples.drained() }

// Stop closes the record stream.
func (s *Source) Stop() {
	if s.capture != nil {
		s.capture.Close()
		s.capture = nil
	}
}
