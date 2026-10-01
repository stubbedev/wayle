package cava

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
)

// squeezelite's vis_t, the shared visualization area libcava's shmem
// input maps (input/shmem.c): a pthread rwlock, then buf_size,
// buf_index, running, rate, the updated time_t, and 16384 s16 samples.
// The offsets are the 64-bit Linux layout (pthread_rwlock_t is 56
// bytes there on both x86-64 and aarch64).
const (
	visRwlockSize = 56
	visBufSizeOff = visRwlockSize
	visRunningOff = visRwlockSize + 8
	visRateOff    = visRwlockSize + 12
	visBufferOff  = visRwlockSize + 24 // after the 8-aligned time_t
	visBufSamples = 16384
	visSize       = visBufferOff + visBufSamples*2
)

// ShmemInput reads squeezelite's visualization area (libcava's
// input_shmem): it maps the POSIX shared memory object the source
// names and, paced to the stream rate, hands the analyzer the area's
// first fftw_frames*2 samples while squeezelite runs, silence while it
// does not.
type ShmemInput struct {
	name     string
	channels int
	samples  *samples

	area []byte
	stop chan struct{}
	done sync.WaitGroup
}

// NewShmemInput reads the shared memory object name ("/squeezelite-…");
// channels is 1 or 2, capacity the analyzer's input size.
func NewShmemInput(name string, channels, capacity int) (*ShmemInput, error) {
	if channels < 1 || channels > 2 {
		return nil, errors.New("cava: the capture is mono or stereo")
	}
	if capacity < channels {
		return nil, errors.New("cava: input capacity below one frame")
	}
	return &ShmemInput{name: name, channels: channels, samples: newSamples(channels, capacity)}, nil
}

// Start maps the area and begins reading. A missing object or a failed
// map is an error (libcava exits the process over them).
func (s *ShmemInput) Start() error {
	if s.stop != nil {
		return errors.New("cava: input already running")
	}
	path := "/dev/shm/" + strings.TrimPrefix(s.name, "/")
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("cava: could not open source '%s': %w", s.name, err)
	}
	area, err := syscall.Mmap(fd, 0, visSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	_ = syscall.Close(fd)
	if err != nil {
		return errors.New("cava: mmap failed - check if squeezelite is running with visualization enabled")
	}
	s.area = area
	s.stop = make(chan struct{})
	s.done.Add(1)
	go s.run()
	return nil
}

// Drained hands over every sample read since the last call.
func (s *ShmemInput) Drained() []float64 { return s.samples.drained() }

// Stop ends the reader and unmaps the area.
func (s *ShmemInput) Stop() {
	if s.stop == nil {
		return
	}
	close(s.stop)
	s.done.Wait()
	_ = syscall.Munmap(s.area)
	s.area, s.stop = nil, nil
}

func (s *ShmemInput) u32(off int) uint32 { return binary.LittleEndian.Uint32(s.area[off:]) }

func (s *ShmemInput) run() {
	defer s.done.Done()
	// fftw_frames is input_buffer_size / 2; the read block is twice it.
	block := fragmentFrames * s.channels
	for {
		rate := s.u32(visRateOff)
		bufFrames := int(s.u32(visBufSizeOff) / 2)
		// libcava divides by the rate unchecked; a zero rate (the area
		// not yet written) waits as the fifo's idle read does.
		wait := fifoIdle
		if rate != 0 {
			wait = time.Duration(1000000/rate) * time.Duration(bufFrames)
		}
		if s.area[visRunningOff] != 0 {
			// libcava's loop bound reads buf_frames / (fftw_frames * 2)
			// against a stride of fftw_frames * 2, so it hands over the
			// first block once a pass.
			for i := 0; i < bufFrames/block; i += block {
				chunk := make([]float64, block)
				for n := range chunk {
					chunk[n] = float64(int16(binary.LittleEndian.Uint16(s.area[visBufferOff+(n+i)*2:])))
				}
				s.samples.push(chunk)
			}
		} else {
			s.samples.push(make([]float64, block))
		}
		t := time.NewTimer(wait)
		select {
		case <-s.stop:
			t.Stop()
			return
		case <-t.C:
		}
	}
}
