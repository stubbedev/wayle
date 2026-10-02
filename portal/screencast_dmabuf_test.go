package portal

import (
	"errors"
	"slices"
	"syscall"
	"testing"

	"github.com/stubbedev/gelm/capture"
	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/gbm"
	"github.com/stubbedev/wayle/internal/pipewire"
)

// fakeGBM allocates memfds posing as buffer objects.
type fakeGBM struct {
	planes int
	fail   bool
	asked  [][]uint64
	fds    []int
}

func (f *fakeGBM) Alloc(fourcc, w, h uint32, mods []uint64) (*gbm.Buffer, error) {
	f.asked = append(f.asked, mods)
	if f.fail {
		return nil, errors.New("no memory")
	}
	b := &gbm.Buffer{Width: w, Height: h, Fourcc: fourcc, Modifier: 7}
	for range max(f.planes, 1) {
		fd, err := unix.MemfdCreate("fake-bo", unix.MFD_CLOEXEC)
		if err != nil {
			return nil, err
		}
		f.fds = append(f.fds, fd)
		b.Planes = append(b.Planes, gbm.Plane{Fd: fd, Stride: w * 4})
	}
	return b, nil
}

func (f *fakeGBM) Close() {}

func open(fd int) bool {
	var st syscall.Stat_t
	return syscall.Fstat(fd, &st) == nil
}

type fakeImport struct{ destroyed *int }

func (f fakeImport) Destroy() { *f.destroyed++ }

// fakeCopy imports and captures, or refuses either.
type fakeCopy struct {
	rejectImport, failCapture bool
	imported, destroyed       int
	got                       []capture.Dmabuf
}

func (f *fakeCopy) Import(d capture.Dmabuf) (importedBuffer, error) {
	f.got = append(f.got, d)
	if f.rejectImport {
		return nil, capture.ErrDmabufRejected
	}
	f.imported++
	return fakeImport{&f.destroyed}, nil
}

func (f *fakeCopy) CopyInto(capture.Output, bool, importedBuffer) error {
	if f.failCapture {
		return errors.New("copy failed")
	}
	return nil
}

var shmFrame = &capture.Frame{Width: 64, Height: 32, Stride: 256, Format: capture.FormatXRGB8888}

func offer() capture.DmabufFormat {
	return capture.DmabufFormat{Fourcc: gbm.FormatXRGB8888, Width: 64, Height: 32}
}

func TestProbeTakesTheModifierAndReleasesTheProbe(t *testing.T) {
	dev, cp := &fakeGBM{}, &fakeCopy{}
	src, err := probeGPUSource(nil, dev, cp, offer(), capture.Output{}, true, shmFrame)
	if err != nil {
		t.Fatal(err)
	}
	if src.Modifier() != 7 || len(dev.asked) != 1 || dev.asked[0] != nil {
		t.Errorf("modifier %d asked %v, want the driver's choice probed then offered", src.Modifier(), dev.asked)
	}
	if open(dev.fds[0]) || cp.destroyed != 1 {
		t.Errorf("the probe buffer leaked: fd open %v, imports destroyed %d", open(dev.fds[0]), cp.destroyed)
	}
	if d := cp.got[0]; d.Fourcc != gbm.FormatXRGB8888 || d.Width != 64 || d.Modifier != 7 || len(d.Planes) != 1 || d.Planes[0].Stride != 256 {
		t.Errorf("imported %+v", d)
	}
}

func TestProbeDeclines(t *testing.T) {
	for name, c := range map[string]struct {
		offered capture.DmabufFormat
		dev     *fakeGBM
		cp      *fakeCopy
	}{
		"a 24-bit dmabuf":   {capture.DmabufFormat{Fourcc: uint32(capture.FormatBGR888), Width: 64, Height: 32}, &fakeGBM{}, &fakeCopy{}},
		"another size":      {capture.DmabufFormat{Fourcc: gbm.FormatXRGB8888, Width: 32, Height: 32}, &fakeGBM{}, &fakeCopy{}},
		"no allocation":     {offer(), &fakeGBM{fail: true}, &fakeCopy{}},
		"two planes":        {offer(), &fakeGBM{planes: 2}, &fakeCopy{}},
		"a rejected import": {offer(), &fakeGBM{}, &fakeCopy{rejectImport: true}},
		"a failed copy":     {offer(), &fakeGBM{}, &fakeCopy{failCapture: true}},
	} {
		_, err := probeGPUSource(nil, c.dev, c.cp, c.offered, capture.Output{}, false, shmFrame)
		if !errors.Is(err, errNoDmabuf) {
			t.Errorf("%s: err = %v, want errNoDmabuf (stay on shm)", name, err)
		}
		for _, fd := range c.dev.fds {
			if open(fd) {
				t.Errorf("%s: a buffer leaked", name)
			}
		}
		if c.cp.imported != c.cp.destroyed {
			t.Errorf("%s: %d imports, %d destroyed", name, c.cp.imported, c.cp.destroyed)
		}
	}
}

type otherDmabuf struct{}

func (otherDmabuf) Fd() int        { return -1 }
func (otherDmabuf) Offset() uint32 { return 0 }
func (otherDmabuf) Stride() uint32 { return 0 }
func (otherDmabuf) Release()       {}

func TestGPUSourceAllocatesAtTheModifierAndFillsInPlace(t *testing.T) {
	dev, cp := &fakeGBM{}, &fakeCopy{}
	src, err := probeGPUSource(nil, dev, cp, offer(), capture.Output{}, false, shmFrame)
	if err != nil {
		t.Fatal(err)
	}
	var _ pipewire.DmabufSource = src
	d, err := src.AllocDmabuf()
	if err != nil {
		t.Fatal(err)
	}
	if got := dev.asked[len(dev.asked)-1]; !slices.Equal(got, []uint64{7}) {
		t.Errorf("allocated with %v, want the probed modifier", got)
	}
	if d.Stride() != 256 || d.Fd() < 0 {
		t.Errorf("buffer stride %d fd %d", d.Stride(), d.Fd())
	}
	damage, ok := src.FillDmabuf(d)
	if !ok || damage != nil {
		t.Errorf("fill = %v %v, want a whole frame", damage, ok)
	}
	if _, ok := src.FillDmabuf(otherDmabuf{}); ok {
		t.Error("a buffer the source did not allocate filled")
	}
	cp.failCapture = true
	if _, ok := src.FillDmabuf(d); ok {
		t.Error("a failed copy reported a frame")
	}
	fd := d.Fd()
	d.Release()
	if open(fd) || cp.destroyed != cp.imported {
		t.Errorf("release: fd open %v, %d of %d imports destroyed", open(fd), cp.destroyed, cp.imported)
	}
}

func TestScreencopyRejectsAForeignBuffer(t *testing.T) {
	if err := (screencopy{}).CopyInto(capture.Output{}, false, fakeImport{new(int)}); err == nil {
		t.Error("a buffer another client imported was captured into")
	}
}

// A GPU compositor's shm and dmabuf formats differ: the stream offers
// each its own.
func TestProbeTakesADmabufFormatOtherThanTheShmOne(t *testing.T) {
	shm24 := &capture.Frame{Width: 64, Height: 32, Stride: 192, Format: capture.FormatBGR888}
	offered := capture.DmabufFormat{Fourcc: gbm.FormatABGR8888, Width: 64, Height: 32}
	src, err := probeGPUSource(nil, &fakeGBM{}, &fakeCopy{}, offered, capture.Output{}, false, shm24)
	if err != nil {
		t.Fatal(err)
	}
	if src.DmabufVideoFormat() != pipewire.VideoRGBA {
		t.Errorf("dmabuf video format = %v, want RGBA (DRM AB24)", src.DmabufVideoFormat())
	}
}

func TestVideoFormatMapsThePackedLayouts(t *testing.T) {
	for f, want := range map[capture.Format]pipewire.VideoFormat{
		capture.FormatXRGB8888: pipewire.VideoBGRx,
		capture.FormatARGB8888: pipewire.VideoBGRA,
		capture.FormatXBGR8888: pipewire.VideoRGBx,
		capture.FormatABGR8888: pipewire.VideoRGBA,
		capture.FormatBGR888:   pipewire.VideoRGB,
		capture.FormatRGB888:   pipewire.VideoBGR,
	} {
		if got := videoFormat(f); got != want {
			t.Errorf("videoFormat(%v) = %v, want %v", f, got, want)
		}
	}
}
