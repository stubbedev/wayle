// Package gbm allocates GPU buffers for zero-copy screencast frames
// (wayle-share-preview dmabuf.rs): a gbm device on a DRM render node,
// buffer objects with a modifier the consumer takes, and their planes
// as dmabuf fds. libgbm is bound through purego, so the binary stays
// CGO_ENABLED=0. Everything here is best effort: any failure is an
// error the caller answers by staying on shared memory.
package gbm

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"

	"github.com/stubbedev/wayle/internal/clib"
)

// The DRM modifiers every allocator understands: implicit (the
// driver's choice) and linear.
const (
	ModInvalid uint64 = 0x00ffffffffffffff
	ModLinear  uint64 = 0
)

// useRendering is GBM_BO_USE_RENDERING.
const useRendering = 1 << 2

var libCandidates = clib.SystemCandidates("libgbm.so.1")

type libgbm struct {
	createDevice   func(fd int32) uintptr
	deviceDestroy  func(dev uintptr)
	boCreate       func(dev uintptr, w, h, format, flags uint32) uintptr
	boCreateMods2  func(dev uintptr, w, h, format uint32, mods *uint64, count uint32, flags uint32) uintptr
	boDestroy      func(bo uintptr)
	boModifier     func(bo uintptr) uint64
	boPlaneCount   func(bo uintptr) int32
	boFdForPlane   func(bo uintptr, plane int32) int32
	boOffset       func(bo uintptr, plane int32) uint32
	boStrideOfPlan func(bo uintptr, plane int32) uint32
}

var (
	libOnce sync.Once
	lib     *libgbm
	errLib  error
)

func load() (*libgbm, error) {
	libOnce.Do(func() {
		h, err := clib.Open(libCandidates)
		if err != nil {
			errLib = fmt.Errorf("load libgbm: %w", err)
			return
		}
		l := &libgbm{}
		for _, f := range []struct {
			fn   any
			name string
		}{
			{&l.createDevice, "gbm_create_device"},
			{&l.deviceDestroy, "gbm_device_destroy"},
			{&l.boCreate, "gbm_bo_create"},
			{&l.boCreateMods2, "gbm_bo_create_with_modifiers2"},
			{&l.boDestroy, "gbm_bo_destroy"},
			{&l.boModifier, "gbm_bo_get_modifier"},
			{&l.boPlaneCount, "gbm_bo_get_plane_count"},
			{&l.boFdForPlane, "gbm_bo_get_fd_for_plane"},
			{&l.boOffset, "gbm_bo_get_offset"},
			{&l.boStrideOfPlan, "gbm_bo_get_stride_for_plane"},
		} {
			sym, err := purego.Dlsym(h, f.name)
			if err != nil {
				errLib = fmt.Errorf("libgbm: %s: %w", f.name, err)
				return
			}
			purego.RegisterFunc(f.fn, sym)
		}
		lib = l
	})
	return lib, errLib
}

// RenderNodes are the DRM render nodes tried in order: unprivileged,
// and the right device for offscreen allocation.
var RenderNodes = []string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/dri/renderD130"}

// Device is a gbm device on an open render node.
type Device struct {
	lib  *libgbm
	node *os.File
	dev  uintptr
	mu   sync.Mutex
}

// Open opens a gbm device on the first render node that takes one.
func Open() (*Device, error) {
	l, err := load()
	if err != nil {
		return nil, err
	}
	last := errors.New("gbm: no DRM render node")
	for _, node := range RenderNodes {
		f, err := os.OpenFile(node, os.O_RDWR|syscall.O_CLOEXEC, 0) //nolint:gosec // a fixed DRM render node path
		if err != nil {
			last = fmt.Errorf("gbm: open %s: %w", node, err)
			continue
		}
		dev := l.createDevice(int32(f.Fd()))
		if dev == 0 {
			_ = f.Close()
			last = fmt.Errorf("gbm: gbm_create_device %s failed", node)
			continue
		}
		return &Device{lib: l, node: f, dev: dev}, nil
	}
	return nil, last
}

// Close destroys the device; its buffers must be released first.
func (d *Device) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dev != 0 {
		d.lib.deviceDestroy(d.dev)
		d.dev = 0
		_ = d.node.Close()
	}
}

// Plane is one plane of a buffer as a dmabuf.
type Plane struct {
	Fd             int
	Offset, Stride uint32
}

// Buffer is one allocated buffer object and its exported planes; the
// plane fds stay open until Release.
type Buffer struct {
	dev           *Device
	bo            uintptr
	Width, Height uint32
	Fourcc        uint32
	Modifier      uint64
	Planes        []Plane
}

// ModifierCandidates is the order modifiers are tried in: the
// consumer's, then implicit and linear, which every driver takes.
func ModifierCandidates(advertised []uint64) []uint64 {
	var out []uint64
	for _, m := range append(slices.Clone(advertised), ModInvalid, ModLinear) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

// Alloc allocates a width x height buffer in the DRM fourcc, with the
// first candidate modifier the driver takes, else the driver's own
// choice.
func (d *Device) Alloc(fourcc, width, height uint32, modifiers []uint64) (*Buffer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dev == 0 {
		return nil, errors.New("gbm: device closed")
	}
	var bo uintptr
	for _, m := range ModifierCandidates(modifiers) {
		mod := m
		if bo = d.lib.boCreateMods2(d.dev, width, height, fourcc, &mod, 1, useRendering); bo != 0 {
			break
		}
	}
	if bo == 0 {
		bo = d.lib.boCreate(d.dev, width, height, fourcc, useRendering)
	}
	if bo == 0 {
		return nil, fmt.Errorf("gbm: cannot allocate %dx%d of fourcc %#x", width, height, fourcc)
	}
	b := &Buffer{dev: d, bo: bo, Width: width, Height: height, Fourcc: fourcc, Modifier: d.lib.boModifier(bo)}
	for plane := range d.lib.boPlaneCount(bo) {
		fd := d.lib.boFdForPlane(bo, plane)
		if fd < 0 {
			b.release()
			return nil, fmt.Errorf("gbm: no dmabuf fd for plane %d", plane)
		}
		b.Planes = append(b.Planes, Plane{Fd: int(fd), Offset: d.lib.boOffset(bo, plane), Stride: d.lib.boStrideOfPlan(bo, plane)})
	}
	if len(b.Planes) == 0 {
		b.release()
		return nil, errors.New("gbm: buffer has no planes")
	}
	return b, nil
}

// Release closes the plane fds and frees the buffer object.
func (b *Buffer) Release() {
	if b.dev == nil {
		b.release()
		return
	}
	b.dev.mu.Lock()
	defer b.dev.mu.Unlock()
	b.release()
}

func (b *Buffer) release() {
	for _, p := range b.Planes {
		_ = syscall.Close(p.Fd)
	}
	b.Planes = nil
	if b.bo != 0 && b.dev != nil && b.dev.dev != 0 {
		b.dev.lib.boDestroy(b.bo)
	}
	b.bo = 0
}

// Fourcc builds a DRM fourcc code.
func Fourcc(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

// The packed formats screencopy hands out.
var (
	FormatXRGB8888 = Fourcc('X', 'R', '2', '4')
	FormatARGB8888 = Fourcc('A', 'R', '2', '4')
	FormatXBGR8888 = Fourcc('X', 'B', '2', '4')
	FormatABGR8888 = Fourcc('A', 'B', '2', '4')
)
