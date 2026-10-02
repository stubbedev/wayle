package pipewire

import (
	"os/exec"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/stubbedev/wayle/internal/pipewire/pwtest"
)

// solid fills every frame with one byte.
type solid struct{ b byte }

func (s solid) Fill(dst []byte) ([]Rect, bool) {
	for i := range dst {
		dst[i] = s.b
	}
	return nil, true
}

func TestProducerExportsANode(t *testing.T) {
	pwtest.Start(t)
	p, err := Start(Config{Width: 64, Height: 32, Stride: 256, Format: VideoBGRx, FPS: 30, Source: solid{0x7f}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if id := p.NodeID(); id == 0 || id == idAny {
		t.Errorf("node id = %d", id)
	}
}

// consumer is a test-only input stream: it negotiates the producer's
// format and records what each buffer carried.
type consumer struct {
	stream uintptr
	frames chan consumed
}

// consumed is one received buffer.
type consumed struct {
	size      uint32
	first     byte
	seq       uint64
	transform uint32
	damage    []cMetaRegion
}

var (
	consumerOnce    sync.Once
	consumerProcess uintptr
	consumerParam   uintptr
	consumers       sync.Map // handle -> *consumer
)

const consumerHandle = 1 << 30

func newConsumer(t *testing.T, loop *loopThread, width, height uint32) *consumer {
	t.Helper()
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	consumerOnce.Do(func() {
		consumerProcess = purego.NewCallback(func(data uintptr) {
			v, _ := consumers.Load(data)
			c := v.(*consumer)
			raw := l.streamDequeue(c.stream)
			if raw == nil {
				return
			}
			defer l.streamQueue(c.stream, raw)
			buf := (*cPwBuffer)(raw).buffer
			d := buf.datas
			got := consumed{size: d.chunk.size}
			if d.data != nil && d.chunk.size > 0 {
				got.first = *(*byte)(d.data)
			}
			for _, m := range unsafe.Slice(buf.metas, buf.nMetas) {
				switch m.typ {
				case metaHeader:
					got.seq = (*cMetaHeader)(m.data).seq
				case metaVideoTransform:
					got.transform = *(*uint32)(m.data)
				case metaVideoDamage:
					for _, r := range unsafe.Slice((*cMetaRegion)(m.data), m.size/metaRegionSize) {
						if r.width == 0 || r.height == 0 {
							break
						}
						got.damage = append(got.damage, r)
					}
				}
			}
			select {
			case c.frames <- got:
			default:
			}
		})
		consumerParam = purego.NewCallback(func(data uintptr, id uint32, param unsafe.Pointer) {
			v, _ := consumers.Load(data)
			c := v.(*consumer)
			if param == nil || id != paramFormat {
				return
			}
			// Ask for the metas the producer offers.
			withPods(bufferPods(0, 0)[1:], func(params unsafe.Pointer, n uint32) int32 {
				return l.streamUpdate(c.stream, params, n)
			})
		})
	})
	c := &consumer{frames: make(chan consumed, 16)}
	consumers.Store(uintptr(consumerHandle), c)
	events := &cStreamEvents{version: 2, paramChanged: consumerParam, process: consumerProcess}
	var pinner runtime.Pinner
	pinner.Pin(events)
	t.Cleanup(pinner.Unpin)
	var rc int32
	loop.do(func() {
		props := l.propertiesNew("media.type=Video media.category=Capture node.name=test-consumer")
		c.stream = l.streamNewSimple(loop.loop, "test-consumer", props, unsafe.Pointer(events), consumerHandle)
		rc = withPods([][]byte{formatPod(width, height, 30, VideoBGRx, nil)}, func(params unsafe.Pointer, n uint32) int32 {
			return l.streamConnect(c.stream, 0 /* input */, idAny, flagMapBuffers, params, n)
		})
	})
	if rc < 0 {
		t.Fatalf("consumer connect: %d", rc)
	}
	return c
}

// link joins the producer's output to the consumer's input with
// pw-link, retrying while the ports appear.
func link(t *testing.T) {
	t.Helper()
	bin, err := exec.LookPath("pw-link")
	if err != nil {
		t.Fatalf("pw-link not on PATH: %v", err)
	}
	var out []byte
	for range 100 {
		out, err = exec.Command(bin, "wayle-screencast:output_0", "test-consumer:input_0").CombinedOutput()
		if err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pw-link: %v: %s", err, out)
}

// damaged fills the frame and reports two changed rectangles, one of
// them past the frame's edge.
type damaged struct{}

func (damaged) Fill(dst []byte) ([]Rect, bool) {
	for i := range dst {
		dst[i] = 0x42
	}
	return []Rect{{1, 2, 3, 4}, {60, 30, 10, 10}}, true
}

func TestProducerFeedsALinkedConsumer(t *testing.T) {
	pwtest.Start(t)
	p, err := Start(Config{Width: 64, Height: 32, Stride: 256, Format: VideoBGRx, FPS: 30, Transform: 3, Source: damaged{}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	l, _ := load()
	loop, err := startLoop(l)
	if err != nil {
		t.Fatal(err)
	}
	defer loop.stop()
	c := newConsumer(t, loop, 64, 32)
	defer loop.do(func() {
		l.streamDisconnect(c.stream)
		l.streamDestroy(c.stream)
	})
	link(t)
	var frames []consumed
	deadline := time.After(5 * time.Second)
	for len(frames) < 3 {
		select {
		case f := <-c.frames:
			if f.size > 0 {
				frames = append(frames, f)
			}
		case <-deadline:
			t.Fatalf("frames received: %d", len(frames))
		}
	}
	f := frames[0]
	if f.size != 256*32 || f.first != 0x42 || f.transform != 3 {
		t.Errorf("frame = %+v", f)
	}
	if want := []cMetaRegion{{1, 2, 3, 4}, {60, 30, 4, 2}}; !slices.Equal(f.damage, want) {
		t.Errorf("damage = %v, want %v (clipped to the frame)", f.damage, want)
	}
	for i := 1; i < len(frames); i++ {
		if frames[i].seq <= frames[i-1].seq {
			t.Errorf("header seq %d after %d: not increasing", frames[i].seq, frames[i-1].seq)
		}
	}
}
