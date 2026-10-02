package gbm

import (
	"slices"
	"syscall"
	"testing"
)

func TestModifierCandidates(t *testing.T) {
	for _, c := range []struct{ in, want []uint64 }{
		{nil, []uint64{ModInvalid, ModLinear}},
		{[]uint64{7}, []uint64{7, ModInvalid, ModLinear}},
		{[]uint64{ModLinear, 7, 7}, []uint64{ModLinear, 7, ModInvalid}},
	} {
		if got := ModifierCandidates(c.in); !slices.Equal(got, c.want) {
			t.Errorf("ModifierCandidates(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFourcc(t *testing.T) {
	if FormatXRGB8888 != 0x34325258 || FormatARGB8888 != 0x34325241 {
		t.Errorf("XR24 = %#x, AR24 = %#x", FormatXRGB8888, FormatARGB8888)
	}
}

// TestAllocExportsPlanes allocates on the machine's GPU when it has a
// render node gbm opens (skipped elsewhere: CI, containers).
func TestAllocExportsPlanes(t *testing.T) {
	dev, err := Open()
	if err != nil {
		t.Skipf("no gbm device here: %v", err)
	}
	defer dev.Close()
	b, err := dev.Alloc(FormatXRGB8888, 64, 32, nil)
	if err != nil {
		t.Skipf("the driver allocates no XR24 buffer: %v", err)
	}
	if len(b.Planes) == 0 || b.Planes[0].Stride < 64*4 || b.Width != 64 || b.Fourcc != FormatXRGB8888 {
		t.Errorf("buffer = %+v", b)
	}
	fd := b.Planes[0].Fd
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		t.Fatalf("plane fd %d is not open: %v", fd, err)
	}
	b.Release()
	if err := syscall.Fstat(fd, &st); err == nil {
		t.Error("Release left the plane fd open")
	}
	b.Release() // twice is a no-op
	dev.Close()
	if _, err := dev.Alloc(FormatXRGB8888, 8, 8, nil); err == nil {
		t.Error("a closed device allocated")
	}
}
