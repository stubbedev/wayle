package bar

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/recorder"
)

func TestRecorderLabel(t *testing.T) {
	// helpers.rs's assertions: idle shows a dash, recording shows the
	// clock.
	if got := recorderLabel("{{ elapsed }}", false, false, 0); got != "-" {
		t.Errorf("idle = %q", got)
	}
	if got := recorderLabel("{{ elapsed }}", true, false, 65); got != "1:05" {
		t.Errorf("recording = %q", got)
	}
	if got := recorderLabel("{{ state }} {{ elapsed }}", true, true, 3661); got != i18n.T("bar-recorder-paused")+" 1:01:01" {
		t.Errorf("paused = %q", got)
	}
}

func TestRecorderIconPriority(t *testing.T) {
	cfg := config.DefaultsRecorder()
	if got := recorderIconName(cfg, recorder.Change{}); got != cfg.Icons()[config.RecorderIdle].Name {
		t.Errorf("idle icon = %q", got)
	}
	if got := recorderIconName(cfg, recorder.Change{Active: true}); got != cfg.Icons()[config.RecorderRecording].Name {
		t.Errorf("recording icon = %q", got)
	}
	if got := recorderIconName(cfg, recorder.Change{Active: true, Paused: true}); got != cfg.Icons()[config.RecorderPaused].Name {
		t.Errorf("paused icon = %q", got)
	}
	// Preparing keeps the recording glyph (the Rust pulse class).
	if got := recorderIconName(cfg, recorder.Change{Preparing: true}); got != cfg.Icons()[config.RecorderRecording].Name {
		t.Errorf("preparing icon = %q", got)
	}
}

func TestLoadFileAppliesRecorder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.recorder]\noutput-format = \"mp4\"\nmicrophone = true\nstart-delay-ms = 0\nicon-recording = \"tb-circle-dot-symbolic\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Recorder.OutputFormat != "mp4" || !c.Recorder.Microphone || c.Recorder.StartDelayMs != 0 {
		t.Errorf("config = %+v", c.Recorder)
	}
	if got := c.Recorder.Icons()[config.RecorderRecording].Name; got != "tb-circle-dot-symbolic" {
		t.Errorf("icon-recording = %q", got)
	}

	if err := osWrite(path, "[modules.recorder]\noutput-format = \"avi\"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Error("bad output-format: want a load error")
	}
}

func TestRecorderModuleFollowsState(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Recorder = recorder.NewState(fakeRecorderEngine{}, 0)

	module, err := Create("recorder", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	label := findLabel(module.Root())
	if got := label.Text(); got != "-" {
		t.Fatalf("idle label = %q, want -", got)
	}
	// Drive the shared state; the module's follower picks it up.
	ctx.Recorder.Start()
	waitForIdleText(t, label, "0:00")
	ctx.Recorder.Stop()
	waitForIdleText(t, label, "-")
}

// fakeRecorderEngine hands out live handles the state machine can
// drive.
type fakeRecorderEngine struct{}

func (fakeRecorderEngine) Start(context.Context, recorder.Options) (recorder.Handle, error) {
	return fakeRecorderHandle{}, nil
}

type fakeRecorderHandle struct{ stop chan struct{} }

func (fakeRecorderHandle) Pause()  {}
func (fakeRecorderHandle) Resume() {}

func (h fakeRecorderHandle) Stop() {
	if h.stop != nil {
		close(h.stop)
	}
}

func (fakeRecorderHandle) Done() <-chan error { return make(chan error) }
