package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

func TestSysinfoLabelZeroPads(t *testing.T) {
	// helpers.rs: "{:02.0}" pads single digits, leaves the rest.
	if got := sysinfoLabel("{{ percent }}%", 7.2); got != "07%" {
		t.Errorf("= %q, want 07%%", got)
	}
	if got := sysinfoLabel("{{ percent }}%", 42.9); got != "42%" {
		t.Errorf("= %q, want 42%%", got)
	}
	if got := sysinfoLabel("{{ percent }}%", 100); got != "100%" {
		t.Errorf("= %q, want 100%%", got)
	}
	if got := sysinfoLabel("CPU {{ percent }}", 0); got != "CPU 00" {
		t.Errorf("= %q, want CPU 00", got)
	}
}

func TestSysinfoThresholdColors(t *testing.T) {
	cfg := config.Defaults()
	cv, err := config.ParseColorValue("status-error")
	if err != nil {
		t.Fatal(err)
	}
	style := computeStyle(cfg, styling.Default())
	thresholds := []config.ThresholdEntry{{Above: ptrF(90), IconColor: cv, ColorSet: true}}

	if _, ok := sysinfoThreshold(50, thresholds, style.palette); ok {
		t.Error("50 below the threshold should not match")
	}
	color, ok := sysinfoThreshold(95, thresholds, style.palette)
	if !ok {
		t.Fatal("95 above the threshold should match")
	}
	want, _ := styling.ResolveColor(config.ColorValue{Token: "status-error"}, styling.Default())
	if color != want {
		t.Errorf("color = %#08x, want status-error", color)
	}
}
