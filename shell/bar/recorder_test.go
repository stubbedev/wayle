package bar

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/recorder/recordertest"
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
	// Paused only counts once recording: select_icon checks active first.
	if got := recorderIconName(cfg, recorder.Change{Preparing: true, Paused: true}); got != cfg.Icons()[config.RecorderRecording].Name {
		t.Errorf("preparing while paused icon = %q, want the recording icon", got)
	}
}

// The pre-capture delay puts recorder-preparing on the module root and
// takes it off once capture starts; the label keeps label-color.
func TestRecorderPreparingClass(t *testing.T) {
	cfg := config.Defaults()
	cfg.Recorder.OutputDirectory = t.TempDir()
	ctx := newTestContext(t, cfg)
	// The recorder notifies after Stop returns; retiring the generation
	// on the loop drops that late work before the next test runs.
	ctx.gen = newMountGen()
	t.Cleanup(func() { onHeadlessLoop(func() bool { ctx.gen.retire(); return true }) })
	gate := make(chan struct{})
	ctx.Recorder = recorder.NewState(&recordertest.Engine{Gate: gate}, func() config.RecorderConfig { return cfg.Recorder }, recorder.Hooks{})
	module, err := Create("recorder", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*recorderModule)
	chrome := widget.NewBox(widget.Row, 0, 0)
	onHeadlessLoop(func() bool { m.setChrome(chrome); return true })
	if onHeadlessLoop(func() bool { return widget.HasClass(chrome, "recorder-preparing") }) {
		t.Fatal("idle module is preparing")
	}
	// The gate holds the session open: the recorder stays preparing.
	ctx.Recorder.Start()
	waitHeadless(t, "the preparing class", func() bool { return widget.HasClass(chrome, "recorder-preparing") })
	if ink := onHeadlessLoop(func() bool { return m.label.Color() == 0 }); !ink {
		t.Error("preparing pinned the label ink")
	}
	close(gate)
	waitHeadless(t, "the class gone once recording", func() bool {
		return ctx.Recorder.Snapshot().Active && !widget.HasClass(chrome, "recorder-preparing")
	})
	ctx.Recorder.Stop()
	waitHeadless(t, "idle", func() bool { return ctx.Recorder.Snapshot().Status == recorder.StatusIdle })
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
	// A recording lands in a temp dir, never the user's ~/Videos.
	cfg.Recorder.OutputDirectory = t.TempDir()
	ctx := newTestContext(t, cfg)
	ctx.Recorder = recorder.NewState(&recordertest.Engine{}, func() config.RecorderConfig { return cfg.Recorder }, recorder.Hooks{})

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
	waitForText(t, label, "0:00")
	ctx.Recorder.Stop()
	waitForText(t, label, "-")
}

func TestRecorderModuleRequiresService(t *testing.T) {
	if _, err := Create("recorder", newTestContext(t, config.Defaults())); err == nil {
		t.Error("no recorder service: want an error")
	}
}
