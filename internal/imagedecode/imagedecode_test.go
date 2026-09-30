package imagedecode

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func rgba(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// sample is a 2x1 image: red then half-transparent blue.
func sample() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 255})
	return img
}

func TestOpenEncodedFormats(t *testing.T) {
	enc := map[string]func(*bytes.Buffer, image.Image) error{
		"a.png":  func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) },
		"a.bmp":  func(b *bytes.Buffer, m image.Image) error { return bmp.Encode(b, m) },
		"a.tiff": func(b *bytes.Buffer, m image.Image) error { return tiff.Encode(b, m, nil) },
	}
	for name, fn := range enc {
		var buf bytes.Buffer
		if err := fn(&buf, sample()); err != nil {
			t.Fatal(err)
		}
		img, err := Open(write(t, name, buf.Bytes()))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := rgba(img, 0, 0); got != (color.NRGBA{R: 255, A: 255}) {
			t.Errorf("%s pixel 0 = %v", name, got)
		}
		if got := rgba(img, 1, 0); got != (color.NRGBA{B: 255, A: 255}) {
			t.Errorf("%s pixel 1 = %v", name, got)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sample(), nil); err != nil {
		t.Fatal(err)
	}
	if img, err := Open(write(t, "a.jpg", buf.Bytes())); err != nil || img.Bounds().Dx() != 2 {
		t.Errorf("jpeg = %v %v", img, err)
	}
}

func TestOpenFarbfeld(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("farbfeld")
	_ = binary.Write(&buf, binary.BigEndian, [2]uint32{2, 1})
	_ = binary.Write(&buf, binary.BigEndian, [8]uint16{0xffff, 0, 0, 0xffff, 0, 0, 0xffff, 0x8080})
	img, err := Open(write(t, "a.ff", buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := rgba(img, 1, 0); got != (color.NRGBA{B: 255, A: 0x80}) {
		t.Errorf("farbfeld pixel = %v", got)
	}
	// A truncated body is an error, not a half image.
	if _, err := Open(write(t, "b.ff", buf.Bytes()[:20])); err == nil {
		t.Error("truncated farbfeld decoded")
	}
}

func TestOpenPNM(t *testing.T) {
	cases := map[string][]byte{
		"ascii-pixmap":  []byte("P3\n# comment\n2 1\n255\n255 0 0  0 0 255\n"),
		"binary-pixmap": append([]byte("P6 2 1 255\n"), 255, 0, 0, 0, 0, 255),
	}
	for name, data := range cases {
		img, err := Open(write(t, name+".pnm", data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rgba(img, 0, 0) != (color.NRGBA{R: 255, A: 255}) || rgba(img, 1, 0) != (color.NRGBA{B: 255, A: 255}) {
			t.Errorf("%s pixels = %v %v", name, rgba(img, 0, 0), rgba(img, 1, 0))
		}
	}
	gray, err := Open(write(t, "g.pgm", append([]byte("P5 2 1 15\n"), 15, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if rgba(gray, 0, 0) != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) || rgba(gray, 1, 0).R != 0 {
		t.Errorf("maxval scaling = %v %v", rgba(gray, 0, 0), rgba(gray, 1, 0))
	}
	bitmap, err := Open(write(t, "b.pbm", append([]byte("P4 2 1\n"), 0x80)))
	if err != nil {
		t.Fatal(err)
	}
	if rgba(bitmap, 0, 0).R != 0 || rgba(bitmap, 1, 0).R != 255 {
		t.Errorf("bitmap: 1 is black = %v %v", rgba(bitmap, 0, 0), rgba(bitmap, 1, 0))
	}
	if _, err := Open(write(t, "bad.ppm", []byte("P6 0 1 255\n"))); err == nil {
		t.Error("a zero-width PNM decoded")
	}
}

func tgaHeader(imgType, bpp, desc byte, w, h uint16) []byte {
	hdr := make([]byte, 18)
	hdr[2] = imgType
	binary.LittleEndian.PutUint16(hdr[12:], w)
	binary.LittleEndian.PutUint16(hdr[14:], h)
	hdr[16], hdr[17] = bpp, desc
	return hdr
}

func TestOpenTGA(t *testing.T) {
	// Uncompressed, bottom-up 2x2: rows stored bottom first.
	raw := append(tgaHeader(2, 24, 0, 2, 2),
		0, 0, 255, 0, 255, 0, // bottom row: red, green (BGR)
		255, 0, 0, 255, 255, 255) // top row: blue, white
	img, err := Open(write(t, "a.tga", raw))
	if err != nil {
		t.Fatal(err)
	}
	if rgba(img, 0, 0) != (color.NRGBA{B: 255, A: 255}) || rgba(img, 0, 1) != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("orientation: top-left %v bottom-left %v", rgba(img, 0, 0), rgba(img, 0, 1))
	}
	// RLE, top-down: a run of 3 red plus one raw green.
	rle := append(tgaHeader(10, 24, 0x20, 2, 2), 0x82, 0, 0, 255, 0x00, 0, 255, 0)
	img, err = Open(write(t, "b.tga", rle))
	if err != nil {
		t.Fatal(err)
	}
	if rgba(img, 1, 0) != (color.NRGBA{R: 255, A: 255}) || rgba(img, 1, 1) != (color.NRGBA{G: 255, A: 255}) {
		t.Errorf("rle = %v %v", rgba(img, 1, 0), rgba(img, 1, 1))
	}
	if _, err := Open(write(t, "c.tga", tgaHeader(1, 8, 0, 2, 2))); err == nil {
		t.Error("color-mapped TGA decoded")
	}
}

func TestOpenRejectsGarbageAndMissing(t *testing.T) {
	if _, err := Open(write(t, "x.png", []byte("definitely not an image"))); err == nil {
		t.Error("garbage decoded")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("a missing file decoded")
	}
}
