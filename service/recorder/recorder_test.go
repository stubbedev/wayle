package recorder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeHandle records the control calls.
type fakeHandle struct {
	pauses   int
	resumes  int
	stops    int
	done     chan error
	finished bool
}

func (f *fakeHandle) Pause()  { f.pauses++ }
func (f *fakeHandle) Resume() { f.resumes++ }
func (f *fakeHandle) Stop() {
	f.stops++
	if !f.finished {
		f.finished = true
		close(f.done)
	}
}
func (f *fakeHandle) Done() <-chan error { return f.done }

// fakeEngine hands back a fresh handle per start, like a real
// engine would.
type fakeEngine struct {
	handle *fakeHandle
	err    error
}

func (e *fakeEngine) Start(context.Context, Options) (Handle, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.handle = &fakeHandle{done: make(chan error)}
	return e.handle, nil
}

func waitFor(t *testing.T, s *State, cond func(Change) bool) Change {
	t.Helper()
	changes, stop := s.Changes()
	defer stop()
	deadline := time.After(2 * time.Second)
	for {
		snap := s.Snapshot()
		if cond(snap) {
			return snap
		}
		select {
		case <-changes:
		case <-deadline:
			t.Fatalf("state never matched: %+v", s.Snapshot())
		}
	}
}

func TestStartRecordsAndStopFinalizes(t *testing.T) {
	engine := &fakeEngine{}
	s := NewState(engine, 0)
	s.Start()

	// Idle -> Starting -> Recording.
	waitFor(t, s, func(c Change) bool { return c.Status == StatusRecording })
	if got := s.Snapshot().OutputPath; filepath.Base(got) == "" {
		t.Fatalf("output path = %q", got)
	}

	// The elapsed timer ticks once per second.
	waitFor(t, s, func(c Change) bool { return c.ElapsedSecs >= 1 })

	// Pause and resume reach the engine.
	s.SetPaused(true)
	waitFor(t, s, func(c Change) bool { return c.Paused })
	if engine.handle.pauses != 1 {
		t.Fatalf("pauses = %d", engine.handle.pauses)
	}
	s.SetPaused(true)
	if engine.handle.pauses != 1 {
		t.Fatal("a second pause re-fired the engine")
	}
	s.SetPaused(false)
	waitFor(t, s, func(c Change) bool { return !c.Paused })

	// Stop finalizes and resets.
	s.Stop()
	waitFor(t, s, func(c Change) bool { return c.Status == StatusIdle })
	if engine.handle.stops == 0 {
		t.Fatal("stop never reached the engine")
	}
	if snap := s.Snapshot(); snap.ElapsedSecs != 0 || snap.OutputPath != "" {
		t.Fatalf("idle snapshot = %+v", snap)
	}
}

func TestStartClaimIsSynchronous(t *testing.T) {
	engine := &fakeEngine{err: context.DeadlineExceeded}
	s := NewState(engine, 0)
	s.Start()
	// A rapid second start is a no-op while the first is in flight.
	s.Start()
	s.Stop()
	// The failed start lands back on idle, not stuck in stopping.
	waitFor(t, s, func(c Change) bool { return c.Status == StatusIdle })
}

func TestToggle(t *testing.T) {
	engine := &fakeEngine{}
	s := NewState(engine, 0)
	s.Toggle()
	waitFor(t, s, func(c Change) bool { return c.Status == StatusRecording })
	s.Toggle()
	waitFor(t, s, func(c Change) bool { return c.Status == StatusIdle })
	// Toggling from idle again starts fresh.
	s.Toggle()
	waitFor(t, s, func(c Change) bool { return c.Status == StatusRecording })
	s.Stop()
	waitFor(t, s, func(c Change) bool { return c.Status == StatusIdle })
}

func TestFormatElapsed(t *testing.T) {
	for _, tc := range []struct {
		secs uint32
		want string
	}{
		{5, "0:05"}, {65, "1:05"}, {3661, "1:01:01"}, {0, "0:00"},
	} {
		if got := FormatElapsed(tc.secs); got != tc.want {
			t.Errorf("FormatElapsed(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

func TestOutputPath(t *testing.T) {
	t.Setenv("XDG_VIDEOS_DIR", "")
	t.Setenv("HOME", "/home/u")
	p := OutputPath("", "mkv")
	if filepath.Dir(p) != "/home/u/Videos" || filepath.Ext(p) != ".mkv" {
		t.Fatalf("path = %q", p)
	}
	if filepath.Base(p)[:6] != "wayle-" {
		t.Fatalf("name = %q", filepath.Base(p))
	}
	if got := OutputPath("/custom", "mp4"); filepath.Dir(got) != "/custom" || filepath.Ext(got) != ".mp4" {
		t.Fatalf("configured path = %q", got)
	}
}

func TestVideosDirFallback(t *testing.T) {
	t.Setenv("XDG_VIDEOS_DIR", "/vids")
	if got := VideosDir(); got != "/vids" {
		t.Errorf("env = %q", got)
	}
	os.Unsetenv("XDG_VIDEOS_DIR")
	t.Setenv("HOME", "/home/u")
	if got := VideosDir(); got != "/home/u/Videos" {
		t.Errorf("home fallback = %q", got)
	}
}
