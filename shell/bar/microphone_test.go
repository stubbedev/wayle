package bar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

func TestMicrophoneLabel(t *testing.T) {
	if got := microphoneLabel(37.2); got != "37%" {
		t.Errorf("= %q, want 37%%", got)
	}
	if got := microphoneLabel(0); got != "0%" {
		t.Errorf("= %q, want 0%%", got)
	}
	// Rounded like the Rust module, not truncated.
	if got := microphoneLabel(37.6); got != "38%" {
		t.Errorf("= %q, want 38%%", got)
	}
}

func TestMicrophoneModuleDimsWhenMuted(t *testing.T) {
	cfg := config.Defaults()
	style := computeStyle(cfg, styling.Default())
	source := &fakePulseSource{dev: pulse.Device{Name: "mic", Volume: pct(65)}, ticks: make(chan struct{}, 2)}
	m := &microphoneModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := m.label.Text(); got != "65%" {
		t.Errorf("label = %q, want 65%%", got)
	}
	if m.label.Color() != style.fg {
		t.Errorf("active color = %#08x, want the default fg", m.label.Color())
	}

	source.dev.Muted = true
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	muted, _ := styling.ResolveColor(config.ColorValue{Token: config.TokenFgMuted}, styling.Default())
	if m.label.Color() != muted {
		t.Errorf("muted color = %#08x, want fg-muted", m.label.Color())
	}
}

func TestNewMicrophoneRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Pulse = nil
	if _, err := Create("microphone", ctx); err == nil {
		t.Fatal("nil pulse source: want an error, got a module")
	}
}

func TestLoadFileAppliesMicrophone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.microphone]\nicon-muted = \"m-off\"\nlabel-show = false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Microphone.IconMuted != "m-off" || c.Microphone.LabelShow {
		t.Errorf("config = %+v", c.Microphone)
	}
}
