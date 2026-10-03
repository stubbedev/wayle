package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// tokenColor resolves the token itself: a ColorValue without its Kind
// is ColorAuto, which once turned every dropdown token into the accent.
func TestTokenColorResolvesTheToken(t *testing.T) {
	p := styling.Default()
	if p.FgMuted == p.Primary || p.Fg == p.Primary {
		t.Fatal("fixture palette cannot tell the tokens from the accent")
	}
	for tok, want := range map[config.CssToken]uint32{
		config.TokenFgMuted:   uint32(p.FgMuted),
		config.TokenFgDefault: uint32(p.Fg),
		config.TokenAccent:    uint32(p.Primary),
	} {
		if got := uint32(tokenColor(p, tok)); got != want {
			t.Errorf("tokenColor(%s) = %08x, want %08x", tok, got, want)
		}
	}
	if mutedFg(p) != p.FgMuted {
		t.Errorf("mutedFg = %08x, want fg-muted %08x", uint32(mutedFg(p)), uint32(p.FgMuted))
	}
}
