package wallpaper

import (
	"image"
	"math"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/wallpaper"
)

// target is the device-pixel box a wallpaper is prepared for and the
// output's device scale (device px per logical px).
type target struct {
	w, h  int
	scale float64
}

// prepare resamples img for its monitor off the loop, so the surface
// blits it 1:1 instead of rescaling a multi-megapixel source on the
// frame path. The result is shown with ImageScaleDown, which draws it
// at its natural size once it fits the box. The fits mirror the GTK
// ContentFit the Rust shell maps each FitMode to (content_fit): Fill is
// Cover, Fit is Contain, Stretch is Fill, and Center is ScaleDown with
// the natural size in logical pixels, as GTK measures a texture.
func prepare(img image.Image, fit wallpaper.FitMode, box target) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || box.w <= 0 || box.h <= 0 {
		return img
	}
	var (
		src image.Rectangle
		dst render.Rect
	)
	switch fit {
	case wallpaper.FitFit:
		src, dst = render.PlaceImage(sw, sh, box.w, box.h, render.ImageFit, render.ImageCentered)
	case wallpaper.FitStretch:
		src, dst = render.PlaceImage(sw, sh, box.w, box.h, render.ImageStretch, render.ImageCentered)
	case wallpaper.FitCenter:
		nw := int(math.Round(float64(sw) * box.scale))
		nh := int(math.Round(float64(sh) * box.scale))
		if nw <= box.w && nh <= box.h {
			src, dst = image.Rect(0, 0, sw, sh), render.Rect{W: nw, H: nh}
		} else {
			src, dst = render.PlaceImage(sw, sh, box.w, box.h, render.ImageFit, render.ImageCentered)
		}
	default: // FitFill
		src, dst = render.PlaceImage(sw, sh, box.w, box.h, render.ImageCover, render.ImageCentered)
	}
	src = src.Add(b.Min)
	if src == b && dst.W == sw && dst.H == sh {
		return img
	}
	return render.Resample(img, src, dst.W, dst.H)
}

// widgetScale is the gelm policy for an image that could not be
// prepared (the output size is unknown): the closest live equivalent.
func widgetScale(fit wallpaper.FitMode) widget.ImageScale {
	switch fit {
	case wallpaper.FitFit:
		return widget.ImageFit
	case wallpaper.FitStretch:
		return widget.ImageStretch
	case wallpaper.FitCenter:
		return widget.ImageScaleDown
	}
	return widget.ImageCover
}
