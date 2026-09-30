package styling

import (
	"math"
	"strconv"
	"strings"
)

// The palette-provider color math, ported from
// crates/wayle-styling/src/palette_provider/color.rs. The Rust side runs
// on the palette crate in f32; this port reproduces its arithmetic step
// for step in float32 (u8<->f32 stimulus conversions, the RGB<->HSL
// formulas, the clamps), so derived hex strings are byte-identical.
// Every product is wrapped in an explicit float32 conversion, which the
// Go spec guarantees is rounded on its own and never fused into an FMA.

// layerOffset is the lightness step between the derived bg, surface,
// and elevated layers (color.rs OFFSET).
const layerOffset float32 = 0.04

// layers are the three background tones a provider palette derives from
// its one background color.
type layers struct {
	bg, surface, elevated string
}

// deriveLayers spreads a background into bg/surface/elevated: the
// background is the surface, bg one step darker and elevated one step
// lighter in dark mode, the other way round in light mode
// (derive_layers).
func deriveLayers(background string, isLight bool) layers {
	step := layerOffset
	if isLight {
		step = -layerOffset
	}
	return layers{
		bg:       lighten(background, -step),
		surface:  background,
		elevated: lighten(background, step),
	}
}

// hexToRGBA renders a hex color with an alpha as "rgba(r, g, b, a.aa)"
// (hex_to_rgba).
func hexToRGBA(hex string, alpha float32) string {
	r, g, b := parseChannels(hex)
	return "rgba(" + strconv.Itoa(int(r)) + ", " + strconv.Itoa(int(g)) + ", " + strconv.Itoa(int(b)) + ", " +
		strconv.FormatFloat(float64(alpha), 'f', 2, 32) + ")"
}

// lighten shifts a color's HSL lightness by amount, clamped to 0-1.
func lighten(hex string, amount float32) string {
	r, g, b := parseChannels(hex)
	h, s, l := rgbToHSL(u8ToF32(r), u8ToF32(g), u8ToF32(b))
	l = clampUnit(l + amount)
	rf, gf, bf := hslToRGB(h, s, l)
	return "#" + hex2(f32ToU8(rf)) + hex2(f32ToU8(gf)) + hex2(f32ToU8(bf))
}

// parseChannels reads the first three byte pairs after the leading '#'s
// as hex channels; a pair that does not parse is 0, as color.rs's
// unwrap_or(0). A string too short for a pair (where the Rust slice
// panics) reads that channel as 0 too.
func parseChannels(hex string) (r, g, b uint8) {
	digits := strings.TrimLeft(hex, "#")
	ch := func(i int) uint8 {
		if len(digits) < i+2 {
			return 0
		}
		return parseByteRadix16(digits[i : i+2])
	}
	return ch(0), ch(2), ch(4)
}

// parseByteRadix16 is u8::from_str_radix(s, 16).unwrap_or(0): an
// optional leading '+', then hex digits.
func parseByteRadix16(s string) uint8 {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0
	}
	return uint8(v)
}

func hex2(v uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&0xF]})
}

// c23 is 2^23 as f32 bits: the palette crate's rounding constant.
const c23 = 0x4b00_0000

// u8ToF32 is palette's IntoStimulus<f32> for u8: c times the f32
// reciprocal of 255.
func u8ToF32(c uint8) float32 {
	compF := math.Float32frombits(uint32(c)+c23) - math.Float32frombits(c23)
	maxF := math.Float32frombits(255+c23) - math.Float32frombits(c23)
	return compF * float32(1/maxF)
}

// f32ToU8 is palette's IntoStimulus<u8> for f32: scale by 255, cap at
// 255, and round half to even through the 2^23 trick; negatives
// saturate to 0.
func f32ToU8(x float32) uint8 {
	const maxI float32 = 255
	scaled := min(float32(x*maxI), maxI)
	f := scaled + math.Float32frombits(c23)
	bits := math.Float32bits(f)
	if bits < c23 {
		return 0
	}
	return uint8(bits - c23)
}

// rgbToHSL is palette's Hsl::from_color for an Rgb<f32>: the scalar
// branch of FromColorUnclamped, then the saturation/lightness clamp.
// The hue stays unnormalized (it may be negative), as in the crate.
func rgbToHSL(red, green, blue float32) (hue, sat, light float32) {
	red, green, blue = max(red, 0), max(green, 0), max(blue, 0)
	var hi, lo, sep, coeff float32
	if red > green {
		hi, lo, sep, coeff = red, green, green-blue, 0
	} else {
		hi, lo, sep, coeff = green, red, blue-red, 2
	}
	if blue > hi {
		hi, sep, coeff = blue, red-green, 4
	} else if blue < lo {
		lo = blue
	}
	sum := hi + lo
	light = sum / 2
	if hi != lo {
		d := hi - lo
		if sum > 1 {
			sat = d / (2 - sum)
		} else {
			sat = d / sum
		}
		hue = float32(float32(sep/d)+coeff) * 60
	}
	return hue, clampUnit(sat), clampUnit(light)
}

// hslToRGB is palette's Rgb::from_color for an Hsl<f32>: the zone
// selection of FromColorUnclamped, then the 0-1 channel clamp.
func hslToRGB(hue, sat, light float32) (red, green, blue float32) {
	c := float32(1-abs32(float32(light*2)-1)) * sat
	positive := hue - float32(floor32(hue/360)*360)
	h := positive / 60
	hMod2 := h - float32(floor32(float32(h*0.5))*2)
	x := float32(c * float32(1-abs32(hMod2-1)))
	m := light - float32(c*0.5)

	zone := func(lo float32) bool { return h >= lo && h < lo+1 }
	z0, z1, z2, z3, z4 := zone(0), zone(1), zone(2), zone(3), zone(4)
	switch {
	case z1 || z4:
		red = x
	case z2 || z3:
		red = 0
	default:
		red = c
	}
	switch {
	case z0 || z3:
		green = x
	case z1 || z2:
		green = c
	default:
		green = 0
	}
	switch {
	case z0 || z1:
		blue = 0
	case z3 || z4:
		blue = c
	default:
		blue = x
	}
	return clampUnit(red + m), clampUnit(green + m), clampUnit(blue + m)
}

// clampUnit is Rust's f32::clamp(v, 0.0, 1.0) for ordinary (non-NaN) values.
func clampUnit(v float32) float32 { return min(max(v, 0), 1) }

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

func floor32(v float32) float32 { return float32(math.Floor(float64(v))) }
