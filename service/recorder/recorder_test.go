package recorder_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/recorder/recordertest"
)

// hookLog records the toasts and notifications.
type hookLog struct {
	mu      sync.Mutex
	toasts  []string
	notices []string
}

func (h *hookLog) hooks() recorder.Hooks {
	return recorder.Hooks{
		Toast: func(label, _ string, _ uint32) {
			h.mu.Lock()
			h.toasts = append(h.toasts, label)
			h.mu.Unlock()
		},
		Notify: func(_, summary, body, _ string) {
			h.mu.Lock()
			h.notices = append(h.notices, summary+": "+body)
			h.mu.Unlock()
		},
	}
}

func (h *hookLog) snapshot() ([]string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.toasts), slices.Clone(h.notices)
}

func newState(t *testing.T, engine *recordertest.Engine, edit func(*config.RecorderConfig)) (*recorder.State, *hookLog) {
	t.Helper()
	cfg := config.Defaults().Recorder
	cfg.OutputDirectory = filepath.Join(t.TempDir(), "videos")
	cfg.StartDelayMs = 0
	if edit != nil {
		edit(&cfg)
	}
	log := &hookLog{}
	return recorder.NewState(engine, func() config.RecorderConfig { return cfg }, log.hooks()), log
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRecordPauseStopSaves(t *testing.T) {
	engine := &recordertest.Engine{WriteOutput: true}
	s, log := newState(t, engine, func(c *config.RecorderConfig) {
		c.OutputFormat = config.RecorderFormatWebm
		c.Microphone, c.MicrophoneDevice, c.SystemAudio = true, "mic0", true
		c.WebcamEnabled, c.WebcamX, c.WebcamY, c.WebcamSize = true, 10, 90, 25
	})
	s.Start()
	if snap := s.Snapshot(); snap.Status != recorder.StatusStarting || !snap.Preparing || snap.Active {
		t.Fatalf("after Start = %+v, want a synchronous Starting claim", snap)
	}
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	snap := s.Snapshot()
	if snap.Preparing || snap.Status != recorder.StatusRecording || filepath.Ext(snap.OutputPath) != ".webm" {
		t.Fatalf("recording snapshot = %+v", snap)
	}
	rec := engine.Recordings()[0]
	opts := rec.Opts
	if opts.Format != recorder.FormatWebM || !opts.Microphone || opts.MicrophoneDevice != "mic0" || !opts.SystemAudio ||
		opts.Webcam == nil || opts.Webcam.X != 10 || opts.Webcam.Y != 90 || opts.Webcam.Size != 25 {
		t.Errorf("options = %+v (webcam %+v)", opts, opts.Webcam)
	}
	if _, err := os.Stat(filepath.Dir(opts.OutputPath)); err != nil {
		t.Errorf("output dir not created: %v", err)
	}

	s.SetPaused(true)
	s.SetPaused(true) // no change: not forwarded
	if got := rec.Paused(); !slices.Equal(got, []bool{true}) || !s.Snapshot().Paused {
		t.Errorf("pause calls = %v", got)
	}
	s.SetPaused(false)

	s.Stop()
	if snap := s.Snapshot(); snap.Active || snap.Status != recorder.StatusStopping {
		t.Errorf("right after Stop = %+v, want inactive and stopping", snap)
	}
	waitFor(t, "idle", func() bool { return s.Snapshot().Status == recorder.StatusIdle })
	if rec.Stops() != 1 || engine.Sessions()[0].Closed() != 1 {
		t.Errorf("stops %d, session closes %d", rec.Stops(), engine.Sessions()[0].Closed())
	}
	waitFor(t, "the saved notice", func() bool { _, n := log.snapshot(); return len(n) == 1 })
	toasts, notices := log.snapshot()
	if !slices.Contains(toasts, i18n.T("recorder-toast-starting")) || !slices.Contains(toasts, i18n.T("recorder-toast-stopped")) {
		t.Errorf("toasts = %q", toasts)
	}
	if notices[0] != i18n.T("recorder-notification-saved")+": "+opts.OutputPath {
		t.Errorf("notice = %q", notices[0])
	}
}

// A stop that leaves no file reports a failure, not "saved".
func TestStopWithoutOutputReportsFailure(t *testing.T) {
	engine := &recordertest.Engine{}
	s, log := newState(t, engine, nil)
	s.Start()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	s.Stop()
	waitFor(t, "the failure notice", func() bool { _, n := log.snapshot(); return len(n) == 1 })
	if _, n := log.snapshot(); !strings.HasPrefix(n[0], i18n.T("recorder-notification-failed")) {
		t.Errorf("notice = %q, want the failure", n[0])
	}
}

func TestStartClaimIsSynchronousAndStopCancelsTheStart(t *testing.T) {
	gate := make(chan struct{})
	engine := &recordertest.Engine{Gate: gate}
	s, _ := newState(t, engine, nil)
	s.Start()
	s.Start() // a rapid second start is a no-op
	s.Stop()  // cancels the start held at the picker
	waitFor(t, "idle", func() bool { return s.Snapshot().Status == recorder.StatusIdle })
	close(gate)
	time.Sleep(20 * time.Millisecond)
	if len(engine.Recordings()) != 0 {
		t.Error("a cancelled start still recorded")
	}
	// Idle again: a new start proceeds.
	s.Start()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	s.Stop()
	waitFor(t, "idle", func() bool { return s.Snapshot().Status == recorder.StatusIdle })
}

func TestFailedStartsReset(t *testing.T) {
	for name, engine := range map[string]*recordertest.Engine{
		"portal":   {OpenErr: errors.New("denied")},
		"pipeline": {StartErr: errors.New("no encoder")},
	} {
		s, log := newState(t, engine, nil)
		s.Start()
		waitFor(t, name+" failure", func() bool { _, n := log.snapshot(); return len(n) == 1 })
		if snap := s.Snapshot(); snap.Status != recorder.StatusIdle || snap.Preparing || snap.Active {
			t.Errorf("%s: snapshot = %+v", name, snap)
		}
		if name == "pipeline" && engine.Sessions()[0].Closed() != 1 {
			t.Error("pipeline failure leaked the portal session")
		}
	}
}

func TestUnexpectedTerminationTearsDown(t *testing.T) {
	engine := &recordertest.Engine{}
	s, log := newState(t, engine, nil)
	s.Start()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	rec := engine.Recordings()[0]
	rec.Fail("disk full")
	waitFor(t, "idle", func() bool { return s.Snapshot().Status == recorder.StatusIdle })
	if rec.Stops() != 1 {
		t.Errorf("stops = %d", rec.Stops())
	}
	_, n := log.snapshot()
	if len(n) != 1 || !strings.Contains(n[0], "disk full") {
		t.Errorf("notices = %q", n)
	}
	// A stale report from a finished recording is ignored.
	s.Start()
	waitFor(t, "recording again", func() bool { return s.Snapshot().Active })
	rec.Fail("late")
	time.Sleep(20 * time.Millisecond)
	if !s.Snapshot().Active {
		t.Error("a stale termination stopped the new recording")
	}
	s.Stop()
}

func TestPauseFailureKeepsState(t *testing.T) {
	engine := &recordertest.Engine{}
	s, _ := newState(t, engine, nil)
	s.SetPaused(true) // idle: ignored
	s.Start()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	engine.Recordings()[0].PauseErr = errors.New("busy")
	s.SetPaused(true)
	if s.Snapshot().Paused {
		t.Error("a failed pause reported paused")
	}
	s.Stop()
}

func TestElapsedSkipsPausedSeconds(t *testing.T) {
	engine := &recordertest.Engine{}
	s, _ := newState(t, engine, nil)
	s.Start()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	waitFor(t, "a second", func() bool { return s.Snapshot().ElapsedSecs >= 1 })
	s.SetPaused(true)
	held := s.Snapshot().ElapsedSecs
	time.Sleep(1300 * time.Millisecond)
	if got := s.Snapshot().ElapsedSecs; got != held {
		t.Errorf("elapsed moved while paused: %d -> %d", held, got)
	}
	s.Stop()
}

func TestToggle(t *testing.T) {
	engine := &recordertest.Engine{}
	s, _ := newState(t, engine, nil)
	s.Toggle()
	waitFor(t, "recording", func() bool { return s.Snapshot().Active })
	s.Toggle()
	waitFor(t, "idle", func() bool { return s.Snapshot().Status == recorder.StatusIdle })
}

func TestFormatElapsed(t *testing.T) {
	for secs, want := range map[uint32]string{5: "0:05", 65: "1:05", 3661: "1:01:01", 0: "0:00"} {
		if got := recorder.FormatElapsed(secs); got != want {
			t.Errorf("FormatElapsed(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestOutputPath(t *testing.T) {
	t.Setenv("XDG_VIDEOS_DIR", "")
	t.Setenv("HOME", "/home/u")
	now := time.Date(2026, 10, 1, 9, 8, 7, 0, time.Local)
	if got := recorder.OutputPath("", recorder.FormatMKV, now); got != "/home/u/Videos/wayle-20261001-090807.mkv" {
		t.Errorf("default = %q", got)
	}
	if got := recorder.OutputPath("/custom", recorder.FormatMP4, now); got != "/custom/wayle-20261001-090807.mp4" {
		t.Errorf("configured = %q", got)
	}
	t.Setenv("XDG_VIDEOS_DIR", "/vids")
	if got := recorder.VideosDir(); got != "/vids" {
		t.Errorf("env = %q", got)
	}
}

func TestBuildOptionsWithoutWebcam(t *testing.T) {
	cfg := config.Defaults().Recorder
	cfg.WebcamEnabled = false
	if opts := recorder.BuildOptions(cfg, time.Now()); opts.Webcam != nil {
		t.Error("webcam disabled still overlays")
	}
}
