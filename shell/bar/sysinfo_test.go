package bar

import (
	"testing"

	"github.com/stubbedev/gelm/widget"

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
	cfg.CPU.Thresholds = []config.ThresholdEntry{{Above: ptrF(90), LabelColor: &cv, IconColor: &cv}}
	ctx := ModuleContext{Config: cfg, Font: testFont(t), Style: &style}
	m := &pollModule{ctx: ctx, cfg: cfg.CPU}
	m.label = widget.NewLabel(ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(ctx, cfg.CPU.Icon)

	m.render(50)
	if m.label.Color() != style.fg || m.icon.Tint() != moduleIconTint(ctx, cfg.CPU.Icon.Color) {
		t.Error("50 below the threshold recolored the module")
	}
	m.render(95)
	want, _ := styling.ResolveColor(config.ColorValue{Token: "status-error"}, styling.Default())
	if m.label.Color() != want || m.icon.Tint() != want {
		t.Errorf("at 95: label %#08x icon %#08x, want status-error", m.label.Color(), m.icon.Tint())
	}
}
