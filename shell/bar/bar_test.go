package bar

import (
	"testing"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
)

func TestAnchorsForEachLocation(t *testing.T) {
	for _, tc := range []struct {
		location config.Location
		want     app.Anchor
	}{
		{config.LocationTop, app.AnchorTop | app.AnchorLeft | app.AnchorRight},
		{config.LocationBottom, app.AnchorBottom | app.AnchorLeft | app.AnchorRight},
		{config.LocationLeft, app.AnchorLeft | app.AnchorTop | app.AnchorBottom},
		{config.LocationRight, app.AnchorRight | app.AnchorTop | app.AnchorBottom},
	} {
		if got := AnchorsFor(tc.location); got != tc.want {
			t.Errorf("AnchorsFor(%q) = %d, want %d", tc.location, got, tc.want)
		}
	}
}

func TestAnchorsForUnknownIsZero(t *testing.T) {
	if got := AnchorsFor("sideways"); got != 0 {
		t.Errorf("AnchorsFor(sideways) = %d, want 0 (validation rejects this at load)", got)
	}
}

func TestLayerForEachConfigLayer(t *testing.T) {
	for _, tc := range []struct {
		layer config.Layer
		want  app.Layer
	}{
		{config.LayerBackground, app.LayerBackground},
		{config.LayerBottom, app.LayerBottom},
		{config.LayerTop, app.LayerTop},
		{config.LayerOverlay, app.LayerOverlay},
	} {
		if got := LayerFor(tc.layer); got != tc.want {
			t.Errorf("LayerFor(%q) = %d, want %d", tc.layer, got, tc.want)
		}
	}
}

func TestLayerForUnknownFallsBackToTop(t *testing.T) {
	if got := LayerFor("middle"); got != app.LayerTop {
		t.Errorf("LayerFor(middle) = %d, want LayerTop", got)
	}
}

func TestExclusiveZoneReservesBarHeightOnlyWhenSet(t *testing.T) {
	if got := exclusiveZone(true, 32); got != 32 {
		t.Errorf("exclusiveZone(true, 32) = %d, want 32", got)
	}
	if got := exclusiveZone(false, 32); got != 0 {
		t.Errorf("exclusiveZone(false, 32) = %d, want 0", got)
	}
}

func TestWithAlphaMapsPercentageToAlphaByte(t *testing.T) {
	c := render.RGB(0x1e, 0x1e, 0x2e)
	if got := withAlpha(c, 100); got != c {
		t.Errorf("withAlpha(100) = %#08x, want the opaque color %#08x", got, c)
	}
	if got := withAlpha(c, 0); got&0xFF000000 != 0 {
		t.Errorf("withAlpha(0) = %#08x, want a fully transparent color", got)
	}
	if got := withAlpha(c, 50); got&0xFF000000>>24 != 127 {
		t.Errorf("withAlpha(50) alpha = %d, want 127", got&0xFF000000>>24)
	}
	if got := withAlpha(c, 200); got&0xFF000000>>24 != 255 {
		t.Errorf("withAlpha(200) alpha = %d, want clamped 255", got&0xFF000000>>24)
	}
	if got := withAlpha(c, -5); got&0xFF000000 != 0 {
		t.Errorf("withAlpha(-5) = %#08x, want clamped transparent", got)
	}
}

func TestLogicalWidthDividesByTheEffectiveScale(t *testing.T) {
	for _, tc := range []struct {
		name        string
		modeW       int
		outputScale int
		configured  float64
		want        int
	}{
		{"integer scale from the output", 3840, 2, 1.0, 1920},
		{"configured scale overrides a larger output scale", 3840, 1, 2.0, 1920},
		{"output scale wins when larger", 3840, 2, 1.0, 1920},
		{"zero scales fall back to 1", 1920, 0, 0.25, 1920},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := logicalWidth(tc.modeW, tc.outputScale, tc.configured); got != tc.want {
				t.Errorf("logicalWidth(%d, %d, %v) = %d, want %d", tc.modeW, tc.outputScale, tc.configured, got, tc.want)
			}
		})
	}
}
