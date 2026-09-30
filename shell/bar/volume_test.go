package bar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

// fakePulseSource is a scripted pulse.Source: dev is both the default
// sink and the default source.
type fakePulseSource struct {
	dev   pulse.Device
	ticks chan struct{}
}

// pct is a stereo volume at a percentage.
func pct(p float64) pulse.Volume { return pulse.VolumeFromPercentage(p, 2) }

func (f *fakePulseSource) DefaultSink(context.Context) (pulse.OutputDevice, error) {
	return pulse.OutputDevice{Device: f.dev}, nil
}

func (f *fakePulseSource) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	return f.ticks, func() {}, nil
}

func (f *fakePulseSource) SetVolume(context.Context, float64) error { return nil }

func (f *fakePulseSource) SetMuted(context.Context, bool) error { return nil }

func (f *fakePulseSource) DefaultSource(context.Context) (pulse.InputDevice, error) {
	return pulse.InputDevice{Device: f.dev}, nil
}

func (f *fakePulseSource) SetSourceMuted(context.Context, bool) error { return nil }

func (f *fakePulseSource) Capture(pulse.CaptureTarget, pulse.CaptureSpec, func([]byte)) (*pulse.Capture, error) {
	return nil, errors.New("fake: no capture")
}

func TestVolumeLabelRounds(t *testing.T) {
	if got := volumeLabel("{{ percent }}%", 12.4); got != "12%" {
		t.Errorf("= %q, want 12%%", got)
	}
	if got := volumeLabel("Vol {{ percent }}", 100); got != "Vol 100" {
		t.Errorf("= %q, want Vol 100", got)
	}
}

func TestVolumePercentAveragesAndRounds(t *testing.T) {
	// Uneven channels show their rounded mean (average_percentage).
	if got := volumePercent(pulse.Device{Volume: pulse.StereoVolume(0.3, 0.505)}); got != 40 {
		t.Errorf("percent = %v, want 40", got)
	}
	if got := volumePercent(pulse.Device{Volume: pulse.NewVolume(nil)}); got != 0 {
		t.Errorf("no channels = %v, want 0", got)
	}
	// A 94.6% level rounds to 95 and so crosses an above-94.9 line.
	cfg := config.Defaults()
	cv, _ := config.ParseColorValue("status-error")
	cfg.Volume.Thresholds = []config.ThresholdEntry{{Above: ptrF(94.9), IconColor: cv, ColorSet: true}}
	style := computeStyle(cfg, styling.Default())
	got := volumeColor(pulse.Device{Volume: pct(94.6)}, cfg.Volume, style.palette, style.fg)
	if want, _ := styling.ResolveColor(cv, styling.Default()); got != want {
		t.Errorf("threshold on the rounded level: %#08x, want %#08x", got, want)
	}
	if got := volumeColor(pulse.Device{Volume: pct(94.4)}, cfg.Volume, style.palette, style.fg); got != style.fg {
		t.Errorf("94.4%% rounds to 94 and stays default, got %#08x", got)
	}
}

func TestVolumeModuleRendersAndRestyles(t *testing.T) {
	cfg := config.Defaults()
	cv, err := config.ParseColorValue("status-error")
	if err != nil {
		t.Fatal(err)
	}
	loud := config.ThresholdEntry{Above: ptrF(95), IconColor: cv, ColorSet: true}
	cfg.Volume.Thresholds = []config.ThresholdEntry{loud}

	source := &fakePulseSource{
		dev:   pulse.Device{Name: "out", Volume: pct(42), Muted: false},
		ticks: make(chan struct{}, 2),
	}
	style := computeStyle(cfg, styling.Default())
	m := &volumeModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "42%" {
		t.Errorf("label = %q, want 42%%", got)
	}
	if m.label.Color() != style.fg {
		t.Errorf("color = %#08x, want default fg", m.label.Color())
	}

	source.dev.Volume = pct(100)
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "100%" {
		t.Errorf("label = %q, want 100%%", got)
	}
	errorColor, _ := styling.ResolveColor(cv, styling.Default())
	if m.label.Color() != errorColor {
		t.Errorf("color at 100 = %#08x, want the status-error override", m.label.Color())
	}
}

func TestVolumeMutedDimsTheLabel(t *testing.T) {
	cfg := config.Defaults()
	source := &fakePulseSource{
		dev:   pulse.Device{Volume: pct(42), Muted: true},
		ticks: make(chan struct{}, 1),
	}
	style := computeStyle(cfg, styling.Default())
	m := &volumeModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "42%" {
		t.Errorf("muted label = %q, want the percent (icons are not ported; mute shows as fg-muted)", got)
	}
	mutedColor, _ := styling.ResolveColor(config.ColorValue{Token: config.TokenFgMuted}, styling.Default())
	if m.label.Color() != mutedColor {
		t.Errorf("muted color = %#08x, want fg-muted", m.label.Color())
	}
}

func TestLoadFileAppliesVolume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.volume]\nformat = \"S {{ percent }}\"\nlabel-show = false\nicon-muted = \"ld-x-symbolic\"\nlevel-icons = [\"a\", \"b\"]\n"
	if err := osWrite(path, content); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Volume.Format != "S {{ percent }}" || c.Volume.LabelShow {
		t.Errorf("config = %+v", c.Volume)
	}
	if c.Volume.IconMuted != "ld-x-symbolic" {
		t.Errorf("icon-muted = %q", c.Volume.IconMuted)
	}
	if len(c.Volume.LevelIcons) != 2 || c.Volume.LevelIcons[0] != "a" {
		t.Errorf("level-icons = %v", c.Volume.LevelIcons)
	}
}

func TestLoadFileRejectsBadVolume(t *testing.T) {
	for _, content := range []string{
		"[modules.volume]\n[[modules.volume.thresholds]]\nicon-color = \"accent\"\n",
		"[modules.volume]\n[[modules.volume.thresholds]]\nbelow = 10\nicon-color = \"bogus\"\n",
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

func TestNewVolumeRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Pulse = nil
	if _, err := Create("volume", ctx); err == nil {
		t.Fatal("nil pulse source: want an error, got a module")
	}
}
