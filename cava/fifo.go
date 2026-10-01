package cava

import (
	"errors"
	"sync"
	"syscall"
	"time"
)

// fifoIdle and fifoReopen are input_fifo's pacing: a read that finds
// nothing waits 10ms, and after ten of those in a row the buffer goes
// silent and the pipe is reopened (the writer may have gone away).
const (
	fifoIdle   = 10 * time.Millisecond
	fifoReopen = 10
)

// FifoInput reads s16le PCM from a named pipe (libcava's input_fifo):
// whole blocks of 512 frames, interleaved when stereo, at the rate the
// analyzer was planned for. /dev/zero is the test source, paced at a
// block a millisecond.
type FifoInput struct {
	path     string
	channels int
	samples  *samples

	stop chan struct{}
	done sync.WaitGroup
}

// NewFifoInput reads path; channels is 1 or 2, capacity the analyzer's
// input size in interleaved samples.
func NewFifoInput(path string, channels, capacity int) (*FifoInput, error) {
	if channels < 1 || channels > 2 {
		return nil, errors.New("cava: the capture is mono or stereo")
	}
	if capacity < channels {
		return nil, errors.New("cava: input capacity below one frame")
	}
	return &FifoInput{path: path, channels: channels, samples: newSamples(channels, capacity)}, nil
}

// Start begins reading on a goroutine (libcava's input thread).
func (f *FifoInput) Start() error {
	if f.stop != nil {
		return errors.New("cava: input already running")
	}
	f.stop = make(chan struct{})
	f.done.Add(1)
	go f.run()
	return nil
}

// Drained hands over every sample read since the last call.
func (f *FifoInput) Drained() []float64 { return f.samples.drained() }

// Stop ends the reader.
func (f *FifoInput) Stop() {
	if f.stop == nil {
		return
	}
	close(f.stop)
	f.done.Wait()
	f.stop = nil
}

// stopped reports a Stop, waiting at most d for it.
func (f *FifoInput) stopped(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-f.stop:
			return true
		default:
			return false
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-f.stop:
		return true
	case <-t.C:
		return false
	}
}

func (f *FifoInput) run() {
	defer f.done.Done()
	buf := make([]byte, fragmentFrames*f.channels*2)
	fd := f.open()
	defer func() { closeFd(fd) }()
	testMode := f.path == "/dev/zero"
	for !f.stopped(0) {
		idle, offset := 0, 0
		for offset < len(buf) {
			n := 0
			if fd >= 0 {
				n, _ = syscall.Read(fd, buf[offset:])
			}
			if n >= 1 {
				offset += n
				idle = 0
				continue
			}
			if f.stopped(fifoIdle) {
				return
			}
			idle++
			if idle > fifoReopen {
				f.samples.silence()
				closeFd(fd)
				fd = f.open()
				idle, offset = 0, 0
			}
		}
		f.samples.push(s16(buf, f.channels))
		if testMode && f.stopped(time.Millisecond) {
			return
		}
	}
}

// open is open_fifo with its reads non-blocking. libcava opens
// blocking, which parks the thread until a writer appears and leaves it
// unjoinable; opening non-blocking reads nothing until then instead,
// so Stop always returns. Like libcava it keeps no error: a path that
// will not open reads nothing and is retried with the next reopen.
func (f *FifoInput) open() int {
	fd, err := syscall.Open(f.path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1
	}
	return fd
}

func closeFd(fd int) {
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}
