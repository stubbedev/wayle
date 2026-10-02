package portal

import (
	"errors"
	"fmt"
	"image"

	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/internal/pipewire"
)

// defaultFPS is the advertised capture rate, clamped to the output's
// refresh: 60 keeps a 60 Hz panel smooth without oversampling slower
// ones.
const defaultFPS = 60

// screenStream is one running ScreenCast stream.
type screenStream interface {
	NodeID() uint32
	Size() (width, height int32)
	Close()
}

// pipewireStream is a producer fed from a capture source.
type pipewireStream struct {
	producer      *pipewire.Producer
	width, height int32
	closeSource   func()
}

func (s *pipewireStream) NodeID() uint32              { return s.producer.NodeID() }
func (s *pipewireStream) Size() (width, height int32) { return s.width, s.height }

func (s *pipewireStream) Close() {
	s.producer.Close()
	s.closeSource()
}

// startScreenStream opens the capture for target and its PipeWire
// producer (pipewire.rs start_stream). A whole output streams from an
// ext-image-copy session (damage-driven: a static screen sends
// nothing new); a compositor without it, a region, and a window take
// one screencopy or toplevel capture per frame.
func startScreenStream(target captureTarget, cursor bool, fps uint32) (screenStream, error) {
	if target.kind == targetOutput {
		if s, err := capture.OpenOutputStream(target.name, cursor); err == nil {
			info := s.Info()
			return startProducer(producerGeometry{
				width: info.Width, height: info.Height, stride: info.Stride, format: info.Format,
				fps: effectiveFPS(fps, info.RefreshMHz), transform: uint32(info.Transform),
			}, &extSource{stream: s}, func() { _ = s.Close() })
		} else if !errors.Is(err, capture.ErrUnsupported) {
			return nil, err
		}
	}
	return startSnapshotStream(target, cursor, fps)
}

// producerGeometry is the stream's fixed layout, from its first frame.
type producerGeometry struct {
	width, height, stride int
	format                capture.Format
	fps, transform        uint32
}

func startProducer(g producerGeometry, src pipewire.Source, closeSource func()) (screenStream, error) {
	p, err := pipewire.Start(pipewire.Config{
		Width: uint32(g.width), Height: uint32(g.height), Stride: uint32(g.stride),
		Format: videoFormat(g.format), FPS: g.fps, Transform: g.transform, Source: src,
	})
	if err != nil {
		closeSource()
		return nil, err
	}
	return &pipewireStream{producer: p, width: int32(g.width), height: int32(g.height), closeSource: closeSource}, nil
}

// videoFormat maps the wl_shm format the compositor returned; an
// exotic one is offered as BGRx, as the Rust producer does.
func videoFormat(f capture.Format) pipewire.VideoFormat {
	switch f {
	case capture.FormatARGB8888:
		return pipewire.VideoBGRA
	case capture.FormatXBGR8888:
		return pipewire.VideoRGBx
	case capture.FormatABGR8888:
		return pipewire.VideoRGBA
	}
	return pipewire.VideoBGRx
}

// extSource hands the producer the ext-image-copy stream's newest
// frame, once each.
type extSource struct {
	stream *capture.Stream
	seq    uint64
	sent   bool
}

func (e *extSource) Fill(dst []byte) ([]pipewire.Rect, bool) {
	f := e.stream.Latest()
	if f == nil || (e.sent && f.Seq == e.seq) {
		return nil, false
	}
	if f.ReadInto(dst) == 0 {
		return nil, false
	}
	e.seq, e.sent = f.Seq, true
	return rects(f.Damage), true
}

func rects(damage []capture.Rect) []pipewire.Rect {
	out := make([]pipewire.Rect, len(damage))
	for i, r := range damage {
		out[i] = pipewire.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
	}
	return out
}

// snapshotSource captures a fresh frame on every fill.
type snapshotSource struct {
	grab   func(dst []byte) (*capture.Frame, error)
	stride int
	height int
	buf    []byte
}

func (s *snapshotSource) Fill(dst []byte) ([]pipewire.Rect, bool) {
	f, err := s.grab(s.buf)
	if err != nil {
		return nil, false
	}
	s.buf = f.Data
	// A frame whose layout changed (a resized window) does not fit the
	// negotiated buffers; it is skipped rather than streamed torn.
	if f.Stride != s.stride || f.Height != s.height || len(dst) < s.stride*s.height {
		return nil, false
	}
	copyRows(dst, f)
	return rects(f.Damage), true
}

// copyRows copies the frame top-down, undoing a y-inverted screencopy.
func copyRows(dst []byte, f *capture.Frame) {
	if !f.YInvert {
		copy(dst, f.Data[:f.Stride*f.Height])
		return
	}
	for row := range f.Height {
		src := f.Data[(f.Height-1-row)*f.Stride:]
		copy(dst[row*f.Stride:(row+1)*f.Stride], src[:f.Stride])
	}
}

// startSnapshotStream captures per frame on a connection of its own.
func startSnapshotStream(target captureTarget, cursor bool, fps uint32) (screenStream, error) {
	c, err := capture.Connect()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to wayland: %w", err)
	}
	opts := capture.Options{Cursor: cursor}
	var grab func([]byte) (*capture.Frame, error)
	var refresh int32
	var transform capture.Transform
	switch target.kind {
	case targetOutput, targetRegion:
		o, ok := findOutput(c, target.name)
		if !ok {
			_ = c.Close()
			return nil, fmt.Errorf("output '%s' is gone", target.name)
		}
		refresh, transform = o.RefreshMHz, o.Transform
		if target.kind == targetOutput {
			grab = func(dst []byte) (*capture.Frame, error) { opts.Dst = dst; return c.CaptureOutput(o, opts) }
		} else {
			region := image.Rect(int(target.x), int(target.y), int(target.x+target.width), int(target.y+target.height))
			grab = func(dst []byte) (*capture.Frame, error) {
				opts.Dst = dst
				return c.CaptureOutputRegion(o, region, opts)
			}
		}
	case targetWindow:
		t, ok := findToplevel(c, target.name)
		if !ok {
			_ = c.Close()
			return nil, fmt.Errorf("window '%s' is gone", target.name)
		}
		grab = func(dst []byte) (*capture.Frame, error) { opts.Dst = dst; return c.CaptureToplevel(t, opts) }
	}
	first, err := grab(nil)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initial capture failed: %w", err)
	}
	src := &snapshotSource{grab: grab, stride: first.Stride, height: first.Height, buf: first.Data}
	return startProducer(producerGeometry{
		width: first.Width, height: first.Height, stride: first.Stride, format: first.Format,
		fps: effectiveFPS(fps, refresh), transform: uint32(transform),
	}, src, func() { _ = c.Close() })
}

func findOutput(c *capture.Client, name string) (capture.Output, bool) {
	for _, o := range c.Outputs() {
		if o.Name == name {
			return o, true
		}
	}
	return capture.Output{}, false
}

func findToplevel(c *capture.Client, id string) (capture.Toplevel, bool) {
	list, err := c.Toplevels()
	if err != nil {
		return capture.Toplevel{}, false
	}
	for _, t := range list {
		if t.Identifier == id {
			return t, true
		}
	}
	return capture.Toplevel{}, false
}
