package bar

import (
	"context"
	"sync"
	"testing"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/pulse/native"
	"github.com/stubbedev/wayle/service/pulse/pulsetest"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/recorder/recordertest"
)

func TestPreviewGeometryAndPercent(t *testing.T) {
	if pw, ph, cw, ch := previewGeometry(360, 20); pw != 312 || ph != 175 || cw != 62 || ch != 34 {
		t.Errorf("geometry = %d %d %d %d", pw, ph, cw, ch)
	}
	if pw, _, cw, ch := previewGeometry(100, 1); pw != 160 || cw != 24 || ch != 14 {
		t.Errorf("floors = %d %d %d", pw, cw, ch)
	}
	for _, tc := range []struct {
		px, margin, travel int32
		want               uint8
	}{
		{8, 8, 100, 0}, {58, 8, 100, 50}, {108, 8, 100, 100}, {500, 8, 100, 100}, {0, 8, 100, 0}, {50, 8, 0, 0},
	} {
		if got := pctFromPx(tc.px, tc.margin, tc.travel); got != tc.want {
			t.Errorf("pctFromPx(%d, %d, %d) = %d, want %d", tc.px, tc.margin, tc.travel, got, tc.want)
		}
	}
}

// The frame follows the pointer from where it was grabbed, clamped to
// the inset travel, and the release reports the percentages.
func TestWebcamPreviewDrag(t *testing.T) {
	p := newWebcamPreview(360, 20, 0, 0, 0, 0, 0)
	p.Arrange(render.Rect{X: 100, Y: 50, W: int(p.pw), H: int(p.ph)})
	margin, tw, th := p.travel()
	if p.camX != margin || p.camY != margin {
		t.Fatalf("0,0 placed at %d,%d, want the margin %d", p.camX, p.camY, margin)
	}
	var moved [][2]uint8
	p.onMove = func(x, y uint8) { moved = append(moved, [2]uint8{x, y}) }
	// Grab 5px into the frame, drag right by half the travel.
	p.HoverMove(widget.Point{X: 100 + int(margin) + 5, Y: 50 + int(margin) + 5})
	p.SetPressed(true)
	if p.CursorName() != "grabbing" {
		t.Error("no grabbing cursor while dragging")
	}
	p.DragMove(widget.Point{X: 100 + int(margin) + 5 + int(tw/2), Y: 50 + int(margin) + 5})
	if p.camX != margin+tw/2 || p.camY != margin {
		t.Errorf("after drag = %d,%d", p.camX, p.camY)
	}
	// Far past the corner clamps.
	p.DragMove(widget.Point{X: 5000, Y: 5000})
	if p.camX != margin+tw || p.camY != margin+th {
		t.Errorf("clamped = %d,%d", p.camX, p.camY)
	}
	p.SetPressed(false)
	if len(moved) != 1 || moved[0] != [2]uint8{100, 100} {
		t.Errorf("reported = %v", moved)
	}
	// Motion without a press moves nothing.
	p.DragMove(widget.Point{X: 0, Y: 0})
	if p.camX != margin+tw || len(moved) != 1 {
		t.Error("an unpressed move dragged the frame")
	}
	if p.CursorName() != "grab" {
		t.Error("cursor after release")
	}
	p.place(50, 0)
	if x, y := p.percent(); x != 50 || y != 0 {
		t.Errorf("place/percent round trip = %d,%d", x, y)
	}
}

// configLog records ModuleContext.SetConfig writes.
type configLog struct {
	mu     sync.Mutex
	writes map[string]any
}

func (c *configLog) set(path string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writes == nil {
		c.writes = map[string]any{}
	}
	c.writes[path] = value
}

func (c *configLog) get(path string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes[path]
}

func recorderTestCtx(t *testing.T, cams []deviceChoice) (ModuleContext, *configLog, *recordertest.Engine) {
	t.Helper()
	prev := recorderCameras
	recorderCameras = func() []deviceChoice { return cams }
	t.Cleanup(func() { recorderCameras = prev })
	cfg := config.Defaults()
	cfg.Recorder.OutputDirectory, cfg.Recorder.StartDelayMs = t.TempDir(), 0
	ctx := newTestContext(t, cfg)
	log := &configLog{}
	ctx.SetConfig = log.set
	engine := &recordertest.Engine{}
	ctx.Recorder = recorder.NewState(engine, func() config.RecorderConfig { return cfg.Recorder }, recorder.Hooks{})
	return ctx, log, engine
}

func TestRecorderDropdownStateAndControls(t *testing.T) {
	ctx, _, engine := recorderTestCtx(t, []deviceChoice{{"", "Automatic"}})
	v := recorderDropdown(ctx).(*recorderView)
	defer v.dropdownClosed()
	if v.status.Visible() || v.pause.Enabled() || !v.record.HasClass("primary") || v.recordLabel.Text() != i18n.T("dropdown-recorder-record") {
		t.Error("idle: want the record button, no status, pause disabled")
	}
	if v.webcam.Visible() || v.webcamHeader.Visible() {
		t.Error("no camera: the webcam card shows")
	}

	v.record.OnClick()
	waitHeadless(t, "recording", func() bool { return v.status.Visible() })
	if !v.record.HasClass("danger") || v.record.HasClass("primary") || v.recordLabel.Text() != i18n.T("dropdown-recorder-stop") || !v.pause.Enabled() {
		t.Error("recording: want stop, danger, pause enabled")
	}
	v.pause.OnClick()
	waitHeadless(t, "paused", func() bool { return v.statusDot.HasClass("paused") })
	if v.pauseIcon.Name() != "ld-play-symbolic" || v.pause.TooltipText() != i18n.T("dropdown-recorder-resume") {
		t.Error("paused: want the resume button")
	}
	if got := engine.Recordings()[0].Paused(); len(got) != 1 || !got[0] {
		t.Errorf("pause calls = %v", got)
	}
	v.record.OnClick()
	waitHeadless(t, "stopped", func() bool { return !v.status.Visible() })
}

func TestRecorderDropdownWritesConfig(t *testing.T) {
	ctx, log, _ := recorderTestCtx(t, []deviceChoice{{"", "Automatic"}, {"/dev/video0", "Cam"}})
	srv := pulsetest.New(t)
	srv.PutSource(pulsetest.Source(10, "mic", "Microphone", native.InvalidIndex, ""))
	svc, err := pulse.Connect(context.Background(), srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	ctx.Pulse = svc
	v := recorderDropdown(ctx).(*recorderView)
	defer v.dropdownClosed()
	if !v.webcam.Visible() {
		t.Fatal("a camera is present: the webcam card is hidden")
	}

	// Switches and pickers write their keys.
	micSwitch := v.Children()[3].(*widget.Box).Children()[0].(*widget.Box).Children()[1].(*widget.Switch)
	micSwitch.SetOn(!micSwitch.On())
	if log.get(recorderPathMic) != micSwitch.On() {
		t.Errorf("microphone write = %v", log.get(recorderPathMic))
	}
	if len(v.mics) != 2 {
		t.Fatalf("mics = %v", v.mics)
	}
	v.micPicker.SetSelected(1)
	if log.get(recorderPathMicDevice) != "mic" {
		t.Errorf("mic device write = %v", log.get(recorderPathMicDevice))
	}
	v.preview.onMove(30, 70)
	if log.get(recorderPathWebcamX) != int64(30) || log.get(recorderPathWebcamY) != int64(70) {
		t.Errorf("position writes = %v %v", log.get(recorderPathWebcamX), log.get(recorderPathWebcamY))
	}

	// A hotplugged microphone rebuilds the picker.
	old := v.micPicker
	srv.PutSource(pulsetest.Source(11, "usb", "USB Mic", native.InvalidIndex, ""))
	waitHeadless(t, "the new source", func() bool { return len(v.mics) == 3 })
	if v.micPicker == old {
		t.Error("the picker was not rebuilt")
	}
	// An unchanged list keeps the picker.
	same := v.micPicker
	v.syncMics(append([]deviceChoice(nil), v.mics...))
	if v.micPicker != same {
		t.Error("an unchanged list rebuilt the picker")
	}
}
