// Package layering maps a configured layer onto the layer-shell stack
// (wayle-shell-core helpers/layer_shell.rs apply_layer): every shell
// surface with a layer key goes through For, so general.tearing-mode
// demotes them all the same way.
package layering

import (
	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

// For is the stack layer a surface configured with layer takes, with
// tearing mode applied (overlay demoted to top); an unknown layer is
// top.
func For(general config.GeneralConfig, layer config.Layer) app.Layer {
	switch general.EffectiveLayer(layer) {
	case config.LayerBackground:
		return app.LayerBackground
	case config.LayerBottom:
		return app.LayerBottom
	case config.LayerTop:
		return app.LayerTop
	case config.LayerOverlay:
		return app.LayerOverlay
	}
	return app.LayerTop
}
