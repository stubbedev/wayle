package portal

import (
	"errors"
	"fmt"

	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/internal/gbm"
	"github.com/stubbedev/wayle/internal/pipewire"
)

// The zero-copy path (wayle-share-preview dmabuf.rs, pipewire.rs's
// run_loop_dmabuf): a whole output's screencopy frames land straight in
// gbm buffers, one bound to each PipeWire buffer for the stream's life,
// which the consumer imports as dmabufs. Everything is best effort: no
// render node, a format the compositor offers only as shm, an
// allocation or import it refuses - the stream stays on shared memory.

// gpuAllocator is the gbm device the source allocates from.
type gpuAllocator interface {
	Alloc(fourcc, width, height uint32, modifiers []uint64) (*gbm.Buffer, error)
	Close()
}

// gpuCapturer is the capture connection the source copies with.
type gpuCapturer interface {
	Import(capture.Dmabuf) (importedBuffer, error)
	CopyInto(o capture.Output, cursor bool, buf importedBuffer) error
}

// importedBuffer is the compositor's import of one buffer.
type importedBuffer interface{ Destroy() }

// screencopy is the gpuCapturer over a capture client.
type screencopy struct{ c *capture.Client }

func (s screencopy) Import(d capture.Dmabuf) (importedBuffer, error) { return s.c.ImportDmabuf(d) }

// CopyInto takes the next frame without waiting for damage: the stream
// fills at its own rate, and an idle output must not stall the loop.
func (s screencopy) CopyInto(o capture.Output, cursor bool, buf importedBuffer) error {
	b, ok := buf.(*capture.DmabufBuffer)
	if !ok {
		return errors.New("screencast: a buffer this client did not import")
	}
	return s.c.CopyOutputDmabuf(o, cursor, b)
}

// gpuSource is a pipewire.DmabufSource over screencopy into gbm buffers;
// a consumer that takes only shared memory is fed by the shm source.
type gpuSource struct {
	pipewire.Source // the shm fallback
	dev             gpuAllocator
	cap             gpuCapturer
	output          capture.Output
	cursor          bool
	fourcc          uint32
	video           pipewire.VideoFormat
	width, height   uint32
	modifier        uint64
}

// gpuBuffer is one bound buffer: the gbm allocation and the
// compositor's import of it.
type gpuBuffer struct {
	bo       *gbm.Buffer
	imported importedBuffer
}

func (b *gpuBuffer) Fd() int        { return b.bo.Planes[0].Fd }
func (b *gpuBuffer) Offset() uint32 { return b.bo.Planes[0].Offset }
func (b *gpuBuffer) Stride() uint32 { return b.bo.Planes[0].Stride }

func (b *gpuBuffer) Release() {
	b.imported.Destroy()
	b.bo.Release()
}

func (s *gpuSource) Modifier() uint64 { return s.modifier }

func (s *gpuSource) DmabufVideoFormat() pipewire.VideoFormat { return s.video }

// AllocDmabuf allocates and imports one buffer at the probed modifier.
func (s *gpuSource) AllocDmabuf() (pipewire.Dmabuf, error) {
	return s.alloc([]uint64{s.modifier})
}

func (s *gpuSource) alloc(modifiers []uint64) (*gpuBuffer, error) {
	bo, err := s.dev.Alloc(s.fourcc, s.width, s.height, modifiers)
	if err != nil {
		return nil, err
	}
	// Packed formats only: one plane is what the stream describes.
	if len(bo.Planes) != 1 {
		bo.Release()
		return nil, fmt.Errorf("screencast: a %d-plane buffer", len(bo.Planes))
	}
	p := bo.Planes[0]
	imported, err := s.cap.Import(capture.Dmabuf{
		Width: int(s.width), Height: int(s.height), Fourcc: s.fourcc, Modifier: bo.Modifier,
		Planes: []capture.DmabufPlane{{Fd: uintptr(p.Fd), Offset: p.Offset, Stride: p.Stride}},
	})
	if err != nil {
		bo.Release()
		return nil, err
	}
	return &gpuBuffer{bo: bo, imported: imported}, nil
}

// FillDmabuf screencopies the output into the bound buffer.
func (s *gpuSource) FillDmabuf(d pipewire.Dmabuf) ([]pipewire.Rect, bool) {
	b, ok := d.(*gpuBuffer)
	if !ok {
		return nil, false
	}
	// A plain copy reports no damage: the whole frame changed.
	if err := s.cap.CopyInto(s.output, s.cursor, b.imported); err != nil {
		return nil, false
	}
	return nil, true
}

// errNoDmabuf is a stream the zero-copy path does not take.
var errNoDmabuf = errors.New("screencast: no dmabuf path")

// probeGPUSource readies the zero-copy path for an output whose shm
// frames are first's: the compositor must offer the same format as a
// dmabuf, and one buffer must allocate, import and take a frame; its
// modifier is the one the stream offers.
func probeGPUSource(shm pipewire.Source, dev gpuAllocator, c gpuCapturer, offered capture.DmabufFormat, o capture.Output, cursor bool, first *capture.Frame) (*gpuSource, error) {
	// The size must match the stream's; the format may differ (a GPU
	// compositor's shm is 24-bit, its dmabufs 32-bit) but must be one
	// packed 32-bit layout.
	format := capture.FormatFromFourcc(offered.Fourcc)
	if offered.Width != first.Width || offered.Height != first.Height || format.BytesPerPixel() != 4 {
		return nil, fmt.Errorf("%w: a %v %dx%d dmabuf for a %dx%d stream", errNoDmabuf, format, offered.Width, offered.Height, first.Width, first.Height)
	}
	s := &gpuSource{
		Source: shm, dev: dev, cap: c, output: o, cursor: cursor,
		fourcc: offered.Fourcc, video: videoFormat(format), width: uint32(offered.Width), height: uint32(offered.Height),
	}
	probe, err := s.alloc(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoDmabuf, err)
	}
	defer probe.Release()
	if err := c.CopyInto(o, cursor, probe.imported); err != nil {
		return nil, fmt.Errorf("%w: %w", errNoDmabuf, err)
	}
	s.modifier = probe.bo.Modifier
	return s, nil
}
