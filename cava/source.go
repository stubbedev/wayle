package cava

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
)

// sampleRate is cava's fixed capture rate (service.rs's
// DEFAULT_SAMPLERATE); the plan is built for it.
const sampleRate = 44100

// Source captures the audio output monitor through pw-record, writing
// raw f32le mono frames to stdout. PipeWire is part of wayle's runtime
// already (the recorder, the portals); this keeps the capture path
// cgo-free while the Rust shell links libpulse.
type Source struct {
	target string

	cmd     *exec.Cmd
	cancel  context.CancelFunc
	errOnce sync.Once
	err     error

	mu   sync.Mutex
	pend []float64
}

// NewSource captures from target, a PipeWire node name. "auto" and the
// empty string select the default monitor.
func NewSource(target string) *Source {
	if target == "" || target == "auto" {
		target = "@DEFAULT_MONITOR@"
	}
	return &Source{target: target}
}

// args builds the pw-record invocation; split out so tests pin it
// without spawning a process.
func (s *Source) args() []string {
	return []string{
		"--raw",
		"--format=f32",
		fmt.Sprintf("--rate=%d", sampleRate),
		"--channels=1",
		"--target=" + s.target,
		"-", // stdout
	}
}

// Start spawns pw-record and begins draining its stdout in the
// background. Samples accumulate until Drained.
func (s *Source) Start() error {
	if s.cmd != nil {
		return errors.New("cava: source already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "pw-record", s.args()...) //nolint:gosec // the target is a config value, the same trust level as the custom-module command
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("cava: pw-record stdout: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("cava: start pw-record: %w", err)
	}
	s.cmd, s.cancel = cmd, cancel

	go func() {
		defer cancel()
		reader := bufio.NewReaderSize(stdout, 1<<16)
		var chunk [4 * 1024]byte
		for {
			n, err := reader.Read(chunk[:])
			if n > 0 {
				s.accumulate(chunk[:n])
			}
			if err != nil {
				s.errOnce.Do(func() {
					if err != io.EOF {
						s.mu.Lock()
						s.err = fmt.Errorf("cava: pw-record: %w", err)
						s.mu.Unlock()
					}
				})
				return
			}
		}
	}()
	return nil
}

// accumulate converts one raw read to samples.
func (s *Source) accumulate(data []byte) {
	n := len(data) / 4
	samples := make([]float64, n)
	for i := range n {
		bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		samples[i] = float64(math.Float32frombits(bits))
	}
	s.mu.Lock()
	s.pend = append(s.pend, samples...)
	s.mu.Unlock()
}

// Drained hands over every sample captured since the last call. The
// second return reports whether the capture stream is gone (process
// exited or failed), so the caller can restart it.
func (s *Source) Drained() ([]float64, bool, error) {
	s.mu.Lock()
	pend := s.pend
	s.pend = nil
	err := s.err
	s.mu.Unlock()
	if s.cmd == nil {
		return pend, true, nil
	}
	return pend, false, err
}

// Stop terminates the capture process.
func (s *Source) Stop() {
	if s.cancel != nil {
		s.cancel()
		_ = s.cmd.Wait()
		s.cmd, s.cancel = nil, nil
	}
}
