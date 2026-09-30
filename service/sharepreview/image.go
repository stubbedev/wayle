// Package sharepreview is the capture half of wayle-share-preview on
// top of gelm's capture package: turning captured frames into images
// (the output-transform correction, the preview downscale), the XDPH
// window list the share picker reads, and the best-effort gbm dmabuf
// allocator the screencast producer's zero-copy path imports.
package sharepreview

import (
	"image"

	"github.com/stubbedev/gelm/capture"
	xdraw "golang.org/x/image/draw"
)

// FrameImage converts a captured frame to an image and applies the
// output transform, the Image::new → into_rgb → transform chain of
// image.rs. Frames without alpha come out opaque.
func FrameImage(f *capture.Frame, t capture.Transform) (*image.RGBA, error) {
	img, err := f.Image()
	if err != nil {
		return nil, err
	}
	return Transform(img, t), nil
}

// Transform applies an output transform to a captured frame the way
// image.rs's Image::transform does: the 90/270 variants rotate
// clockwise (image's rotate90/rotate270), and the flipped variants
// flip vertically first.
func Transform(img *image.RGBA, t capture.Transform) *image.RGBA {
	switch t {
	case capture.Transform90:
		return rotate90(img)
	case capture.Transform180:
		return rotate180(img)
	case capture.Transform270:
		return rotate270(img)
	case capture.TransformFlipped:
		return flipVertical(img)
	case capture.TransformFlipped90:
		return rotate90(flipVertical(img))
	case capture.TransformFlipped180:
		return rotate180(flipVertical(img))
	case capture.TransformFlipped270:
		return rotate270(flipVertical(img))
	}
	return img
}

// remap builds a w x h image whose pixel (x, y) comes from src at
// at(x, y).
func remap(src *image.RGBA, w, h int, at func(x, y int) (int, int)) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	for y := range h {
		for x := range w {
			sx, sy := at(x, y)
			si := src.PixOffset(b.Min.X+sx, b.Min.Y+sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

// rotate90 turns the image 90 degrees clockwise.
func rotate90(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	return remap(src, h, w, func(x, y int) (int, int) { return y, h - 1 - x })
}

// rotate180 turns the image upside down.
func rotate180(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	return remap(src, w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y })
}

// rotate270 turns the image 270 degrees clockwise.
func rotate270(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	return remap(src, h, w, func(x, y int) (int, int) { return w - 1 - y, x })
}

// flipVertical mirrors the image top to bottom.
func flipVertical(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	return remap(src, w, h, func(x, y int) (int, int) { return x, h - 1 - y })
}

// Resize scales the image to w x h with a triangle (bilinear) filter,
// image's FilterType::Triangle.
func Resize(src *image.RGBA, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// ResizeToFit downscales the image so its smaller dimension is at most
// size, keeping the aspect ratio - image.rs's resize_to_fit, including
// its quirks: a portrait image scales when its width exceeds size, a
// landscape one when its height does, and a square image never scales.
func ResizeToFit(src *image.RGBA, size int) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	aspect := float64(w) / float64(h)
	switch {
	case h > w && w > size:
		return Resize(src, size, int(float64(size)/aspect))
	case w > h && h > size:
		return Resize(src, int(float64(size)*aspect), size)
	}
	return src
}
