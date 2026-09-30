// Package imagedecode opens raster images in the formats the Rust
// shell's `image` crate decodes by default for wallpapers: PNG, JPEG,
// GIF, BMP, TIFF, WebP (stdlib and golang.org/x/image), plus farbfeld,
// PNM (P1-P6), and TGA, which Go has no decoder for and this package
// registers itself. SVG, JPEG XL, and AVIF are listed as wallpaper
// extensions but the Rust decoder rejects them as well.
package imagedecode

import (
	"bufio"
	"fmt"
	"image"
	_ "image/gif"  // GIF decoder
	_ "image/jpeg" // JPEG decoder
	_ "image/png"  // PNG decoder
	"os"

	_ "golang.org/x/image/bmp"  // BMP decoder
	_ "golang.org/x/image/tiff" // TIFF decoder
	_ "golang.org/x/image/webp" // WebP decoder
)

func init() {
	image.RegisterFormat("farbfeld", "farbfeld", decodeFarbfeld, decodeFarbfeldConfig)
	for _, magic := range []string{"P1", "P2", "P3", "P4", "P5", "P6"} {
		image.RegisterFormat("pnm", magic, decodePNM, decodePNMConfig)
	}
}

// Open decodes the image file at path. The format is sniffed from the
// content, except TGA, which has no magic and is tried last.
func Open(path string) (image.Image, error) {
	f, err := os.Open(path) //nolint:gosec // the caller's image path
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	if err == nil {
		return img, nil
	}
	if _, seekErr := f.Seek(0, 0); seekErr != nil {
		return nil, err
	}
	if tga, tgaErr := decodeTGA(bufio.NewReader(f)); tgaErr == nil {
		return tga, nil
	}
	return nil, fmt.Errorf("%s: %w", path, err)
}
