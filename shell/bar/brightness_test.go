package bar

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/styling"
)

func TestBrightnessLabel(t *testing.T) {
	if got := brightnessLabel("{{ percent }}%", 42.6, true); got != "43%" {
		t.Errorf("= %q, want 43%%", got)
	}
	if got := brightnessLabel("{{ percent }}", 0, true); got != "0" {
		t.Errorf("= %q, want 0", got)
	}
	if got := brightnessLabel("{{ percent }}%", 0, false); got != "N/A" {
		t.Errorf("no devices = %q, want N/A", got)
	}
}

func TestAveragePercentage(t *testing.T) {
	if _, ok := averagePercentage(nil); ok {
		t.Error("no devices: want present=false")
	}
	got, ok := averagePercentage([]brightness.Device{
		{Brightness: 2500, Max: 10000},
		{Brightness: 7500, Max: 10000},
	})
	if !ok || got < 49.9 || got > 50.1 {
		t.Errorf("average = %v/%v, want 50", got, ok)
	}
}

// fakeBrightnessSource is a scripted brightness.Source.
type fakeBrightnessSource struct {
	devices []brightness.Device
	ticks   chan struct{}
}

func (f *fakeBrightnessSource) Devices(context.Context) ([]brightness.Device, error) {
	return f.devices, nil
}

func (f *fakeBrightnessSource) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	return f.ticks, func() {}, nil
}

func (f *fakeBrightnessSource) Set(context.Context, string, float64) error { return nil }

func TestBrightnessModuleShowsAverageAndRestyles(t *testing.T) {
	cfg := config.Defaults()
	cv, err := config.ParseColorValue("status-warning")
	if err != nil {
		t.Fatal(err)
	}
	above := config.ThresholdEntry{Above: ptrF(80), IconColor: cv, ColorSet: true}
	cfg.Brightness.Thresholds = []config.ThresholdEntry{above}

	source := &fakeBrightnessSource{
		devices: []brightness.Device{
			{Brightness: 4000, Max: 10000},
			{Brightness: 6000, Max: 10000},
		},
		ticks: make(chan struct{}, 2),
	}
	style := computeStyle(cfg, styling.Default())
	m := &brightnessModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "50%" {
		t.Errorf("label = %q, want 50%%", got)
	}
	if m.label.Color() != style.fg {
		t.Errorf("color = %#08x, want default fg", m.label.Color())
	}

	source.devices = []brightness.Device{{Brightness: 9000, Max: 10000}}
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "90%" {
		t.Errorf("label = %q, want 90%%", got)
	}
	warning, _ := styling.ResolveColor(cv, styling.Default())
	if m.label.Color() != warning {
		t.Errorf("color at 90%% = %#08x, want the status-warning override", m.label.Color())
	}
}

func TestBrightnessNoDevicesShowsNA(t *testing.T) {
	cfg := config.Defaults()
	source := &fakeBrightnessSource{ticks: make(chan struct{}, 1)}
	style := computeStyle(cfg, styling.Default())
	m := &brightnessModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "N/A" {
		t.Errorf("label = %q, want N/A", got)
	}
}

func TestNewBrightnessRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Brightness = nil
	if _, err := Create("brightness", ctx); err == nil {
		t.Fatal("nil source: want an error, got a module")
	}
}

func TestLoadFileAppliesBrightness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.brightness]\nformat = \"L {{ percent }}\"\nmin-brightness = 5\nenable-external = false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Brightness.Format != "L {{ percent }}" || c.Brightness.MinBright != 5 || c.Brightness.EnableExt {
		t.Errorf("config = %+v", c.Brightness)
	}
}

func TestLoadFileRejectsBadBrightness(t *testing.T) {
	for _, content := range []string{
		"[modules.brightness]\nmin-brightness = 101\n",
		"[modules.brightness]\nmin-brightness = -1\n",
		"[modules.brightness]\n[[modules.brightness.thresholds]]\nicon-color = \"accent\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}
