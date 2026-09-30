package sharepreview

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/stubbedev/gelm/capture"
)

// marked is a 3x2 image whose pixels encode their position, so every
// transform's mapping reads off the result.
func marked() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	return img
}

// at reads the (x, y) marker a pixel carries.
func at(img *image.RGBA, x, y int) [2]uint8 {
	c := img.RGBAAt(x, y)
	return [2]uint8{c.R, c.G}
}

func TestTransform(t *testing.T) {
	cases := []struct {
		name string
		tr   capture.Transform
		w, h int
		// topLeft and topRight are the source positions landing in
		// the result's first row corners.
		topLeft, topRight [2]uint8
	}{
		{"normal", capture.TransformNormal, 3, 2, [2]uint8{0, 0}, [2]uint8{2, 0}},
		{"90 rotates clockwise", capture.Transform90, 2, 3, [2]uint8{0, 1}, [2]uint8{0, 0}},
		{"180", capture.Transform180, 3, 2, [2]uint8{2, 1}, [2]uint8{0, 1}},
		{"270 rotates counter-clockwise", capture.Transform270, 2, 3, [2]uint8{2, 0}, [2]uint8{2, 1}},
		{"flipped flips vertically", capture.TransformFlipped, 3, 2, [2]uint8{0, 1}, [2]uint8{2, 1}},
		{"flipped90", capture.TransformFlipped90, 2, 3, [2]uint8{0, 0}, [2]uint8{0, 1}},
		{"flipped180", capture.TransformFlipped180, 3, 2, [2]uint8{2, 0}, [2]uint8{0, 0}},
		{"flipped270", capture.TransformFlipped270, 2, 3, [2]uint8{2, 1}, [2]uint8{2, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Transform(marked(), tc.tr)
			if out.Bounds().Dx() != tc.w || out.Bounds().Dy() != tc.h {
				t.Fatalf("size %v, want %dx%d", out.Bounds(), tc.w, tc.h)
			}
			if got := at(out, 0, 0); got != tc.topLeft {
				t.Errorf("top-left from %v, want %v", got, tc.topLeft)
			}
			if got := at(out, tc.w-1, 0); got != tc.topRight {
				t.Errorf("top-right from %v, want %v", got, tc.topRight)
			}
		})
	}
}

func TestResizeToFit(t *testing.T) {
	cases := []struct {
		name       string
		w, h, size int
		wantW      int
		wantH      int
	}{
		{"landscape above the cap scales its height to it", 1920, 1080, 640, 1137, 640},
		{"portrait above the cap scales its width to it", 1080, 1920, 640, 640, 1137},
		{"under the cap is untouched", 800, 600, 640, 800, 600},
		{"a square never scales", 2000, 2000, 640, 2000, 2000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ResizeToFit(image.NewRGBA(image.Rect(0, 0, tc.w, tc.h)), tc.size)
			if out.Bounds().Dx() != tc.wantW || out.Bounds().Dy() != tc.wantH {
				t.Fatalf("resized to %v, want %dx%d", out.Bounds(), tc.wantW, tc.wantH)
			}
		})
	}
}

func TestFrameImageAppliesTheTransform(t *testing.T) {
	f := &capture.Frame{Width: 2, Height: 1, Stride: 8, Format: capture.FormatXRGB8888, Data: []byte{0, 0, 0xff, 0, 0xff, 0, 0, 0}}
	img, err := FrameImage(f, capture.Transform90)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 2 || img.RGBAAt(0, 0).R != 0xff {
		t.Fatalf("rotated frame %v top %v", img.Bounds(), img.RGBAAt(0, 0))
	}
	f.Format = capture.Format(0x36314752) // RGB565: not convertible
	if _, err := FrameImage(f, capture.TransformNormal); err == nil {
		t.Fatal("an unsupported format converted")
	}
}

func TestParseList(t *testing.T) {
	t.Run("entries with and without addresses", func(t *testing.T) {
		got := ParseList("12[HC>]foot[HT>]shell[HE>]94221[HA>]7[HC>]firefox[HT>]News[HE>]")
		want := []Toplevel{
			{ID: 12, Class: "foot", Title: "shell", WindowAddress: 94221, HasAddress: true},
			{ID: 7, Class: "firefox", Title: "News"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parsed %+v", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := ParseList(""); len(got) != 0 {
			t.Fatalf("parsed %+v", got)
		}
	})
	t.Run("a malformed entry stops the parse, keeping the good ones", func(t *testing.T) {
		got := ParseList("1[HC>]a[HT>]b[HE>]2[HA>]x[HC>]c[HT>]d[HE>]")
		if len(got) != 1 || got[0].ID != 1 {
			t.Fatalf("parsed %+v", got)
		}
		if got := ParseList("5[HC>]a[HT>]b[HE>]notanumber[HA>]"); len(got) != 0 {
			t.Fatalf("a bad address parsed: %+v", got)
		}
		if got := ParseList("5[HC>]only-class"); len(got) != 0 {
			t.Fatalf("a truncated entry parsed: %+v", got)
		}
	})
	t.Run("out-of-order separators do not panic", func(t *testing.T) {
		if got := ParseList("5[HT>]x[HC>]y[HE>]"); len(got) != 0 {
			t.Fatalf("parsed %+v", got)
		}
	})
}
