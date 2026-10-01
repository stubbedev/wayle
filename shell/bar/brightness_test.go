package bar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/styling"
)

func TestBrightnessLabel(t *testing.T) {
	if got := brightnessLabel("{{ percent }}%", 42.6); got != "43%" {
		t.Errorf("= %q, want 43%%", got)
	}
	if got := brightnessLabel("{{ percent }}", 0); got != "0" {
		t.Errorf("= %q, want 0", got)
	}
}

func TestBrightnessIconBands(t *testing.T) {
	levels := []string{"low", "mid", "high"}
	for _, tc := range []struct {
		percent float64
		want    string
	}{{0, "low"}, {33, "low"}, {34, "mid"}, {66, "mid"}, {67, "high"}, {100, "high"}, {150, "high"}, {-5, "low"}} {
		if got := brightnessIcon(levels, tc.percent); got != tc.want {
			t.Errorf("%v%% = %q, want %q", tc.percent, got, tc.want)
		}
	}
	if got := brightnessIcon(nil, 50); got != "" {
		t.Errorf("no icons = %q", got)
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
	above := config.ThresholdEntry{Above: ptrF(80), LabelColor: &cv}
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
	btn := newBarButton(m.ctx, nil, m.label)
	m.setButton(btn)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "50%" {
		t.Errorf("label = %q, want 50%%", got)
	}
	if strings.Contains(btn.InlineStyle(), "--bar-btn-label-color: "+cv.ToCSS()) {
		t.Errorf("the threshold colored the button at 50%%: %q", btn.InlineStyle())
	}

	source.devices = []brightness.Device{{Brightness: 9000, Max: 10000}}
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "90%" {
		t.Errorf("label = %q, want 90%%", got)
	}
	if !strings.Contains(btn.InlineStyle(), "--bar-btn-label-color: "+cv.ToCSS()) {
		t.Errorf("at 90%% the button vars = %q, want the status-warning threshold", btn.InlineStyle())
	}
}

func TestBrightnessNoDevicesShowsDashes(t *testing.T) {
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
	if got := m.label.Text(); got != "--%" {
		t.Errorf("label = %q, want --%%", got)
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
	if c.Brightness.Format != "L {{ percent }}" || c.Brightness.MinBrightness != 5 || c.Brightness.EnableExternal {
		t.Errorf("config = %+v", c.Brightness)
	}
}

func TestLoadFileRejectsBadBrightness(t *testing.T) {
	for _, content := range []string{
		"[modules.brightness]\nmin-brightness = -1\n",
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

// The module's icon is the level icon for the mean brightness; without
// a device it is the first one.
func TestBrightnessModuleIconFollowsTheLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Brightness.LevelIcons = []string{"low", "high"}
	ctx := newTestContext(t, cfg)
	source := &fakeBrightnessSource{devices: []brightness.Device{{Brightness: 9000, Max: 10000}}}
	ctx.Brightness = source
	module, err := Create("brightness", ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := module.(*brightnessModule)
	if m.icon == nil || m.icon.Name() != "high" {
		t.Fatalf("icon at 90%% = %v, want high", m.icon)
	}
	source.devices = nil
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if m.icon.Name() != "low" || m.label.Text() != "--%" {
		t.Errorf("no device: icon %q label %q", m.icon.Name(), m.label.Text())
	}
}
