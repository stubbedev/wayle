package imagedecode

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
)

// maxPixels bounds a decoded image (16k x 16k): a corrupt header must
// not allocate gigabytes.
const maxPixels = 1 << 28

func checkSize(w, h int) error {
	if w <= 0 || h <= 0 || w > maxPixels/h {
		return fmt.Errorf("imagedecode: invalid dimensions %dx%d", w, h)
	}
	return nil
}

// farbfeld: "farbfeld", be32 width, be32 height, then RGBA as be16.

func decodeFarbfeldConfig(r io.Reader) (image.Config, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return image.Config{}, err
	}
	if string(hdr[:8]) != "farbfeld" {
		return image.Config{}, errors.New("imagedecode: not farbfeld")
	}
	w, h := int(binary.BigEndian.Uint32(hdr[8:12])), int(binary.BigEndian.Uint32(hdr[12:16]))
	if err := checkSize(w, h); err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: color.NRGBA64Model, Width: w, Height: h}, nil
}

func decodeFarbfeld(r io.Reader) (image.Image, error) {
	cfg, err := decodeFarbfeldConfig(r)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA64(image.Rect(0, 0, cfg.Width, cfg.Height))
	if _, err := io.ReadFull(r, img.Pix); err != nil {
		return nil, fmt.Errorf("imagedecode: farbfeld pixels: %w", err)
	}
	// NRGBA64's Pix is big-endian 16-bit RGBA: farbfeld's exact layout.
	return img, nil
}

// PNM: P1/P4 bitmap, P2/P5 graymap, P3/P6 pixmap; ASCII or binary.

type pnmHeader struct {
	magic      byte // '1'..'6'
	w, h, maxv int
}

// pnmToken reads the next whitespace-separated header token, skipping
// '#' comments.
func pnmToken(r *bufio.Reader) (string, error) {
	var tok []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if len(tok) > 0 && errors.Is(err, io.EOF) {
				return string(tok), nil
			}
			return "", err
		}
		switch {
		case c == '#' && len(tok) == 0:
			if _, err := r.ReadString('\n'); err != nil {
				return "", err
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			if len(tok) > 0 {
				return string(tok), nil
			}
		default:
			tok = append(tok, c)
		}
	}
}

func pnmInt(r *bufio.Reader) (int, error) {
	tok, err := pnmToken(r)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(tok)
}

func readPNMHeader(r *bufio.Reader) (pnmHeader, error) {
	var magic [2]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return pnmHeader{}, err
	}
	if magic[0] != 'P' || magic[1] < '1' || magic[1] > '6' {
		return pnmHeader{}, errors.New("imagedecode: not PNM")
	}
	h := pnmHeader{magic: magic[1], maxv: 1}
	var err error
	if h.w, err = pnmInt(r); err != nil {
		return h, err
	}
	if h.h, err = pnmInt(r); err != nil {
		return h, err
	}
	if h.magic != '1' && h.magic != '4' {
		if h.maxv, err = pnmInt(r); err != nil {
			return h, err
		}
		if h.maxv <= 0 || h.maxv > 65535 {
			return h, fmt.Errorf("imagedecode: PNM maxval %d", h.maxv)
		}
	}
	return h, checkSize(h.w, h.h)
}

func decodePNMConfig(r io.Reader) (image.Config, error) {
	h, err := readPNMHeader(bufio.NewReader(r))
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: h.w, Height: h.h}, nil
}

func decodePNM(r io.Reader) (image.Image, error) {
	br := bufio.NewReader(r)
	h, err := readPNMHeader(br)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, h.w, h.h))
	channels := 1
	if h.magic == '3' || h.magic == '6' {
		channels = 3
	}
	wide := h.maxv > 255
	sample := func() (int, error) {
		switch h.magic {
		case '1', '2', '3':
			return pnmInt(br)
		}
		if wide {
			var b [2]byte
			_, err := io.ReadFull(br, b[:])
			return int(b[0])<<8 | int(b[1]), err
		}
		c, err := br.ReadByte()
		return int(c), err
	}
	scale := func(v int) uint8 { return uint8(min(v, h.maxv) * 255 / h.maxv) }
	for y := range h.h {
		if h.magic == '4' {
			// Packed bitmap rows, MSB first; 1 is black.
			row := make([]byte, (h.w+7)/8)
			if _, err := io.ReadFull(br, row); err != nil {
				return nil, err
			}
			for x := range h.w {
				v := uint8(255)
				if row[x/8]&(0x80>>(x%8)) != 0 {
					v = 0
				}
				img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
			}
			continue
		}
		for x := range h.w {
			var rgb [3]uint8
			for c := range channels {
				v, err := sample()
				if err != nil {
					return nil, fmt.Errorf("imagedecode: PNM pixels: %w", err)
				}
				if h.magic == '1' {
					rgb[c] = uint8(255 * (1 - min(v, 1))) // 1 is black
				} else {
					rgb[c] = scale(v)
				}
			}
			if channels == 1 {
				rgb[1], rgb[2] = rgb[0], rgb[0]
			}
			img.SetNRGBA(x, y, color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255})
		}
	}
	return img, nil
}

// TGA: uncompressed and RLE true-color (24/32 bit) and grayscale (8 bit).

func decodeTGA(r io.Reader) (image.Image, error) {
	var hdr [18]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	idLen, cmapType, imgType := int(hdr[0]), hdr[1], hdr[2]
	w := int(binary.LittleEndian.Uint16(hdr[12:14]))
	h := int(binary.LittleEndian.Uint16(hdr[14:16]))
	bpp, desc := int(hdr[16]), hdr[17]
	if cmapType != 0 {
		return nil, errors.New("imagedecode: color-mapped TGA is not supported")
	}
	rle := imgType == 10 || imgType == 11
	gray := imgType == 3 || imgType == 11
	switch {
	case imgType != 2 && imgType != 3 && imgType != 10 && imgType != 11:
		return nil, fmt.Errorf("imagedecode: TGA type %d", imgType)
	case gray && bpp != 8, !gray && bpp != 24 && bpp != 32:
		return nil, fmt.Errorf("imagedecode: TGA depth %d", bpp)
	}
	if err := checkSize(w, h); err != nil {
		return nil, err
	}
	if _, err := io.CopyN(io.Discard, r, int64(idLen)); err != nil {
		return nil, err
	}
	px := bpp / 8
	data := make([]byte, w*h*px)
	if !rle {
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
	} else {
		for i := 0; i < len(data); {
			var head [1]byte
			if _, err := io.ReadFull(r, head[:]); err != nil {
				return nil, err
			}
			n := int(head[0]&0x7f) + 1
			if i+n*px > len(data) {
				return nil, errors.New("imagedecode: TGA run overflows the image")
			}
			if head[0]&0x80 != 0 {
				if _, err := io.ReadFull(r, data[i:i+px]); err != nil {
					return nil, err
				}
				for k := 1; k < n; k++ {
					copy(data[i+k*px:], data[i:i+px])
				}
			} else if _, err := io.ReadFull(r, data[i:i+n*px]); err != nil {
				return nil, err
			}
			i += n * px
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	topDown := desc&0x20 != 0
	rightToLeft := desc&0x10 != 0
	for y := range h {
		for x := range w {
			p := data[(y*w+x)*px:]
			dx, dy := x, h-1-y
			if topDown {
				dy = y
			}
			if rightToLeft {
				dx = w - 1 - x
			}
			c := color.NRGBA{A: 255}
			if gray {
				c.R, c.G, c.B = p[0], p[0], p[0]
			} else {
				c.B, c.G, c.R = p[0], p[1], p[2]
				if px == 4 {
					c.A = p[3]
				}
			}
			img.SetNRGBA(dx, dy, c)
		}
	}
	return img, nil
}
