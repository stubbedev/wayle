package pipewire

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/clib"
)

// Rect is a damaged region in buffer pixels.
type Rect struct{ X, Y, Width, Height int }

// Source fills one frame: Fill copies stride-byte rows into dst and
// reports what changed (nil or empty is the whole frame); ok false
// skips this cycle (nothing new, or the capture failed).
type Source interface {
	Fill(dst []byte) (damage []Rect, ok bool)
}

// DmabufSource is a Source that also fills GPU buffers: the producer
// offers its modifier's format first and, when the consumer takes it,
// binds one buffer from AllocDmabuf to each PipeWire buffer for the
// stream's life and fills it in place (zero copy). A consumer that
// takes the plain format gets producer-allocated shared memory, filled
// through Fill.
type DmabufSource interface {
	Source
	Modifier() uint64
	// DmabufVideoFormat is the GPU buffers' format, which need not be
	// the shared-memory one (a GPU compositor hands out 24-bit shm and
	// 32-bit dmabufs).
	DmabufVideoFormat() VideoFormat
	AllocDmabuf() (Dmabuf, error)
	FillDmabuf(Dmabuf) (damage []Rect, ok bool)
}

// Dmabuf is one GPU buffer's single plane.
type Dmabuf interface {
	Fd() int
	Offset() uint32
	Stride() uint32
	Release()
}

// Config is one producer's fixed stream geometry.
type Config struct {
	// Name is the node name the consumer sees.
	Name                  string
	Width, Height, Stride uint32
	Format                VideoFormat
	// FPS is the advertised and paced frame rate.
	FPS uint32
	// Transform is the constant spa_meta_videotransform value (the
	// output's wl_output transform; 0 for a window).
	Transform uint32
	Source    Source
}

// Producer is one running PipeWire video output stream.
type Producer struct {
	lib    *libpw
	cfg    Config
	loop   *loopThread
	stream uintptr
	handle uintptr
	events *cStreamEvents
	pinner runtime.Pinner

	ready chan error
	once  sync.Once

	// dmabuf is the source's GPU side when it has one; useDmabuf is set
	// once the consumer negotiated the modifier format, bound holds each
	// PipeWire buffer's memory by its spa_data (loop only).
	dmabuf    DmabufSource
	useDmabuf bool
	bound     map[*cSpaData]binding

	start       time.Time
	minInterval time.Duration
	last        time.Time
	seq         uint64
	damage      []Rect
}

// producers routes the C event trampolines to their Producer by the
// data handle each stream was created with.
var (
	producersMu sync.Mutex
	producers   = map[uintptr]*Producer{}
	nextHandle  uintptr
)

func producerOf(handle uintptr) *Producer {
	producersMu.Lock()
	defer producersMu.Unlock()
	return producers[handle]
}

// connectTimeout bounds the wait for the server to export the node.
const connectTimeout = 5 * time.Second

// Start connects the stream and returns once the server exported its
// node (the stream reached PAUSED: before that the node id is
// SPA_ID_INVALID and no consumer could link to it).
func Start(cfg Config) (*Producer, error) {
	l, err := load()
	if err != nil {
		return nil, err
	}
	if cfg.FPS == 0 {
		cfg.FPS = 1
	}
	if cfg.Name == "" {
		cfg.Name = "wayle-screencast"
	}
	p := &Producer{
		lib: l, cfg: cfg, ready: make(chan error, 1), start: time.Now(),
		// A consumer that free-runs past the advertised rate would drive
		// a full-frame copy per cycle; captures are gated to 1/(1.2 fps).
		minInterval: time.Second * 10 / time.Duration(cfg.FPS*12),
	}
	p.last = p.start.Add(-p.minInterval)
	p.dmabuf, _ = cfg.Source.(DmabufSource)
	p.bound = map[*cSpaData]binding{}
	producersMu.Lock()
	nextHandle++
	p.handle = nextHandle
	producers[p.handle] = p
	producersMu.Unlock()

	p.loop, err = startLoop(l)
	if err != nil {
		p.forget()
		return nil, err
	}
	p.events = &cStreamEvents{version: 2, stateChanged: l.onState, paramChanged: l.onParam, process: l.onProcess}
	formats, flags := [][]byte{formatPod(cfg.Width, cfg.Height, cfg.FPS, cfg.Format, nil)}, uint32(flagMapBuffers)
	if p.dmabuf != nil {
		// The modifier's format first; the producer allocates every
		// buffer, a gbm one or shared memory per what was negotiated.
		mod := p.dmabuf.Modifier()
		formats = append([][]byte{formatPod(cfg.Width, cfg.Height, cfg.FPS, p.dmabuf.DmabufVideoFormat(), &mod)}, formats...)
		flags = flagAllocBuffers
		p.events.addBuffer, p.events.removeBuffer = l.onAddBuffer, l.onRemoveBuffer
	}
	p.pinner.Pin(p.events)
	var rc int32
	p.loop.do(func() {
		props := l.propertiesNew("media.type=Video media.category=Capture media.role=Screen node.name=" + cfg.Name)
		p.stream = l.streamNewSimple(p.loop.loop, cfg.Name, props, unsafe.Pointer(p.events), p.handle) //nolint:gosec // audited: the events table is pinned for the stream's life
		if p.stream == 0 {
			rc = -1
			return
		}
		rc = withPods(formats, func(params unsafe.Pointer, n uint32) int32 {
			return l.streamConnect(p.stream, directionOutput, idAny, flags, params, n)
		})
	})
	switch {
	case p.stream == 0:
		p.teardown()
		return nil, errors.New("pipewire: cannot create the stream (is the PipeWire daemon running?)")
	case rc < 0:
		p.teardown()
		return nil, fmt.Errorf("pipewire: connect the stream: %d", rc)
	}

	select {
	case err := <-p.ready:
		if err != nil {
			p.teardown()
			return nil, err
		}
	case <-time.After(connectTimeout):
		p.teardown()
		return nil, errors.New("pipewire: the server did not export the stream node")
	}
	return p, nil
}

// NodeID is the node the consumer connects to.
func (p *Producer) NodeID() uint32 {
	var id uint32
	p.loop.do(func() { id = p.lib.streamNodeID(p.stream) })
	return id
}

// Close stops the stream and its loop.
func (p *Producer) Close() { p.once.Do(p.teardown) }

func (p *Producer) teardown() {
	if p.loop != nil {
		if p.stream != 0 {
			p.loop.do(func() {
				p.lib.streamDisconnect(p.stream)
				p.lib.streamDestroy(p.stream)
				// What remove_buffer did not get to.
				for d, b := range p.bound {
					b.release()
					delete(p.bound, d)
				}
			})
			p.stream = 0
		}
		p.loop.stop()
		p.loop = nil
	}
	p.pinner.Unpin()
	p.forget()
}

func (p *Producer) forget() {
	producersMu.Lock()
	delete(producers, p.handle)
	producersMu.Unlock()
}

// withPods hands C an array of pointers to the pods, pinned for the
// call (PipeWire copies the params).
func withPods(pods [][]byte, call func(params unsafe.Pointer, n uint32) int32) int32 {
	var pinner runtime.Pinner
	defer pinner.Unpin()
	ptrs := make([]unsafe.Pointer, len(pods))
	for i, b := range pods {
		pinner.Pin(&b[0])
		ptrs[i] = unsafe.Pointer(&b[0]) //nolint:gosec // audited: pinned for the call
	}
	pinner.Pin(&ptrs[0])
	return call(unsafe.Pointer(&ptrs[0]), uint32(len(ptrs))) //nolint:gosec // audited: pinned for the call
}

// stateTrampoline is pw_stream_events.state_changed.
func stateTrampoline(data uintptr, _, state int32, msg *byte) {
	p := producerOf(data)
	if p == nil {
		return
	}
	switch state {
	case statePaused:
		select {
		case p.ready <- nil:
		default:
		}
	case stateError:
		select {
		case p.ready <- fmt.Errorf("pipewire: stream error: %s", clib.GoString(msg)):
		default:
		}
	}
}

// paramTrampoline is pw_stream_events.param_changed: once a format is
// negotiated, declare the buffer layout and metas.
func paramTrampoline(data uintptr, id uint32, param unsafe.Pointer) {
	p := producerOf(data)
	if p == nil || param == nil || id != paramFormat {
		return
	}
	pods := bufferPods(p.cfg.Stride, p.cfg.Height)
	if p.dmabuf != nil {
		// Producer-allocated: a dmabuf when the consumer took the
		// modifier's format, else a memfd.
		size := *(*uint32)(param)
		p.useDmabuf = objectHas(unsafe.Slice((*byte)(param), 8+size), formatVideoModifier) //nolint:gosec // audited: the pod is 8 + size bytes
		data := uint32(1 << dataMemFd)
		if p.useDmabuf {
			data = 1 << dataDmaBuf
		}
		pods = bufferPodsOf(p.cfg.Stride, p.cfg.Height, data)
	}
	withPods(pods, func(params unsafe.Pointer, n uint32) int32 {
		return p.lib.streamUpdate(p.stream, params, n)
	})
}

// processTrampoline is pw_stream_events.process: the consumer wants a
// buffer.
func processTrampoline(data uintptr) {
	if p := producerOf(data); p != nil {
		p.process()
	}
}

func (p *Producer) process() {
	now := time.Now()
	if now.Sub(p.last) < p.minInterval {
		return
	}
	raw := p.lib.streamDequeue(p.stream)
	if raw == nil {
		return
	}
	defer p.lib.streamQueue(p.stream, raw)
	buf := (*cPwBuffer)(raw).buffer
	if buf == nil || buf.nDatas == 0 {
		return
	}
	d := buf.datas
	if d.chunk == nil {
		return
	}
	var damage []Rect
	var ok bool
	if b, bound := p.bound[d]; bound && b.dmabuf != nil {
		damage, ok = p.dmabuf.FillDmabuf(b.dmabuf)
		d.chunk.offset = b.dmabuf.Offset()
		d.chunk.stride = int32(b.dmabuf.Stride())
		if ok {
			d.chunk.size = b.dmabuf.Stride() * p.cfg.Height
			p.last = now
			p.stamp(buf, now, damage)
		} else {
			d.chunk.size = 0
		}
		return
	}
	if d.data == nil {
		return
	}
	dst := unsafe.Slice((*byte)(d.data), d.maxsize) //nolint:gosec // audited: the mapped buffer is maxsize bytes
	damage, ok = p.cfg.Source.Fill(dst)
	d.chunk.offset = 0
	d.chunk.stride = int32(p.cfg.Stride)
	if !ok {
		// Queued empty: the consumer keeps its previous frame.
		d.chunk.size = 0
		return
	}
	p.last = now
	d.chunk.size = min(p.cfg.Stride*p.cfg.Height, d.maxsize)
	p.stamp(buf, now, damage)
}

// stamp writes the metas the consumer negotiated.
func (p *Producer) stamp(buf *cSpaBuffer, now time.Time, damage []Rect) {
	metas := unsafe.Slice(buf.metas, buf.nMetas) //nolint:gosec // audited: n_metas entries
	for i := range metas {
		m := &metas[i]
		if m.data == nil {
			continue
		}
		switch m.typ {
		case metaHeader:
			h := (*cMetaHeader)(m.data)
			*h = cMetaHeader{pts: now.Sub(p.start).Nanoseconds(), seq: p.seq}
			p.seq++
		case metaVideoTransform:
			*(*uint32)(m.data) = p.cfg.Transform
		case metaVideoDamage:
			p.damage = ClampDamage(p.damage[:0], damage, int(p.cfg.Width), int(p.cfg.Height), maxDamageRegions)
			writeDamage(unsafe.Slice((*cMetaRegion)(m.data), m.size/metaRegionSize), p.damage) //nolint:gosec // audited: the meta holds size bytes of regions
		}
	}
}

// ClampDamage clips rects to the frame into out; none, none left, or
// more than max collapse to the whole frame.
func ClampDamage(out, rects []Rect, width, height, max int) []Rect {
	out = out[:0]
	if len(rects) == 0 || max == 0 {
		return append(out, Rect{0, 0, width, height})
	}
	for _, r := range rects {
		if r.X < 0 || r.Y < 0 || r.X >= width || r.Y >= height {
			continue
		}
		w, h := min(r.Width, width-r.X), min(r.Height, height-r.Y)
		if w <= 0 || h <= 0 {
			continue
		}
		out = append(out, Rect{r.X, r.Y, w, h})
	}
	if len(out) == 0 || len(out) > max {
		return append(out[:0], Rect{0, 0, width, height})
	}
	return out
}

// writeDamage fills the meta's region slots and terminates the list
// with a zero-size region, leaving room for it.
func writeDamage(slots []cMetaRegion, rects []Rect) {
	if len(slots) == 0 {
		return
	}
	i := 0
	for ; i < len(rects) && i+1 < len(slots); i++ {
		r := rects[i]
		slots[i] = cMetaRegion{int32(r.X), int32(r.Y), uint32(r.Width), uint32(r.Height)}
	}
	slots[i] = cMetaRegion{}
}

// binding is one producer-allocated PipeWire buffer's memory: a GPU
// buffer, or a mapped memfd.
type binding struct {
	dmabuf Dmabuf
	mem    []byte
	fd     int
}

// addBufferTrampoline is pw_stream_events.add_buffer: allocate the new
// buffer's memory (only with ALLOC_BUFFERS, a DmabufSource's stream).
func addBufferTrampoline(data uintptr, raw unsafe.Pointer) {
	if p := producerOf(data); p != nil {
		p.addBuffer((*cPwBuffer)(raw))
	}
}

// removeBufferTrampoline is pw_stream_events.remove_buffer.
func removeBufferTrampoline(data uintptr, raw unsafe.Pointer) {
	if p := producerOf(data); p != nil {
		p.removeBuffer((*cPwBuffer)(raw))
	}
}

func (p *Producer) addBuffer(pwb *cPwBuffer) {
	if pwb == nil || pwb.buffer == nil || pwb.buffer.nDatas < 1 {
		return
	}
	d := pwb.buffer.datas
	size := p.cfg.Stride * p.cfg.Height
	if p.useDmabuf {
		b, err := p.dmabuf.AllocDmabuf()
		if err != nil {
			return // the buffer stays empty; process skips it
		}
		d.typ, d.flags = dataDmaBuf, dataFlagReadable
		d.fd, d.mapoffset, d.maxsize, d.data = int64(b.Fd()), 0, b.Stride()*p.cfg.Height, nil
		if d.chunk != nil {
			d.chunk.offset, d.chunk.stride, d.chunk.size = b.Offset(), int32(b.Stride()), 0
		}
		p.bound[d] = binding{dmabuf: b, fd: -1}
		return
	}
	fd, mem, err := memfd(int(size))
	if err != nil {
		return
	}
	d.typ, d.flags = dataMemFd, dataFlagReadable|dataFlagMappable
	d.fd, d.mapoffset, d.maxsize, d.data = int64(fd), 0, size, unsafe.Pointer(&mem[0]) //nolint:gosec // audited: the memfd mapping outlives the buffer (removeBuffer clears it)
	p.bound[d] = binding{mem: mem, fd: fd}
}

func (p *Producer) removeBuffer(pwb *cPwBuffer) {
	if pwb == nil || pwb.buffer == nil || pwb.buffer.nDatas < 1 {
		return
	}
	d := pwb.buffer.datas
	if b, ok := p.bound[d]; ok {
		b.release()
		delete(p.bound, d)
		d.data, d.fd = nil, -1
	}
}

func (b binding) release() {
	if b.dmabuf != nil {
		b.dmabuf.Release()
		return
	}
	_ = unix.Munmap(b.mem)
	_ = unix.Close(b.fd)
}

// memfd is a size-byte sealed-size shared memory file, mapped.
func memfd(size int) (int, []byte, error) {
	fd, err := unix.MemfdCreate("wayle-screencast", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return -1, nil, err
	}
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		_ = unix.Close(fd)
		return -1, nil, err
	}
	mem, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return -1, nil, err
	}
	return fd, mem, nil
}
