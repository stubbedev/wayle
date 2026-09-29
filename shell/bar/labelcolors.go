package bar

import (
	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// mutedFg resolves the shared fg-muted ink for dimmed labels (offline
// network, muted microphone, idle bluetooth).
func mutedFg(palette *styling.Palette) render.Color {
	color, _ := styling.ResolveColor(config.ColorValue{Token: config.TokenFgMuted}, palette)
	return color
}
