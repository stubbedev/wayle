package bar

import (
	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// tokenColor is one palette token as a render color: the single path
// every Go-painted surface takes to the theme's colors. (A
// config.ColorValue built without its Kind is ColorAuto, which resolves
// to the accent whatever the token: never resolve tokens that way.)
func tokenColor(palette *styling.Palette, token config.CssToken) render.Color {
	color, _ := palette.Token(token)
	return color
}

// mutedFg resolves the shared fg-muted ink for dimmed labels (offline
// network, muted microphone, idle bluetooth).
func mutedFg(palette *styling.Palette) render.Color {
	return tokenColor(palette, config.TokenFgMuted)
}
