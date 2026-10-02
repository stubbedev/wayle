package pipewire

import (
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/pipewire/pwtest"
)

func TestObjectHasFindsTheModifier(t *testing.T) {
	mod := uint64(0x00ffffffffffffff)
	if !objectHas(formatPod(8, 8, 30, VideoBGRx, &mod), formatVideoModifier) {
		t.Error("a dmabuf format lost its modifier")
	}
	if objectHas(formatPod(8, 8, 30, VideoBGRx, nil), formatVideoModifier) {
		t.Error("a plain format claims a modifier")
	}
	if !objectHas(formatPod(8, 8, 30, VideoBGRx, nil), formatVideoRate) {
		t.Error("the last property was not found")
	}
	if objectHas([]byte{1, 2, 3}, formatVideoModifier) || objectHas(podInt(1).bytes(), formatVideoModifier) {
		t.Error("garbage or a non-object carries a modifier")
	}
}

// memDmabuf poses a memfd as a GPU buffer: PipeWire passes the fd
// through whatever its kind.
type memDmabuf struct {
	fd       int
	released *atomic.Int32
}

func (m memDmabuf) Fd() int        { return m.fd }
func (m memDmabuf) Offset() uint32 { return 0 }
func (m memDmabuf) Stride() uint32 { return 256 }
func (m memDmabuf) Release()       { _ = unix.Close(m.fd); m.released.Add(1) }

// gpuSource is a DmabufSource over memDmabufs, counting what it did.
type gpuSource struct {
	solid
	allocs, fills, released atomic.Int32
}

func (g *gpuSource) Modifier() uint64 { return 0 }

func (g *gpuSource) DmabufVideoFormat() VideoFormat { return VideoBGRx }

func (g *gpuSource) AllocDmabuf() (Dmabuf, error) {
	fd, _, err := memfd(256 * 32)
	if err != nil {
		return nil, err
	}
	g.allocs.Add(1)
	return memDmabuf{fd: fd, released: &g.released}, nil
}

func (g *gpuSource) FillDmabuf(Dmabuf) ([]Rect, bool) {
	g.fills.Add(1)
	return nil, true
}

// streamTo runs src into a consumer offering modifier (nil: the plain
// format) and returns the first non-empty buffer it received.
func streamTo(t *testing.T, src Source, modifier *uint64) (consumed, *Producer) {
	t.Helper()
	pwtest.Start(t)
	p, err := Start(Config{Width: 64, Height: 32, Stride: 256, Format: VideoBGRx, FPS: 30, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	l, _ := load()
	loop, err := startLoop(l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(loop.stop)
	c := newConsumerOf(t, loop, 64, 32, modifier)
	t.Cleanup(func() {
		loop.do(func() {
			l.streamDisconnect(c.stream)
			l.streamDestroy(c.stream)
		})
	})
	link(t)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-c.frames:
			if f.size > 0 {
				return f, p
			}
		case <-deadline:
			t.Fatal("no frame arrived")
			return consumed{}, nil
		}
	}
}

func TestADmabufSourceStreamsGPUBuffersToADmabufConsumer(t *testing.T) {
	src := &gpuSource{solid: solid{0x11}}
	mod := uint64(0)
	f, p := streamTo(t, src, &mod)
	if f.dataType != dataDmaBuf || f.fd < 0 || f.size != 256*32 {
		t.Errorf("frame = %+v, want a %d-byte DmaBuf with its fd", f, 256*32)
	}
	if src.allocs.Load() == 0 || src.fills.Load() == 0 {
		t.Errorf("allocs %d fills %d, want the buffers bound and filled in place", src.allocs.Load(), src.fills.Load())
	}
	p.Close()
	if r, a := src.released.Load(), src.allocs.Load(); r != a {
		t.Errorf("released %d of %d GPU buffers on close", r, a)
	}
}

func TestADmabufSourceFallsBackToSharedMemory(t *testing.T) {
	src := &gpuSource{solid: solid{0x22}}
	f, _ := streamTo(t, src, nil)
	if f.dataType != dataMemFd || f.first != 0x22 || f.size != 256*32 {
		t.Errorf("frame = %+v, want producer-allocated memfd filled by Fill", f)
	}
	if src.allocs.Load() != 0 || src.fills.Load() != 0 {
		t.Errorf("a plain consumer got GPU buffers (allocs %d fills %d)", src.allocs.Load(), src.fills.Load())
	}
}
