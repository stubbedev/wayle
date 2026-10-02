package filechooser

import (
	"errors"
	"image"
	"os"

	"golang.org/x/image/draw"

	"github.com/stubbedev/wayle/internal/imagedecode"
)

// thumbMaxBytes skips images too large to decode for a row icon.
const thumbMaxBytes = 32 << 20

// errThumbTooLarge is a source past thumbMaxBytes.
var errThumbTooLarge = errors.New("filechooser: image too large to thumbnail")

// thumbnail decodes an image into an exact px square, centre-cropped
// to fill: every thumbnail is the same size, so rows align whatever
// the source's aspect ratio, and a folder of photos holds icons, not
// full frames. Run it off the loop.
func thumbnail(path string, px int) (*image.RGBA, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > thumbMaxBytes {
		return nil, errThumbTooLarge
	}
	src, err := imagedecode.Open(path)
	if err != nil {
		return nil, err
	}
	px = max(1, px)
	b := src.Bounds()
	// The largest centred crop with the target's (square) aspect.
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, side, side).Add(b.Min).Add(image.Pt((b.Dx()-side)/2, (b.Dy()-side)/2))
	dst := image.NewRGBA(image.Rect(0, 0, px, px))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	return dst, nil
}
