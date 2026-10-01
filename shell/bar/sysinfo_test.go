package bar

import (
	"strings"
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
	c := cfg.CPU
	m := &pollModule{ctx: ctx, cfg: pollConfig{c.Format, c.LabelShow, c.Thresholds, c.Icon(), c.PollIntervalMs}}
	m.label = widget.NewLabel(ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(ctx, cfg.CPU.Icon())
	btn := newBarButton(ctx, m.icon, m.label)
	m.setButton(btn)

	m.render(50)
	if strings.Contains(btn.InlineStyle(), cv.ToCSS()) {
		t.Errorf("50 below the threshold recolored the button: %q", btn.InlineStyle())
	}
	m.render(95)
	for _, slot := range []string{"--bar-btn-label-color: ", "--bar-btn-icon-color: "} {
		if !strings.Contains(btn.InlineStyle(), slot+cv.ToCSS()) {
			t.Errorf("at 95 the button vars = %q, want %sstatus-error", btn.InlineStyle(), slot)
		}
	}
}
