package styling

import (
	"testing"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
)

func TestColorMixInterpolatesPremultipliedChannels(t *testing.T) {
	black := render.RGB(0, 0, 0)
	white := render.RGB(0xff, 0xff, 0xff)
	if got := ColorMix(black, white, 50); got != render.RGB(0x80, 0x80, 0x80) {
		t.Errorf("ColorMix(black, white, 50) = %#08x, want mid gray", got)
	}
	if got := ColorMix(black, white, 0); got != white {
		t.Errorf("ColorMix(black, white, 0) = %#08x, want white", got)
	}
	if got := ColorMix(black, white, 100); got != black {
		t.Errorf("ColorMix(black, white, 100) = %#08x, want black", got)
	}
	// Mixing with transparent scales the premultiplied channels, which
	// is what makes color-mix(..., transparent) the opacity idiom.
	if got := ColorMix(white, transparent, 50); got>>24 != 0x80 {
		t.Errorf("ColorMix(white, transparent, 50) alpha = %#02x, want half", got>>24)
	}
	// Out-of-range percentages clamp instead of smearing.
	if got := ColorMix(black, white, 150); got != black {
		t.Errorf("ColorMix at 150 = %#08x, want clamped to black", got)
	}
}

func TestTokenTableMatchesTokensSCSS(t *testing.T) {
	p := Default()
	for _, tc := range []struct {
		token config.CssToken
		want  render.Color
	}{
		// Pass-through tokens.
		{config.TokenBgBase, p.Bg},
		{config.TokenBgSurface, p.Surface},
		{config.TokenBgElevated, p.Elevated},
		{config.TokenFgDefault, p.Fg},
		{config.TokenFgMuted, p.FgMuted},
		{config.TokenAccent, p.Primary},
		// color-mix tokens, one per percentage used in tokens.scss.
		{config.TokenBgSurfaceElevated, ColorMix(p.Surface, p.Fg, 93)},
		{config.TokenBgOverlay, ColorMix(p.Elevated, p.Fg, 92)},
		{config.TokenBgHover, ColorMix(p.Elevated, p.Fg, 88)},
		{config.TokenBgActive, ColorMix(p.Elevated, p.Fg, 82)},
		{config.TokenBgSelected, ColorMix(p.Primary, transparent, 15)},
		{config.TokenFgSubtle, ColorMix(p.Fg, transparent, 50)},
		{config.TokenAccentHover, ColorMix(p.Primary, white, 80)},
		{config.TokenStatusErrorHover, ColorMix(p.Red, black, 85)},
		{config.TokenBorderSubtle, ColorMix(p.Fg, transparent, 12)},
		{config.TokenBorderDefault, ColorMix(p.Elevated, p.Fg, 90)},
		{config.TokenBorderStrong, ColorMix(p.Elevated, p.Fg, 82)},
	} {
		got, ok := p.Token(tc.token)
		if !ok {
			t.Errorf("Token(%q): not in the table", tc.token)
			continue
		}
		if got != tc.want {
			t.Errorf("Token(%q) = %#08x, want %#08x", tc.token, got, tc.want)
		}
	}
}

func TestTokenTableCoversEveryConfigToken(t *testing.T) {
	for _, token := range []config.CssToken{
		config.TokenBgBase, config.TokenBgSurface, config.TokenBgSurfaceElevated,
		config.TokenBgElevated, config.TokenBgOverlay, config.TokenBgHover,
		config.TokenBgActive, config.TokenBgSelected,
		config.TokenFgDefault, config.TokenFgMuted, config.TokenFgSubtle, config.TokenFgOnAccent,
		config.TokenAccent, config.TokenAccentSubtle, config.TokenAccentHover,
		config.TokenStatusError, config.TokenStatusWarning, config.TokenStatusSuccess,
		config.TokenStatusInfo, config.TokenStatusErrorSubtle, config.TokenStatusWarningSubtle,
		config.TokenStatusSuccessSubtle, config.TokenStatusInfoSubtle, config.TokenStatusErrorHover,
		config.TokenRed, config.TokenYellow, config.TokenGreen, config.TokenBlue,
		config.TokenBorderSubtle, config.TokenBorderDefault, config.TokenBorderStrong,
		config.TokenBorderAccent, config.TokenBorderError,
	} {
		if _, ok := Default().Token(token); !ok {
			t.Errorf("config token %q has no styling resolution", token)
		}
	}
}

func TestResolveColorVariants(t *testing.T) {
	p := Default()
	accent, _ := p.Token(config.TokenAccent)

	if got, ok := ResolveColor(config.ColorValue{Kind: config.ColorAuto}, p); !ok || got != accent {
		t.Errorf("Auto = %#08x, want the accent", got)
	}
	if got, ok := ResolveColor(config.ColorValue{Kind: config.ColorTransparent}, p); !ok || got != 0 {
		t.Errorf("Transparent = %#08x, want fully transparent", got)
	}
	got, ok := ResolveColor(config.ColorValue{Kind: config.ColorCustom, Hex: "#414868"}, p)
	if !ok {
		t.Fatal("Custom hex: not resolvable")
	}
	if got>>24 != 255 {
		t.Errorf("opaque hex kept alpha = %#02x, want 255", got>>24)
	}
	if got>>16&0xFF != 0x41 || got>>8&0xFF != 0x48 || got&0xFF != 0x68 {
		t.Errorf("Custom #414868 = %#08x, want the 414868 channels", got)
	}
	if _, ok := ResolveColor(config.ColorValue{Kind: config.ColorCustom, Hex: "#nothex"}, p); ok {
		t.Error("malformed hex: want unresolvable, got a color")
	}
	if _, ok := ResolveColor(config.ColorValue{Kind: config.ColorToken, Token: "not-a-token"}, p); ok {
		t.Error("unknown token: want unresolvable, got a color")
	}
}

func TestParseHexAlphaForms(t *testing.T) {
	full, ok := parseHex("#112233")
	if !ok || full != render.RGB(0x11, 0x22, 0x33) {
		t.Errorf("#112233 = %#08x, want the opaque color", full)
	}
	half, ok := parseHex("#ffffff80")
	if !ok {
		t.Fatal("#ffffff80: not parsed")
	}
	if half>>24 != 0x80 {
		t.Errorf("#ffffff80 alpha = %#02x, want 80", half>>24)
	}
	if channel := half >> 16 & 0xFF; channel != 0x80 {
		t.Errorf("#ffffff80 red = %#02x, want premultiplied 80", channel)
	}
	if _, ok := parseHex("#12345"); ok {
		t.Error("#12345: want a parse failure")
	}
}

func TestRoundingRadiusPx(t *testing.T) {
	for _, tc := range []struct {
		level config.RoundingLevel
		scale float64
		want  int
	}{
		{config.RoundingNone, 1, 0},
		{config.RoundingSm, 1, 6},  // 0.375rem
		{config.RoundingMd, 1, 10}, // 0.625rem
		{config.RoundingLg, 1, 14}, // 0.875rem
		{config.RoundingFull, 1, 9999},
		{config.RoundingMd, 2, 20},         // scales with the bar
		{config.RoundingFull, 2, 9999},     // full never scales
		{config.RoundingLevel("xl"), 1, 0}, // invalid levels are sharp
	} {
		if got := RoundingRadiusPx(tc.level, tc.scale); got != tc.want {
			t.Errorf("RoundingRadiusPx(%q, %v) = %d, want %d", tc.level, tc.scale, got, tc.want)
		}
	}
}

func TestHexRGBARoundTrips(t *testing.T) {
	opaque := render.RGB(0x18, 0x18, 0x25)
	if got := HexRGBA(opaque); got != "#181825" {
		t.Errorf("HexRGBA(opaque) = %q, want #181825", got)
	}
	half := ColorMix(white, transparent, 50)
	if got := HexRGBA(half); got != "#ffffff80" {
		t.Errorf("HexRGBA(half) = %q, want #ffffff80", got)
	}
	back, ok := parseHex(HexRGBA(half))
	if !ok || back != half {
		t.Errorf("HexRGBA then parseHex = %#08x, want %#08x", back, half)
	}
}
