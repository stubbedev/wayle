package bar

import (
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/sysinfo"
	"github.com/stubbedev/wayle/styling"
)

func TestSysinfoContextsMatchTheRustHelpers(t *testing.T) {
	// helpers.rs: "{:02.0}" pads single digits (rounding), leaves the rest.
	for v, want := range map[float32]string{7.2: "07", 42.9: "43", 100: "100", 0: "00", 2.5: "02"} {
		if got := pad2(v); got != want {
			t.Errorf("pad2(%v) = %q, want %q", v, got, want)
		}
	}
	cpu := sysinfo.CPU{UsagePercent: 7, BusiestCoreFreqMHz: 3450, AvgFrequencyMHz: 2000, MaxFrequencyMHz: 4999, TemperatureC: 45.5, HasTemperature: true}
	got := jinja.RenderOr("{{ percent }}% {{ freq_ghz }}/{{ avg_freq_ghz }}/{{ max_freq_ghz }} {{ temp_c }}C {{ temp_f }}F", cpuContext(cpu))
	if got != "07% 3.5/2.0/5.0 46C 114F" {
		t.Errorf("cpu = %q", got)
	}
	// No sensor reads as 0 degrees, as unwrap_or(0.0).
	if got := jinja.RenderOr("{{ temp_c }}/{{ temp_f }}", cpuContext(sysinfo.CPU{})); got != "00/32" {
		t.Errorf("no sensor = %q", got)
	}
	mem := sysinfo.Memory{Total: 16 << 30, Available: 12 << 30, SwapTotal: 4 << 30, SwapFree: 3 << 30}
	if got := jinja.RenderOr("{{ percent }} {{ used_gib }}/{{ total_gib }} {{ available_gib }} {{ swap_percent }} {{ swap_used_gib }}/{{ swap_total_gib }}", ramContext(mem)); got != "25 4.0/16.0 12.0 25 1.0/4.0" {
		t.Errorf("ram = %q", got)
	}
	st := sysinfo.Storage{UsagePercent: 50, UsedBytes: 1 << 40, TotalBytes: 2 << 40, AvailableBytes: 1 << 40, Filesystem: "ext4"}
	if got := jinja.RenderOr("{{ used_tib }} {{ total_gib }} {{ free_mib }} {{ used_auto }} {{ filesystem }}", storageContext(st)); got != "1.00 2048.0 1048576 1.0 TiB ext4" {
		t.Errorf("storage = %q", got)
	}
	st.Multiple = true
	if got := jinja.RenderOr("{{ filesystem }}", storageContext(st)); got != i18n.T("bar-storage-multiple") {
		t.Errorf("multiple = %q", got)
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
	m := &pollModule{ctx: ctx, cfg: pollConfig{c.Format, c.Thresholds, c.Icon(), c.PollIntervalMs}}
	m.label = widget.NewLabel(ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(ctx, cfg.CPU.Icon())
	btn := newBarButton(ctx, m.icon, m.label)
	m.setButton(btn)

	m.render(reading{vars: cpuContext(sysinfo.CPU{UsagePercent: 50}), value: 50, ok: true})
	if strings.Contains(btn.InlineStyle(), cv.ToCSS()) {
		t.Errorf("50 below the threshold recolored the button: %q", btn.InlineStyle())
	}
	m.render(reading{vars: cpuContext(sysinfo.CPU{UsagePercent: 95}), value: 95, ok: true})
	if got := m.label.Text(); got != "95%" {
		t.Errorf("label = %q, want the rendered default format", got)
	}
	// A reading with nothing to show (no mount point) keeps the label.
	m.render(reading{})
	if got := m.label.Text(); got != "95%" {
		t.Errorf("an empty reading changed the label to %q", got)
	}
	for _, slot := range []string{"--bar-btn-label-color: ", "--bar-btn-icon-color: "} {
		if !strings.Contains(btn.InlineStyle(), slot+cv.ToCSS()) {
			t.Errorf("at 95 the button vars = %q, want %sstatus-error", btn.InlineStyle(), slot)
		}
	}
}
