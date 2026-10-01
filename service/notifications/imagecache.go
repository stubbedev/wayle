package notifications

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/stubbedev/wayle/internal/xdg"
)

// cacheImage is image_cache.rs's cache_image_data: inline pixels become
// $XDG_CACHE_HOME/wayle/notifications/<content hash>.png, written once.
// Only 8-bit RGB and RGBA are supported; anything else, or a buffer too
// short for its geometry, caches nothing.
func cacheImage(img imageData) (string, bool) {
	dir, ok := xdg.CacheDir()
	if !ok {
		return "", false
	}
	return cacheImageIn(filepath.Join(dir, "notifications"), img)
}

func cacheImageIn(dir string, img imageData) (string, bool) {
	if img.bitsPerSample != 8 || img.channels != 3 && img.channels != 4 {
		log.Printf("notifications: unsupported image data (%d bits, %d channels)", img.bitsPerSample, img.channels)
		return "", false
	}
	rgba, ok := imageRGBA(img)
	if !ok {
		log.Printf("notifications: image data shorter than %dx%d", img.width, img.height)
		return "", false
	}
	h := fnv.New64a()
	_, _ = h.Write(img.data)
	path := filepath.Join(dir, fmt.Sprintf("%016x.png", h.Sum64()))
	if _, err := os.Stat(path); err == nil {
		return path, true
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("notifications: image cache: %v", err)
		return "", false
	}
	if err := writePNG(path, rgba); err != nil {
		log.Printf("notifications: image cache: %v", err)
		_ = os.Remove(path)
		return "", false
	}
	return path, true
}

// imageRGBA unpacks the rows, dropping any rowstride padding
// (strip_rowstride_padding).
func imageRGBA(img imageData) (*image.NRGBA, bool) {
	w, hgt, ch, stride := int(img.width), int(img.height), int(img.channels), int(img.rowstride)
	if w <= 0 || hgt <= 0 || stride < w*ch || len(img.data) < (hgt-1)*stride+w*ch {
		return nil, false
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, hgt))
	for y := range hgt {
		row := img.data[y*stride:]
		for x := range w {
			px := row[x*ch:]
			a := byte(0xff)
			if ch == 4 {
				a = px[3]
			}
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = px[0], px[1], px[2], a
		}
	}
	return out, true
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path) //nolint:gosec // wayle's own cache dir
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(w, img); err != nil {
		_ = f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
