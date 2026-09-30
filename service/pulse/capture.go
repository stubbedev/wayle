package pulse

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// captureRetry spaces out attempts to reopen a capture whose source
// is missing or whose stream the server killed.
var captureRetry = time.Second

type captureKind int

const (
	captureDefaultMonitor captureKind = iota
	captureDefaultInput
	captureNamed
)

// CaptureTarget selects what a capture records. The zero value is the
// default sink's monitor.
type CaptureTarget struct {
	kind captureKind
	name string
}

// CaptureDefaultMonitor records whatever the default sink plays, and
// follows the default when it changes.
func CaptureDefaultMonitor() CaptureTarget { return CaptureTarget{kind: captureDefaultMonitor} }

// CaptureDefaultInput records the default source, following it.
func CaptureDefaultInput() CaptureTarget { return CaptureTarget{kind: captureDefaultInput} }

// CaptureNamed records one device by name: a source, or a sink, whose
// monitor it then records (PipeWire's target.object semantics).
func CaptureNamed(name string) (CaptureTarget, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return CaptureTarget{}, fmt.Errorf("pulse: invalid capture device name %q", name)
	}
	return CaptureTarget{kind: captureNamed, name: name}, nil
}

func (t CaptureTarget) String() string {
	switch t.kind {
	case captureDefaultInput:
		return "default input"
	case captureNamed:
		return t.name
	}
	return "default monitor"
}

// CaptureSpec is the PCM a capture delivers: signed 16-bit
// little-endian samples at Rate, Channels interleaved (1 or 2; the
// server mixes down or up), in chunks of about FragmentFrames frames.
// Name becomes the stream's media.name.
type CaptureSpec struct {
	Name           string
	Rate           uint32
	Channels       uint8
	FragmentFrames uint32
}

func (spec CaptureSpec) validate() error {
	switch {
	case spec.Rate == 0:
		return errors.New("pulse: capture: zero sample rate")
	case spec.Channels != 1 && spec.Channels != 2:
		return fmt.Errorf("pulse: capture: %d channels (want 1 or 2)", spec.Channels)
	case spec.FragmentFrames == 0:
		return errors.New("pulse: capture: zero fragment size")
	}
	return nil
}

func (spec CaptureSpec) params(source string) native.RecordParams {
	channels := native.ChannelMap{native.ChannelMono}
	if spec.Channels == 2 {
		channels = native.ChannelMap{native.ChannelFrontLeft, native.ChannelFrontRight}
	}
	ss := native.SampleSpec{Format: native.SampleS16LE, Channels: spec.Channels, Rate: spec.Rate}
	props := native.PropList{}
	if spec.Name != "" {
		props["media.name"] = spec.Name
	}
	return native.RecordParams{
		SampleSpec: ss,
		ChannelMap: channels,
		Source:     source,
		FragSize:   spec.FragmentFrames * uint32(ss.FrameSize()),
		Props:      props,
	}
}

// Capture is a running record stream that keeps itself pointed at its
// target: it reopens on the new source when the target resolves
// differently (the default changed) and after the server kills the
// stream, and waits while the target does not exist.
type Capture struct {
	s      *Service
	target CaptureTarget
	spec   CaptureSpec
	onData func([]byte)
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once

	mu     sync.Mutex
	source string
}

// Capture starts recording target. onData receives each chunk on the
// connection's reader goroutine: it must not block, and it owns the
// slice. A target that does not resolve yet is not an error; the
// capture waits for it.
func (s *Service) Capture(target CaptureTarget, spec CaptureSpec, onData func([]byte)) (*Capture, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	if onData == nil {
		return nil, errors.New("pulse: capture: nil data handler")
	}
	ticks, unsubscribe, err := s.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	c := &Capture{
		s: s, target: target, spec: spec, onData: onData,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go c.run(ticks, unsubscribe)
	return c, nil
}

// Source names the source being recorded, empty while waiting.
func (c *Capture) Source() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.source
}

// Close stops the capture and deletes its stream.
func (c *Capture) Close() {
	c.once.Do(func() { close(c.stop) })
	<-c.done
}

// resolveCapture maps a target onto a source name, "" when it does
// not exist right now.
func (s *Service) resolveCapture(t CaptureTarget) string {
	switch t.kind {
	case captureDefaultInput:
		if d, ok := s.DefaultInput(); ok {
			return d.Name
		}
		return ""
	case captureNamed:
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, d := range s.outputs {
			if d.Name == t.name {
				return d.MonitorSourceName
			}
		}
		return t.name
	}
	if d, ok := s.DefaultOutput(); ok {
		return d.MonitorSourceName
	}
	return ""
}

func (c *Capture) run(ticks <-chan struct{}, unsubscribe func()) {
	var stream *native.RecordStream
	current, failed := "", ""
	var retry <-chan time.Time
	setSource := func(name string) {
		c.mu.Lock()
		c.source = name
		c.mu.Unlock()
	}
	closeStream := func() {
		if stream != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = stream.Close(ctx)
			cancel()
			stream, current = nil, ""
			setSource("")
		}
	}
	defer func() {
		closeStream()
		unsubscribe()
		close(c.done)
	}()
	for {
		want := c.s.resolveCapture(c.target)
		if stream != nil && want != current {
			closeStream()
		}
		// A name that just failed waits for the retry timer; a new
		// name is tried at once.
		if stream == nil && want != "" && (retry == nil || want != failed) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			st, err := c.s.conn.CreateRecordStream(ctx, c.spec.params(want), c.onData)
			cancel()
			if err != nil {
				if want != failed {
					log.Printf("pulse: capture %s from %q: %v", c.target, want, err)
				}
				failed, retry = want, time.After(captureRetry)
			} else {
				stream, current, failed, retry = st, want, "", nil
				setSource(want)
			}
		}
		var ended <-chan struct{}
		if stream != nil {
			ended = stream.Ended()
		}
		select {
		case <-c.stop:
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
		case <-ended:
			failed, retry = current, time.After(captureRetry)
			stream, current = nil, ""
			setSource("")
		case <-retry:
			retry = nil
		}
	}
}
