// Package recordertest is the scripted recorder.Engine every recorder
// test drives: sessions and recordings that record their calls, an
// optional gate that holds OpenSession (the portal picker), and a hook
// that ends a recording as a failed pipeline would.
package recordertest

import (
	"context"
	"os"
	"sync"

	"github.com/stubbedev/wayle/service/recorder"
)

// Engine is a fake recorder.Engine.
type Engine struct {
	// OpenErr and StartErr fail the respective step.
	OpenErr, StartErr error
	// Gate, when set, holds OpenSession until it closes or ctx ends.
	Gate chan struct{}
	// WriteOutput makes a stopped recording leave a non-empty file at
	// its output path, as a finalized muxer does.
	WriteOutput bool

	mu         sync.Mutex
	sessions   []*Session
	recordings []*Recording
}

// OpenSession implements recorder.Engine.
func (e *Engine) OpenSession(ctx context.Context, _ bool) (recorder.Session, error) {
	if e.Gate != nil {
		select {
		case <-e.Gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e.OpenErr != nil {
		return nil, e.OpenErr
	}
	s := &Session{}
	e.mu.Lock()
	e.sessions = append(e.sessions, s)
	e.mu.Unlock()
	return s, nil
}

// Start implements recorder.Engine.
func (e *Engine) Start(_ recorder.Session, opts recorder.Options, term func(string)) (recorder.Recording, error) {
	if e.StartErr != nil {
		return nil, e.StartErr
	}
	r := &Recording{Opts: opts, term: term, write: e.WriteOutput}
	e.mu.Lock()
	e.recordings = append(e.recordings, r)
	e.mu.Unlock()
	return r, nil
}

// Sessions are the opened sessions.
func (e *Engine) Sessions() []*Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]*Session(nil), e.sessions...)
}

// Recordings are the started recordings.
func (e *Engine) Recordings() []*Recording {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]*Recording(nil), e.recordings...)
}

// Session is a fake recorder.Session.
type Session struct {
	mu     sync.Mutex
	closed int
}

// Cast implements recorder.Session.
func (s *Session) Cast() recorder.Cast { return recorder.Cast{Node: 1, Width: 1920, Height: 1080} }

// Close implements recorder.Session.
func (s *Session) Close() {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
}

// Closed counts Close calls.
func (s *Session) Closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Recording is a fake recorder.Recording.
type Recording struct {
	// Opts are the options it started with.
	Opts recorder.Options

	mu       sync.Mutex
	paused   []bool
	stops    int
	term     func(string)
	write    bool
	PauseErr error
}

// SetPaused implements recorder.Recording.
func (r *Recording) SetPaused(paused bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.PauseErr != nil {
		return r.PauseErr
	}
	r.paused = append(r.paused, paused)
	return nil
}

// Stop implements recorder.Recording.
func (r *Recording) Stop() {
	r.mu.Lock()
	r.stops++
	write := r.write && r.stops == 1
	r.mu.Unlock()
	if write {
		_ = os.WriteFile(r.Opts.OutputPath, []byte("video"), 0o600)
	}
}

// Paused lists the SetPaused arguments.
func (r *Recording) Paused() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.paused...)
}

// Stops counts Stop calls.
func (r *Recording) Stops() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stops
}

// Fail ends the recording on its own, as a dying pipeline reports.
func (r *Recording) Fail(reason string) { r.term(reason) }
