package layering

import (
	"testing"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

func TestForEachConfigLayer(t *testing.T) {
	for layer, want := range map[config.Layer]app.Layer{
		config.LayerBackground: app.LayerBackground,
		config.LayerBottom:     app.LayerBottom,
		config.LayerTop:        app.LayerTop,
		config.LayerOverlay:    app.LayerOverlay,
	} {
		if got := For(config.GeneralConfig{}, layer); got != want {
			t.Errorf("For(%q) = %d, want %d", layer, got, want)
		}
	}
	if got := For(config.GeneralConfig{}, "middle"); got != app.LayerTop {
		t.Errorf("an unknown layer = %d, want top", got)
	}
}

func TestTearingModeDemotesOnlyOverlay(t *testing.T) {
	tearing := config.GeneralConfig{TearingMode: true}
	if got := For(tearing, config.LayerOverlay); got != app.LayerTop {
		t.Errorf("overlay under tearing mode = %d, want top", got)
	}
	if got := For(tearing, config.LayerBottom); got != app.LayerBottom {
		t.Errorf("bottom under tearing mode = %d, want bottom", got)
	}
}

func TestOutputPicksTheMonitor(t *testing.T) {
	dp, hdmi := &app.Output{Name: "DP-1"}, &app.Output{Name: "HDMI-A-1"}
	outs := []*app.Output{dp, hdmi}
	if got := Output(outs, config.PopupMonitor{}); got != dp {
		t.Errorf("primary = %v, want the first output", got)
	}
	if got := Output(outs, config.OsdOnConnector("HDMI-A-1")); got != hdmi {
		t.Errorf("connector = %v, want HDMI-A-1", got)
	}
	if got := Output(outs, config.PopupOnConnector("DP-9")); got != dp {
		t.Errorf("missing connector = %v, want the primary fallback", got)
	}
	if got := Output(nil, config.OsdMonitor{}); got != nil {
		t.Errorf("no outputs = %v, want nil", got)
	}
}
