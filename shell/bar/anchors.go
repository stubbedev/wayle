package bar

import (
	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

// AnchorsFor maps a bar location onto layer-shell anchors the way the
// Rust shell's apply_anchors does: the docked edge plus both edges of
// the stretch axis, so the surface spans the screen while the
// cross-axis auto-sizes to the content.
func AnchorsFor(location config.Location) app.Anchor {
	switch location {
	case config.LocationTop:
		return app.AnchorTop | app.AnchorLeft | app.AnchorRight
	case config.LocationBottom:
		return app.AnchorBottom | app.AnchorLeft | app.AnchorRight
	case config.LocationLeft:
		return app.AnchorLeft | app.AnchorTop | app.AnchorBottom
	case config.LocationRight:
		return app.AnchorRight | app.AnchorTop | app.AnchorBottom
	}
	return 0
}
