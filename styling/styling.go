// Package styling resolves wayle's design tokens to concrete colors:
// the Go counterpart of crates/wayle-styling. The palette is the
// runtime source of truth; every token is a color-mix derivation from
// it, ported exactly from scss/tokens/_index.scss so a resolved token
// here matches what the Rust shell's compiled CSS produces.
package styling

import (
	"math"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
)

// RemBase is the rem the SCSS base values multiply: styling.rs's
// REM_BASE.
const RemBase = 16.0

// Palette is the runtime color palette — the --palette-* custom
// properties. Themes replace it wholesale.
type Palette struct {
	Bg, Surface, Elevated render.Color
	Fg, FgMuted           render.Color
	Primary               render.Color
	Red, Yellow           render.Color
	Green, Blue           render.Color
}

// Default returns the palette compiled into _palette.scss: the Catppuccin
// Mocha set the Rust shell falls back to when no theme file overrides it.
func Default() *Palette {
	return &Palette{
		Bg:       render.RGB(0x11, 0x11, 0x1b),
		Surface:  render.RGB(0x18, 0x18, 0x25),
		Elevated: render.RGB(0x1e, 0x1e, 0x2e),
		Fg:       render.RGB(0xcd, 0xd6, 0xf4),
		FgMuted:  render.RGB(0xba, 0xc2, 0xde),
		Primary:  render.RGB(0xb4, 0xbe, 0xfe),
		Red:      render.RGB(0xf3, 0x8b, 0xa8),
		Yellow:   render.RGB(0xf9, 0xe2, 0xaf),
		Green:    render.RGB(0xa6, 0xe3, 0xa1),
		Blue:     render.RGB(0x74, 0xc7, 0xec),
	}
}

var (
	transparent = render.Color(0)
	white       = render.RGB(0xff, 0xff, 0xff)
	black       = render.RGB(0, 0, 0)
)

// ColorMix interpolates two premultiplied colors: color-mix(in srgb, a
// pct%, b). CSS interpolates premultiplied channels, which is exactly
// what interpolating render.Color's bytes does — straight-alpha edge
// cases (mixing with transparent) come out right by construction.
func ColorMix(a, b render.Color, pct int) render.Color {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	w := float64(pct) / 100
	var out uint32
	for shift := uint(0); shift <= 24; shift += 8 {
		ca := float64((uint32(a) >> shift) & 0xFF)
		cb := float64((uint32(b) >> shift) & 0xFF)
		out |= uint32(math.Round(ca*w+cb*(1-w))) << shift
	}
	return render.Color(out)
}

// Token resolves one design token against the palette. The percentages
// are the tokens.scss color-mix table, one entry per token; a token
// without a mix is a palette color passed through.
func (p *Palette) Token(t config.CssToken) (render.Color, bool) {
	switch t {
	case config.TokenBgBase:
		return p.Bg, true
	case config.TokenBgSurface:
		return p.Surface, true
	case config.TokenBgSurfaceElevated:
		return ColorMix(p.Surface, p.Fg, 93), true
	case config.TokenBgElevated:
		return p.Elevated, true
	case config.TokenBgOverlay:
		return ColorMix(p.Elevated, p.Fg, 92), true
	case config.TokenBgHover:
		return ColorMix(p.Elevated, p.Fg, 88), true
	case config.TokenBgActive:
		return ColorMix(p.Elevated, p.Fg, 82), true
	case config.TokenBgSelected:
		return ColorMix(p.Primary, transparent, 15), true

	case config.TokenFgDefault:
		return p.Fg, true
	case config.TokenFgMuted:
		return p.FgMuted, true
	case config.TokenFgSubtle:
		return ColorMix(p.Fg, transparent, 50), true
	case config.TokenFgOnAccent:
		return p.Surface, true

	case config.TokenAccent:
		return p.Primary, true
	case config.TokenAccentSubtle:
		return ColorMix(p.Primary, transparent, 15), true
	case config.TokenAccentHover:
		return ColorMix(p.Primary, white, 80), true

	case config.TokenStatusError:
		return p.Red, true
	case config.TokenStatusWarning:
		return p.Yellow, true
	case config.TokenStatusSuccess:
		return p.Green, true
	case config.TokenStatusInfo:
		return p.Blue, true
	case config.TokenStatusErrorSubtle:
		return ColorMix(p.Red, transparent, 15), true
	case config.TokenStatusWarningSubtle:
		return ColorMix(p.Yellow, transparent, 15), true
	case config.TokenStatusSuccessSubtle:
		return ColorMix(p.Green, transparent, 15), true
	case config.TokenStatusInfoSubtle:
		return ColorMix(p.Blue, transparent, 15), true
	case config.TokenStatusErrorHover:
		return ColorMix(p.Red, black, 85), true

	case config.TokenRed:
		return p.Red, true
	case config.TokenYellow:
		return p.Yellow, true
	case config.TokenGreen:
		return p.Green, true
	case config.TokenBlue:
		return p.Blue, true

	case config.TokenBorderSubtle:
		return ColorMix(p.Fg, transparent, 12), true
	case config.TokenBorderDefault:
		return ColorMix(p.Elevated, p.Fg, 90), true
	case config.TokenBorderStrong:
		return ColorMix(p.Elevated, p.Fg, 82), true
	case config.TokenBorderAccent:
		return p.Primary, true
	case config.TokenBorderError:
		return p.Red, true
	}
	return 0, false
}

// ResolveColor resolves a config ColorValue against the palette: Auto
// defers to the accent (ColorValue::to_css's documented fallback),
// transparent is fully transparent, custom hex parses, tokens go
// through the token table. An invalid hex or unknown token is a load-
// time error by construction; the bool return covers the custom hex
// that only fails at resolve time.
func ResolveColor(cv config.ColorValue, p *Palette) (render.Color, bool) {
	switch cv.Kind {
	case config.ColorAuto:
		return p.Primary, true
	case config.ColorTransparent:
		return transparent, true
	case config.ColorCustom:
		return parseHex(cv.Hex)
	case config.ColorToken:
		return p.Token(cv.Token)
	}
	return 0, false
}

// parseHex decodes #rgb, #rgba, #rrggbb, or #rrggbbaa (straight alpha,
// the CSS form) into a premultiplied render.Color.
func parseHex(s string) (render.Color, bool) {
	if len(s) != 4 && len(s) != 5 && len(s) != 7 && len(s) != 9 {
		return 0, false
	}
	digits := s[1:]
	if len(digits) <= 4 { // #rgb/#rgba expand each nibble, per CSS
		long := make([]byte, 0, 2*len(digits))
		for i := range len(digits) {
			long = append(long, digits[i], digits[i])
		}
		digits = string(long)
	}
	var vals [4]uint8
	bytes := len(digits) / 2
	for i := range bytes {
		hi := hexVal(digits[i*2])
		lo := hexVal(digits[i*2+1])
		if hi < 0 || lo < 0 {
			return 0, false
		}
		vals[i] = uint8(hi<<4 | lo)
	}
	a := uint8(255)
	if bytes == 4 {
		a = vals[3]
	}
	return rgbaStraight(vals[0], vals[1], vals[2], a), true
}

func hexVal(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return -1
}

// rgbaStraight converts straight-alpha sRGB bytes to premultiplied
// render.Color (0xAARRGGBB).
func rgbaStraight(r, g, b, a uint8) render.Color {
	af := float64(a) / 255
	pre := func(c uint8) uint8 {
		return uint8(math.Round(float64(c) * af))
	}
	return render.Color(uint32(a)<<24 | uint32(pre(r))<<16 | uint32(pre(g))<<8 | uint32(pre(b)))
}

// RoundingRadiusPx resolves a rounding level to corner pixels: the
// --radius-* table (tokens.scss $base-radius-*) times the scale, with
// full pinned at 9999px unscaled.
func RoundingRadiusPx(level config.RoundingLevel, scale float64) int {
	if level == config.RoundingFull {
		return 9999
	}
	var rem float64
	switch level {
	case config.RoundingSm:
		rem = 0.375
	case config.RoundingMd:
		rem = 0.625
	case config.RoundingLg:
		rem = 0.875
	default: // none, and anything invalid: sharp corners
		return 0
	}
	return int(math.Round(rem * RemBase * scale))
}

// HexRGBA renders a premultiplied color as the straight-alpha #rrggbbaa
// (or #rrggbb when opaque) form gelm stylesheets parse.
func HexRGBA(c render.Color) string {
	a := uint8(uint32(c) >> 24)
	hex := func(v uint8) string {
		const digits = "0123456789abcdef"
		return string([]byte{digits[v>>4], digits[v&0xF]})
	}
	if a == 255 {
		return "#" + hex(uint8(uint32(c)>>16)) + hex(uint8(uint32(c)>>8)) + hex(uint8(uint32(c)))
	}
	af := float64(a) / 255
	unpre := func(v uint8) uint8 {
		return uint8(math.Round(float64(v) / af))
	}
	return "#" + hex(unpre(uint8(uint32(c)>>16))) + hex(unpre(uint8(uint32(c)>>8))) +
		hex(unpre(uint8(uint32(c)))) + hex(a)
}
