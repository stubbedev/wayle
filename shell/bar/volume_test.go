package bar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

// fakePulseSource is a scripted pulse.Source: dev is both the default
// sink and the default source. The embedded nil Source leaves the
// dropdown's list and control methods unscripted; the audio dropdown
// tests run against a pulsetest server instead.
type fakePulseSource struct {
	pulse.Source
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
	cfg.Volume.Thresholds = []config.ThresholdEntry{{Above: ptrF(94.9), LabelColor: &cv}}
	style := computeStyle(cfg, styling.Default())
	got, _ := thresholdColor(volumePercent(pulse.Device{Volume: pct(94.6)}), cfg.Volume.Thresholds, style.palette)
	if want, _ := styling.ResolveColor(cv, styling.Default()); got != want {
		t.Errorf("threshold on the rounded level: %#08x, want %#08x", got, want)
	}
	if _, hit := thresholdColor(volumePercent(pulse.Device{Volume: pct(94.4)}), cfg.Volume.Thresholds, style.palette); hit {
		t.Error("94.4%% rounds to 94 and must stay default")
	}
}

func TestVolumeModuleRendersAndRestyles(t *testing.T) {
	cfg := config.Defaults()
	cv, err := config.ParseColorValue("status-error")
	if err != nil {
		t.Fatal(err)
	}
	loud := config.ThresholdEntry{Above: ptrF(95), LabelColor: &cv}
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
	btn := newBarButton(m.ctx, nil, m.label)
	m.setButton(btn)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "42%" {
		t.Errorf("label = %q, want 42%%", got)
	}
	if strings.Contains(btn.InlineStyle(), "--bar-btn-label-color: "+cv.ToCSS()) {
		t.Errorf("below the threshold the button vars = %q, want no override", btn.InlineStyle())
	}

	source.dev.Volume = pct(100)
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := m.label.Text(); got != "100%" {
		t.Errorf("label = %q, want 100%%", got)
	}
	if !strings.Contains(btn.InlineStyle(), "--bar-btn-label-color: "+cv.ToCSS()) {
		t.Errorf("at 100 the button vars = %q, want the status-error threshold", btn.InlineStyle())
	}
}

// newVolumeForTest builds the module around a fake source the way
// newVolume does, minus the loop subscription.
func newVolumeForTest(t *testing.T, cfg *config.Config, source *fakePulseSource) *volumeModule {
	t.Helper()
	style := computeStyle(cfg, styling.Default())
	ctx := ModuleContext{Config: cfg, Font: testFont(t), Style: &style}
	m := &volumeModule{ctx: ctx, source: source}
	m.label = widget.NewLabel(ctx.Font, style.labelPx, "", style.fg)
	m.icon = moduleIcon(ctx, cfg.Volume.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	m.setButton(m.root.(*barButton))
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return m
}

func TestVolumeMutedSwapsTheIconInTheTree(t *testing.T) {
	cfg := config.Defaults()
	source := &fakePulseSource{dev: pulse.Device{Volume: pct(42), Muted: true}}
	m := newVolumeForTest(t, cfg, source)
	btn, ok := m.root.(*barButton)
	if !ok || btn.iconBox == nil || len(btn.iconBox.Children()) != 1 {
		t.Fatalf("root = %T, want the icon+label bar button", m.root)
	}
	icon, ok := btn.iconBox.Children()[0].(*widget.Icon)
	if !ok || icon != m.icon {
		t.Fatal("the icon the module updates is not the one in the tree")
	}
	if icon.Name() != cfg.Volume.IconMuted {
		t.Errorf("muted icon = %q, want icon-muted %q", icon.Name(), cfg.Volume.IconMuted)
	}
	// Mute keeps the percent label and sets no color override.
	if got := m.label.Text(); got != "42%" {
		t.Errorf("muted label = %q, want the percent", got)
	}
	if got := btn.InlineStyle(); strings.Contains(got, "--bar-btn-label-color: var(--status") {
		t.Errorf("muted button vars = %q, want no label recolor (mute does not recolor)", got)
	}

	// Unmuting leaves icon-muted for the level icon.
	source.dev.Muted = false
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if want := cfg.Volume.LevelIcons[1]; icon.Name() != want {
		t.Errorf("unmuted icon at 42%% = %q, want %q", icon.Name(), want)
	}
}

func TestVolumeIconNameSelection(t *testing.T) {
	cfg := config.DefaultsVolume()
	cfg.LevelIcons = []string{"vol-1", "vol-2", "vol-3"}
	cfg.IconMuted = "muted"
	for _, tc := range []struct {
		dev  pulse.Device
		want string
	}{
		{pulse.Device{Volume: pct(50), Muted: true}, "muted"},
		{pulse.Device{Volume: pct(0)}, "vol-1"},
		{pulse.Device{Volume: pct(15)}, "vol-1"},
		{pulse.Device{Volume: pct(50)}, "vol-2"},
		{pulse.Device{Volume: pct(100)}, "vol-3"},
		{pulse.Device{Volume: pct(150)}, "vol-3"},
	} {
		if got := volumeIconName(cfg, tc.dev); got != tc.want {
			t.Errorf("%v%%: icon = %q, want %q", tc.dev.Volume.AveragePercentage(), got, tc.want)
		}
	}
	cfg.LevelIcons = nil
	if got := volumeIconName(cfg, pulse.Device{Volume: pct(50)}); got != "muted" {
		t.Errorf("no level icons: icon = %q, want the muted icon", got)
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
