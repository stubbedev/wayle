// Package osd is the on-screen display (wayle-shell osd/): one layer
// surface on the configured monitor flashing volume, microphone,
// brightness, and lock-key events and the toasts `wayle toast` pushes,
// styled by the stylesheet's .osd rules and dismissed after
// osd.duration.
package osd

import (
	"log"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/shell/layering"
	"github.com/stubbedev/wayle/shell/reveal"
	"github.com/stubbedev/wayle/styling"
)

// readyDelay is the start-up quiet period: the services report their
// initial state then, which is not an event to flash.
const readyDelay = 500 * time.Millisecond

// Window is the part of a layer window the OSD drives
// (app.LayerWindow).
type Window interface {
	Close()
	SetAnchor(app.Anchor) error
	SetMargin(app.Margins) error
	SetLayer(app.Layer) error
}

// Deps are the OSD's collaborators.
type Deps struct {
	// Open maps a layer surface.
	Open func(app.LayerConfig) (Window, error)
	// Outputs lists the connected outputs, the primary first.
	Outputs func() []*app.Output
	// Invoke runs fn on the UI loop.
	Invoke func(fn func())
	// After runs fn once d has passed, off the loop.
	After func(d time.Duration, fn func())
	// Sheet styles the surface: the labels and icons take their ink
	// from its cascade. Font is the labels' face.
	Sheet *widget.Stylesheet
	Font  render.Font
}

// face is the mapped surface and the widgets an event updates.
type face struct {
	win          Window
	output       *app.Output
	rev          *widget.Revealer
	container    *widget.Box
	sliderHeader *widget.Box
	sliderIcon   *widget.Icon
	sliderLabel  *widget.Label
	value        *widget.Label
	toggleHeader *widget.Box
	toggleIcon   *widget.Icon
	toggleLabel  *widget.Label
	bar          *widget.ProgressBar
}

// Osd is the display. Its exported methods may be called from any
// goroutine; its state lives on the UI loop.
type Osd struct {
	d         Deps
	cfg       *config.Config
	ready     bool
	dismissID uint32
	face      *face
}

// New builds the OSD; nothing maps until the first event after the
// start-up quiet period.
func New(d Deps, cfg *config.Config) *Osd {
	o := &Osd{d: d, cfg: cfg}
	d.After(readyDelay, func() { d.Invoke(func() { o.ready = true }) })
	return o
}

// Show flashes ev (show_event).
func (o *Osd) Show(ev Event) { o.d.Invoke(func() { o.show(ev) }) }

// ShowToast flashes a `wayle toast` request (handle_show_toast).
func (o *Osd) ShowToast(req widgetipc.ToastRequest) {
	o.d.Invoke(func() { o.show(toastEvent(req, o.cfg.Osd)) })
}

// SetConfig takes a reloaded config; a mapped OSD re-anchors to it
// (OsdCmd::ConfigChanged).
func (o *Osd) SetConfig(cfg *config.Config) {
	o.d.Invoke(func() {
		o.cfg = cfg
		if o.face != nil {
			o.place()
		}
	})
}

// DetachOutput drops the surface on an unplugged output; the next
// event maps it on the configured monitor again.
func (o *Osd) DetachOutput(name string) {
	o.d.Invoke(func() {
		if o.face != nil && o.face.output.Name == name {
			o.closeFace()
		}
	})
}

func (o *Osd) show(ev Event) {
	if !o.ready || !o.cfg.Osd.Enabled {
		return
	}
	o.dismissID++
	if !o.place() {
		return
	}
	o.face.apply(ev, o.cfg.Osd)
	reveal.Show(o.face.rev, o.cfg.Animations, config.AnimOsd)
	ms := ev.durationMS
	if ms == 0 {
		ms = o.cfg.Osd.Duration
	}
	id := o.dismissID
	o.d.After(time.Duration(ms)*time.Millisecond, func() { o.d.Invoke(func() { o.beginDismiss(id) }) })
}

// beginDismiss plays the exit for the latest event, then unmaps.
func (o *Osd) beginDismiss(id uint32) {
	if id != o.dismissID || o.face == nil {
		return
	}
	reveal.Hide(o.face.rev, o.cfg.Animations, config.AnimOsd, func() {
		if id == o.dismissID {
			o.closeFace()
		}
	})
}

func (o *Osd) closeFace() {
	if o.face == nil {
		return
	}
	o.face.win.Close()
	o.face = nil
}

// place is apply_position and apply_layer: the surface sits on the
// configured monitor, anchored and inset per osd.position, on its
// layer. A surface on another output is replaced. It reports whether
// a surface is mapped.
func (o *Osd) place() bool {
	output := layering.Output(o.d.Outputs(), o.cfg.Osd.Monitor)
	if output == nil {
		return false
	}
	anchor, margin, layer := anchors(o.cfg.Osd.Position), margins(o.cfg), layering.For(o.cfg.General, o.cfg.Osd.Layer)
	if o.face != nil && o.face.output == output {
		for _, err := range []error{o.face.win.SetAnchor(anchor), o.face.win.SetMargin(margin), o.face.win.SetLayer(layer)} {
			if err != nil {
				log.Printf("osd: re-anchor: %v", err)
			}
		}
		o.face.rev.SetGenieEdge(genieEdge(o.cfg.Osd.Position))
		return true
	}
	o.closeFace()
	f := o.build()
	win, err := o.d.Open(app.LayerConfig{
		Output:    output,
		Layer:     layer,
		Anchor:    anchor,
		Margin:    margin,
		Keyboard:  app.KeyboardNone,
		Namespace: "wayle-osd",
		Root:      f.root(o.d.Sheet),
	})
	if err != nil {
		log.Printf("osd: cannot map on %s: %v", output.Name, err)
		return false
	}
	f.win, f.output = win, output
	f.rev.SetGenieEdge(genieEdge(o.cfg.Osd.Position))
	o.face = f
	return true
}

// anchors is apply_position's edges: corners take both, the centered
// positions only their own edge (the surface centers along the other).
func anchors(p config.OsdPosition) app.Anchor {
	switch p {
	case config.OsdTopLeft:
		return app.AnchorTop | app.AnchorLeft
	case config.OsdTop:
		return app.AnchorTop
	case config.OsdTopRight:
		return app.AnchorTop | app.AnchorRight
	case config.OsdRight:
		return app.AnchorRight
	case config.OsdBottomRight:
		return app.AnchorBottom | app.AnchorRight
	case config.OsdBottomLeft:
		return app.AnchorBottom | app.AnchorLeft
	case config.OsdLeft:
		return app.AnchorLeft
	}
	return app.AnchorBottom
}

// margins is apply_position's insets: osd.margin (rem base 150px at the
// styling scale, truncated as the Rust `as i32`) on each anchored edge.
func margins(cfg *config.Config) app.Margins {
	m := int32(cfg.Osd.Margin.ResolvePx(config.OsdMarginBaseRem*styling.RemBase, float64(cfg.Styling.Scale)))
	a := anchors(cfg.Osd.Position)
	var out app.Margins
	if a&app.AnchorTop != 0 {
		out.Top = m
	}
	if a&app.AnchorBottom != 0 {
		out.Bottom = m
	}
	if a&app.AnchorLeft != 0 {
		out.Left = m
	}
	if a&app.AnchorRight != 0 {
		out.Right = m
	}
	return out
}

// genieEdge is the edge a genie collapses toward (genie_edge).
func genieEdge(p config.OsdPosition) widget.Edge {
	switch p {
	case config.OsdTop, config.OsdTopLeft, config.OsdTopRight:
		return widget.EdgeTop
	case config.OsdLeft:
		return widget.EdgeLeft
	case config.OsdRight:
		return widget.EdgeRight
	}
	return widget.EdgeBottom
}
