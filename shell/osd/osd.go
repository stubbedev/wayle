// Package osd is the on-screen display: a transient overlay window
// per output showing volume, microphone, and brightness events, with
// the auto-dismiss timer the config's duration drives.
package osd

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/styling"
)

// Event is one OSD flash.
type Event struct {
	// Kind names the source ("volume", "input-volume", "brightness");
	// it doubles as the CSS class.
	Kind string
	// Icon is the theme icon name.
	Icon string
	// Label names the controlled device.
	Label string
	// Value is the 0-100 percentage; Muted shows "Muted" instead of
	// the number.
	Value float64
	Muted bool
}

// anchors maps the schema's position onto layer-shell anchors.
func anchors(position config.OsdPosition) (app.Anchor, bool) {
	switch position {
	case config.OsdTopLeft:
		return app.AnchorTop | app.AnchorLeft, true
	case config.OsdTop:
		return app.AnchorTop | app.AnchorLeft | app.AnchorRight, true
	case config.OsdTopRight:
		return app.AnchorTop | app.AnchorRight, true
	case config.OsdRight:
		return app.AnchorRight | app.AnchorTop | app.AnchorBottom, true
	case config.OsdBottomRight:
		return app.AnchorBottom | app.AnchorRight, true
	case config.OsdBottom:
		return app.AnchorBottom | app.AnchorLeft | app.AnchorRight, true
	case config.OsdBottomLeft:
		return app.AnchorBottom | app.AnchorLeft, true
	case config.OsdLeft:
		return app.AnchorLeft | app.AnchorTop | app.AnchorBottom, true
	}
	return 0, false
}

// marginsFor converts the rem margin into edge insets for the
// position.
func marginsFor(cfg config.OsdConfig) app.Margins {
	m := int32(math.Round(cfg.Margin.ResolvePx(config.OsdMarginBaseRem*16, 1)))
	var out app.Margins
	switch cfg.Position {
	case config.OsdTopLeft, config.OsdTop, config.OsdTopRight:
		out.Top = m
	case config.OsdBottomLeft, config.OsdBottom, config.OsdBottomRight:
		out.Bottom = m
	}
	switch cfg.Position {
	case config.OsdTopLeft, config.OsdBottomLeft, config.OsdLeft:
		out.Left = m
	case config.OsdTopRight, config.OsdBottomRight, config.OsdRight:
		out.Right = m
	}
	return out
}

// face is one output's OSD window: the layer surface and its widget
// tree.
type face struct {
	win    *app.LayerWindow
	icon   *widget.Icon
	label  *widget.Label
	slider *widget.Slider
	value  *widget.Label
}

// setEvent applies an event to the face's widgets.
func (f *face) setEvent(ev Event) {
	f.icon.SetThemeName(ev.Icon)
	f.label.SetText(ev.Label)
	f.slider.SetValue(clamp01(ev.Value / 100))
	text := ""
	if ev.Value >= 0 {
		text = strconv.FormatFloat(ev.Value, 'f', 0, 64) + "%"
	}
	if ev.Muted {
		text = "Muted"
	}
	f.value.SetText(text)
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// Osd manages one face per output and the shared dismiss timer.
// Windows are created on the first Show and closed on dismiss, so a
// quiet session maps nothing.
type Osd struct {
	app  *app.Application
	cfg  config.OsdConfig
	font render.Font
	pal  *styling.Palette

	mu      sync.Mutex
	outputs map[string]*app.Output
	faces   map[string]*face
	timer   *time.Timer
	current Event
}

// New builds the OSD.
func New(application *app.Application, cfg config.OsdConfig, font render.Font, pal *styling.Palette) *Osd {
	return &Osd{app: application, cfg: cfg, font: font, pal: pal, outputs: make(map[string]*app.Output), faces: make(map[string]*face)}
}

// Show flashes the event on every attached face for the dismiss
// duration, creating missing faces. Calling Show again re-arms the
// timer (the Rust reset behavior).
func (o *Osd) Show(ev Event) {
	if o.app == nil || !o.cfg.Enabled {
		return
	}
	o.mu.Lock()
	o.current = ev
	for name, out := range o.outputs {
		if f, err := o.ensure(name, out); err == nil {
			f.setEvent(ev)
		}
	}
	if o.timer != nil {
		o.timer.Stop()
	}
	o.timer = time.AfterFunc(time.Duration(o.cfg.Duration)*time.Millisecond, o.dismiss)
	o.mu.Unlock()
}

// AttachOutput adds an output to the fan-out. The bar calls it once
// per output at startup.
func (o *Osd) AttachOutput(name string, output *app.Output) {
	o.mu.Lock()
	o.outputs[name] = output
	o.mu.Unlock()
}

// SetConfig applies a reloaded [osd] section: the next Show uses it,
// and faces on screen close so they reopen at the new position and
// margin (the Rust OSD re-anchors on its config watchers).
func (o *Osd) SetConfig(cfg config.OsdConfig) {
	o.mu.Lock()
	o.cfg = cfg
	o.mu.Unlock()
	o.dismiss()
}

// dismiss closes the windows; the next Show recreates them.
func (o *Osd) dismiss() {
	o.mu.Lock()
	faces := o.faces
	o.faces = make(map[string]*face)
	o.timer = nil
	o.mu.Unlock()
	for _, f := range faces {
		f.win.Close()
	}
}

// ensure creates the face for one output when missing.
func (o *Osd) ensure(outputName string, output *app.Output) (*face, error) {
	if f, ok := o.faces[outputName]; ok {
		return f, nil
	}
	anchor, ok := anchors(o.cfg.Position)
	if !ok {
		return nil, fmt.Errorf("unknown position %q", o.cfg.Position)
	}
	bg, _ := o.pal.Token(config.TokenBgOverlay)
	fg, _ := o.pal.Token(config.TokenFgDefault)
	icon := widget.NewThemeIcon("", 24)
	label := widget.NewLabel(o.font, 14, "", fg)
	slider := widget.NewSlider(0, 1, 0.01, 0)
	value := widget.NewLabel(o.font, 14, "", fg)
	row := widget.NewBox(widget.Row, 10, 0)
	row.Append(icon, false)
	row.Append(label, true)
	row.Append(slider, false)
	row.Append(value, false)
	pad := widget.NewBox(widget.Row, 0, 14)
	pad.Append(row, false)

	win, err := o.app.NewLayer(app.LayerConfig{
		Output:        output,
		Layer:         app.LayerOverlay,
		Anchor:        anchor,
		Margin:        marginsFor(o.cfg),
		Width:         360,
		Height:        72,
		ExclusiveZone: -1,
		Namespace:     "wayle-osd",
		Root:          pad,
		Background:    bg,
	})
	if err != nil {
		return nil, err
	}
	f := &face{win: win, icon: icon, label: label, slider: slider, value: value}
	o.faces[outputName] = f
	return f, nil
}

// applyToast resolves a toast request against the presets: explicit
// fields win, and the preset supplies the defaults (osd/methods.rs's
// handle_show_toast). An absent label everywhere is an error.
func (o *Osd) applyToast(req widgetipc.ToastRequest) (Event, error) {
	var preset config.ToastPreset
	if req.Preset != nil {
		p, ok := o.cfg.Preset(*req.Preset)
		if !ok {
			return Event{}, fmt.Errorf("unknown toast preset %q", *req.Preset)
		}
		preset = p
	}
	label := deref(preset.Label)
	if req.Label != nil {
		label = *req.Label
	}
	if label == "" {
		return Event{}, errors.New("a toast needs a label or preset")
	}
	icon := deref(preset.Icon)
	if req.Icon != nil {
		icon = *req.Icon
	}
	ev := Event{
		Kind:  "toast",
		Icon:  icon,
		Label: label,
	}
	if req.Percentage != nil {
		ev.Value = clamp01(*req.Percentage) * 100
	} else {
		ev.Value = -1 // no progress bar
	}
	return ev, nil
}

// ShowToast flashes a toast request.
func (o *Osd) ShowToast(req widgetipc.ToastRequest) error {
	ev, err := o.applyToast(req)
	if err != nil {
		return err
	}
	o.Show(ev)
	return nil
}

// Current reads the event on display (tests).
func (o *Osd) Current() Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.current
}

// deref reads an optional preset string; unset is empty.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
