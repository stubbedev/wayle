package credential

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // background decoders
	_ "image/jpeg" // background decoders
	_ "image/png"  // background decoders
	"math"
	"os"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	_ "golang.org/x/image/bmp"  // background decoders
	_ "golang.org/x/image/tiff" // background decoders
	_ "golang.org/x/image/webp" // background decoders

	"github.com/stubbedev/wayle/config"
)

// imageScrim is .lock-bg-scrim over image backgrounds:
// rgba(0, 0, 0, 0.45), for legibility.
var imageScrim = render.RGBA(0, 0, 0, 115)

// HexFill converts a config color to a premultiplied fill.
func HexFill(c config.HexColor) render.Color {
	r, g, b, a := c.RGBA()
	return render.RGBA(r, g, b, a)
}

// Background builds the surface background: the decoded image scaled
// to cover with the legibility scrim, or the solid color when there is
// no image.
func Background(img image.Image, color render.Color) widget.Widget {
	if img == nil {
		return NewFill(color)
	}
	pic := widget.NewImage(img)
	pic.SetScale(widget.ImageCover)
	return widget.NewOverlay().Append(pic).Append(NewFill(imageScrim))
}

// LoadImage decodes a background file and applies a gaussian blur of
// the given radius (0 is none) - the lock screen's load_texture. The
// decoders cover PNG, JPEG, GIF, WebP, BMP, and TIFF.
func LoadImage(path string, blur uint32) (image.Image, error) {
	f, err := os.Open(path) //nolint:gosec // the user's configured background
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if blur == 0 {
		return img, nil
	}
	return Blur(img, float64(blur)), nil
}

// Blur approximates a gaussian of standard deviation sigma (the image
// crate's blur) with three box passes per axis, the classic
// linear-time approximation.
func Blur(src image.Image, sigma float64) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	if sigma <= 0 || out.Rect.Empty() {
		return out
	}
	tmp := make([]uint8, len(out.Pix))
	for _, r := range boxRadii(sigma, 3) {
		boxPass(out.Pix, tmp, out.Rect.Dx(), out.Rect.Dy(), out.Stride, r, true)
		boxPass(tmp, out.Pix, out.Rect.Dx(), out.Rect.Dy(), out.Stride, r, false)
	}
	return out
}

// boxRadii picks n box radii whose successive application matches a
// gaussian of sigma (Kovesi, "Fast Almost-Gaussian Filtering").
func boxRadii(sigma float64, n int) []int {
	ideal := math.Sqrt(12*sigma*sigma/float64(n) + 1)
	lower := int(math.Floor(ideal))
	if lower%2 == 0 {
		lower--
	}
	upper := lower + 2
	m := int(math.Round((12*sigma*sigma - float64(n*lower*lower) - 4*float64(n*lower) - 3*float64(n)) / (-4*float64(lower) - 4)))
	radii := make([]int, n)
	for i := range radii {
		width := upper
		if i < m {
			width = lower
		}
		radii[i] = (width - 1) / 2
	}
	return radii
}

// boxPass blurs src into dst with a box of the given radius along rows
// (horizontal) or columns, clamping at the edges.
func boxPass(src, dst []uint8, w, h, stride, radius int, horizontal bool) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	lines, length := h, w
	if !horizontal {
		lines, length = w, h
	}
	at := func(line, i int) int {
		if horizontal {
			return line*stride + i*4
		}
		return i*stride + line*4
	}
	span := 2*radius + 1
	for line := range lines {
		for ch := range 4 {
			sum := 0
			for i := -radius; i <= radius; i++ {
				sum += int(src[at(line, min(max(i, 0), length-1))+ch])
			}
			for i := range length {
				dst[at(line, i)+ch] = uint8(sum / span)
				out := min(max(i-radius, 0), length-1)
				in := min(max(i+radius+1, 0), length-1)
				sum += int(src[at(line, in)+ch]) - int(src[at(line, out)+ch])
			}
		}
	}
}
