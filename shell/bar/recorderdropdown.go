package bar

import (
	"context"
	"slices"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/recorder"
)

// Recorder config paths the dropdown writes.
const (
	recorderPathMic       = "modules.recorder.microphone"
	recorderPathMicDevice = "modules.recorder.microphone-device"
	recorderPathSystem    = "modules.recorder.system-audio"
	recorderPathWebcam    = "modules.recorder.webcam-enabled"
	recorderPathCamDevice = "modules.recorder.webcam-device"
	recorderPathWebcamX   = "modules.recorder.webcam-x"
	recorderPathWebcamY   = "modules.recorder.webcam-y"
)

// previewGeometry is preview_geometry: the 16:9 screen frame fit to the
// popover width and the webcam frame at its configured size.
func previewGeometry(width int32, sizePct uint8) (pw, ph, cw, ch int32) {
	pw = max(width-48, 160)
	ph = pw * 9 / 16
	cw = max(pw*int32(sizePct)/100, 24)
	ch = max(cw*9/16, 14)
	return pw, ph, cw, ch
}

// pctFromPx is pct_from_px.
func pctFromPx(px, margin, travel int32) uint8 {
	if travel <= 0 {
		return 0
	}
	return uint8(min(max(px-margin, 0), travel) * 100 / travel)
}

// webcamPreview is the draggable position preview: the outer frame is
// the screen, the inner one the webcam picture-in-picture, inset by the
// recording pipeline's margin. Both are classed boxes the stylesheet
// paints (recorder-position-preview, recorder-position-cam); a drag
// tracks the pointer from where it grabbed the frame, and the release
// reports the position as percentages.
type webcamPreview struct {
	widget.Base
	pw, ph, cw, ch int32
	// camX and camY are the frame's offset inside the preview.
	camX, camY int32
	frame, cam *widget.Box

	pointer  widget.Point
	grabX    int32
	grabY    int32
	dragging bool
	onMove   func(x, y uint8)
}

func newWebcamPreview(width int32, sizePct, x, y uint8) *webcamPreview {
	p := &webcamPreview{}
	p.frame = widget.NewBox(widget.Row, 0, 0)
	p.frame.AddClass("recorder-position-preview")
	p.cam = widget.NewBox(widget.Row, 0, 0)
	p.cam.AddClass("recorder-position-cam")
	p.pw, p.ph, p.cw, p.ch = previewGeometry(width, sizePct)
	p.place(x, y)
	return p
}

func (p *webcamPreview) travel() (margin, w, h int32) {
	margin = recorder.WebcamMargin(p.pw, p.ph)
	return margin, max(p.pw-p.cw-2*margin, 0), max(p.ph-p.ch-2*margin, 0)
}

// place is reposition_cam.
func (p *webcamPreview) place(x, y uint8) {
	margin, tw, th := p.travel()
	p.camX = margin + tw*int32(min(x, 100))/100
	p.camY = margin + th*int32(min(y, 100))/100
	p.Invalidate()
}

// percent is the frame's position as the config stores it.
func (p *webcamPreview) percent() (uint8, uint8) {
	margin, tw, th := p.travel()
	return pctFromPx(p.camX, margin, tw), pctFromPx(p.camY, margin, th)
}

// Arrange lays the frame over the whole preview and the cam at its
// offset; the boxes paint from their cascades.
func (p *webcamPreview) Arrange(rect render.Rect) {
	p.Base.Arrange(rect)
	p.layOut()
	widget.SetParents(p, p.frame, p.cam)
}

// layOut positions both boxes against the current bounds.
func (p *webcamPreview) layOut() {
	b := p.Bounds()
	if b.Empty() {
		return
	}
	p.frame.Arrange(render.Rect{X: b.X, Y: b.Y, W: int(p.pw), H: int(p.ph)})
	p.cam.Arrange(render.Rect{X: b.X + int(p.camX), Y: b.Y + int(p.camY), W: int(p.cw), H: int(p.ch)})
}

func (p *webcamPreview) Measure(widget.Constraints) widget.Size {
	return widget.Size{W: int(p.pw), H: int(p.ph)}
}

func (p *webcamPreview) Paint(cv *render.Canvas) {
	widget.PaintChild(cv, p.frame)
	widget.PaintChild(cv, p.cam)
}

// Children exposes the boxes to the tree walks.
func (p *webcamPreview) Children() []widget.Widget { return []widget.Widget{p.frame, p.cam} }

func (p *webcamPreview) HitTest(pt widget.Point) widget.Widget { return p.HitLeaf(p, pt) }

// HoverMove tracks the pointer, so a press knows where it grabbed.
func (p *webcamPreview) HoverMove(pt widget.Point) { p.pointer = pt }

// SetPressed is drag begin and end.
func (p *webcamPreview) SetPressed(on bool) {
	b := p.Bounds()
	switch {
	case on && !p.dragging:
		p.dragging = true
		p.grabX = int32(p.pointer.X-b.X) - p.camX
		p.grabY = int32(p.pointer.Y-b.Y) - p.camY
	case !on && p.dragging:
		p.dragging = false
		if p.onMove != nil {
			p.onMove(p.percent())
		}
	}
	p.Invalidate()
}

// DragMove is drag update: the frame follows the pointer, clamped to
// the inset travel.
func (p *webcamPreview) DragMove(pt widget.Point) {
	p.pointer = pt
	if !p.dragging {
		return
	}
	b := p.Bounds()
	margin, tw, th := p.travel()
	p.camX = min(max(int32(pt.X-b.X)-p.grabX, margin), margin+tw)
	p.camY = min(max(int32(pt.Y-b.Y)-p.grabY, margin), margin+th)
	p.layOut()
	p.Invalidate()
}

// CursorName is the grab and grabbing cursors.
func (p *webcamPreview) CursorName() string {
	if p.dragging {
		return "grabbing"
	}
	return "grab"
}

// recorderView is the recorder dropdown: the live status in the
// header, the record and pause buttons, the audio card, and the webcam
// card (hidden without a camera).
type recorderView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	status               *widget.Box
	statusDot            *widget.Box
	time                 *widget.Label
	record               *widget.Button
	recordIcon           *widget.Icon
	recordLabel          *widget.Label
	pause                *widget.Button
	pauseIcon            *widget.Icon
	micRow               *widget.Box
	micPicker            *widget.Dropdown
	mics, cams           []recorder.DeviceChoice
	webcamHeader, webcam *widget.Box
	preview              *webcamPreview

	once   sync.Once
	cancel context.CancelFunc
}

func recorderDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &recorderView{ctx: ctx, font: font, px: px, cancel: func() {}}
	v.Box = widget.NewBox(widget.Column, 0, 14)
	v.AddClass("dropdown", "recorder-dropdown")
	cfg := ctx.Config.Recorder

	v.status = widget.NewBox(widget.Row, 0, 0)
	v.status.AddClass("recorder-status")
	v.statusDot = widget.NewBox(widget.Row, 0, 0)
	v.statusDot.AddClass("recorder-status-dot")
	v.time = widget.NewLabel(font, px, "", 0)
	v.time.AddClass("recorder-status-time")
	v.status.Append(v.statusDot, false)
	v.status.Append(v.time, false)
	v.Append(dropdownHeader(font, px, "ld-video-symbolic", i18n.T("dropdown-recorder-title"), v.status), false)

	// The DropdownContent template: everything below the header; its
	// classes let the stylesheet's `.recorder-dropdown-content` rules
	// reach the rows, cards, and section headers.
	content := widget.NewBox(widget.Column, 0, 0)
	content.AddClass("dropdown-content", "recorder-dropdown-content")
	v.Append(content, true)

	content.Append(v.controls(), false)

	content.Append(v.sectionHeader("ld-mic-symbolic", i18n.T("dropdown-recorder-section-audio")), false)
	audio := v.card()
	audio.Append(v.switchRow(i18n.T("dropdown-recorder-microphone"), cfg.Microphone, recorderPathMic), false)
	v.mics = microphoneSources(ctx.Pulse)
	v.micRow = v.row(i18n.T("dropdown-recorder-microphone-device"))
	v.micPicker = v.picker(v.mics, cfg.MicrophoneDevice, recorderPathMicDevice, func() []recorder.DeviceChoice { return v.mics })
	v.micRow.Append(v.micPicker, false)
	audio.Append(v.micRow, false)
	audio.Append(v.switchRow(i18n.T("dropdown-recorder-system-audio"), cfg.SystemAudio, recorderPathSystem), false)
	content.Append(audio, false)

	v.cams = recorderCameras()
	v.webcamHeader = v.sectionHeader("ld-camera-symbolic", i18n.T("dropdown-recorder-section-webcam"))
	v.webcam = v.card()
	v.webcam.Append(v.switchRow(i18n.T("dropdown-recorder-webcam"), cfg.WebcamEnabled, recorderPathWebcam), false)
	camRow := v.row(i18n.T("dropdown-recorder-webcam-device"))
	camRow.Append(v.picker(v.cams, cfg.WebcamDevice, recorderPathCamDevice, func() []recorder.DeviceChoice { return v.cams }), false)
	v.webcam.Append(camRow, false)
	position := widget.NewBox(widget.Column, 6, 0)
	position.AddClass("recorder-row")
	position.Append(widget.NewLabel(font, px, i18n.T("dropdown-recorder-position"), 0), false)
	width := int32(360)
	if w, _, ok := dropdownDims("recorder", ctx.Config); ok {
		width = int32(w)
	}
	v.preview = newWebcamPreview(width, uint8(cfg.WebcamSize), uint8(cfg.WebcamX), uint8(cfg.WebcamY))
	v.preview.onMove = v.webcamMoved
	centered := widget.NewBox(widget.Row, 0, 0)
	centered.Append(widget.NewSpacer(0, 0), true)
	centered.Append(v.preview, false)
	centered.Append(widget.NewSpacer(0, 0), true)
	position.Append(centered, false)
	v.webcam.Append(position, false)
	hasCamera := len(v.cams) > 1
	v.webcamHeader.SetVisible(hasCamera)
	v.webcam.SetVisible(hasCamera)
	content.Append(v.webcamHeader, false)
	content.Append(v.webcam, false)

	v.applyState(v.snapshot())
	v.follow()
	return v
}

// controls is the record/stop toggle and the pause button.
func (v *recorderView) controls() widget.Widget {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("recorder-controls")
	content := widget.NewBox(widget.Row, 8, 0)
	v.recordIcon = widget.NewThemeIcon("ld-circle-dot-symbolic", int(v.px))
	v.recordLabel = widget.NewLabel(v.font, v.px, "", 0)
	content.Append(widget.NewSpacer(0, 0), true)
	content.Append(v.recordIcon, false)
	content.Append(v.recordLabel, false)
	content.Append(widget.NewSpacer(0, 0), true)
	v.record = dropdownButton(content, "recorder-record-button", func() {
		if v.ctx.Recorder != nil {
			v.ctx.Recorder.Toggle()
		}
	})
	row.Append(v.record, true)
	v.pauseIcon = widget.NewThemeIcon("ld-pause-symbolic", int(v.px))
	v.pause = dropdownButton(v.pauseIcon, "recorder-pause-button", func() {
		if r := v.ctx.Recorder; r != nil {
			r.SetPaused(!r.Snapshot().Paused)
		}
	})
	v.pause.AddClass("secondary")
	row.Append(v.pause, false)
	return row
}

func (v *recorderView) sectionHeader(icon, title string) *widget.Box {
	row := widget.NewBox(widget.Row, 6, 0)
	row.AddClass("recorder-section-header")
	glyph := widget.NewThemeIcon(icon, int(v.px))
	row.Append(glyph, false)
	label := widget.NewLabel(v.font, v.px, title, 0)
	label.AddClass("section-label")
	row.Append(label, true)
	return row
}

func (v *recorderView) card() *widget.Box {
	c := widget.NewBox(widget.Column, 0, 10)
	c.AddClass("card", "recorder-card")
	return c
}

func (v *recorderView) row(title string) *widget.Box {
	r := widget.NewBox(widget.Row, 0, 0)
	r.AddClass("recorder-row")
	r.Append(widget.NewLabel(v.font, v.px, title, 0), true)
	return r
}

// switchRow is a labeled switch writing a boolean config key.
func (v *recorderView) switchRow(title string, on bool, path string) *widget.Box {
	r := v.row(title)
	sw := widget.NewSwitch(on)
	sw.OnChanged = func(on bool) { v.ctx.setConfig(path, on) }
	r.Append(sw, false)
	return r
}

// picker is a device selector writing the chosen id; choices reads the
// list current at selection time.
func (v *recorderView) picker(list []recorder.DeviceChoice, saved, path string, choices func() []recorder.DeviceChoice) *widget.Dropdown {
	d := widget.NewDropdown(v.font, v.px, recorder.ChoiceLabels(list), recorder.ChoiceIndex(list, saved))
	d.OnSelect = func(i int) {
		if c := choices(); i >= 0 && i < len(c) {
			v.ctx.setConfig(path, c[i].ID)
		}
	}
	return d
}

// webcamMoved is WebcamMoved.
func (v *recorderView) webcamMoved(x, y uint8) {
	v.ctx.setConfig(recorderPathWebcamX, int64(x))
	v.ctx.setConfig(recorderPathWebcamY, int64(y))
}

func (v *recorderView) snapshot() recorder.Change {
	if v.ctx.Recorder == nil {
		return recorder.Change{}
	}
	return v.ctx.Recorder.Snapshot()
}

// applyState is StateChanged plus the view's #[watch]es. The record
// button's `primary`/`danger` classes and the status dot's `paused`
// class carry the colors; the stylesheet paints them.
func (v *recorderView) applyState(c recorder.Change) {
	v.status.SetVisible(c.Active)
	v.time.SetText(recorder.FormatElapsed(c.ElapsedSecs))
	if c.Paused {
		v.statusDot.AddClass("paused")
	} else {
		v.statusDot.RemoveClass("paused")
	}
	v.record.RemoveClass("danger", "primary")
	if c.Active {
		v.record.AddClass("danger")
		v.recordIcon.SetThemeName("ld-square-symbolic")
		v.recordLabel.SetText(i18n.T("dropdown-recorder-stop"))
	} else {
		v.record.AddClass("primary")
		v.recordIcon.SetThemeName("ld-circle-dot-symbolic")
		v.recordLabel.SetText(i18n.T("dropdown-recorder-record"))
	}
	v.pause.SetEnabled(c.Active)
	if c.Paused {
		v.pauseIcon.SetThemeName("ld-play-symbolic")
		v.pause.SetTooltip(i18n.T("dropdown-recorder-resume"))
	} else {
		v.pauseIcon.SetThemeName("ld-pause-symbolic")
		v.pause.SetTooltip(i18n.T("dropdown-recorder-pause"))
	}
}

// syncMics is MicrophonesUpdated: a changed source list rebuilds the
// picker, keeping the saved selection when it is still there.
func (v *recorderView) syncMics(mics []recorder.DeviceChoice) {
	if slices.Equal(mics, v.mics) {
		return
	}
	v.mics = mics
	saved := v.ctx.Config.Recorder.MicrophoneDevice
	next := v.picker(mics, saved, recorderPathMicDevice, func() []recorder.DeviceChoice { return v.mics })
	v.micRow.Remove(v.micPicker)
	v.micRow.Append(next, false)
	v.micPicker = next
}

// follow tracks the recorder state and microphone hotplug until the
// dropdown closes.
func (v *recorderView) follow() {
	life, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	if r := v.ctx.Recorder; r != nil {
		changes, stop := r.Changes()
		go func() {
			defer stop()
			for {
				select {
				case <-life.Done():
					return
				case c, ok := <-changes:
					if !ok {
						return
					}
					v.ctx.Invoke(func() { v.applyState(c) })
				}
			}
		}()
	}
	if src := v.ctx.Pulse; src != nil {
		followTicks(v.ctx, life, "recorder microphones", src.Subscribe,
			func(context.Context) []recorder.DeviceChoice { return microphoneSources(src) }, v.syncMics)
	}
}

// dropdownClosed implements dropdownCloser.
func (v *recorderView) dropdownClosed() { v.once.Do(func() { v.cancel() }) }
