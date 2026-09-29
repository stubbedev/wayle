package bar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/styling"
)

// fakeMediaSource is a scripted mpris.Source.
type fakeMediaSource struct {
	track mpris.Track
	ticks chan struct{}
}

func (f *fakeMediaSource) Active(context.Context) (mpris.Track, error) { return f.track, nil }

func (f *fakeMediaSource) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	return f.ticks, func() {}, nil
}

func TestMediaLabelPlaceholdersMatchRustAssertions(t *testing.T) {
	playing := mpris.Track{
		Title:  "Song Name",
		Artist: "Artist Name",
		Album:  "Album Name",
		State:  mpris.StatePlaying,
	}
	// helpers.rs's format_label_basic_placeholders.
	if got := mediaLabel("{{ title }} - {{ artist }}", playing); got != "Song Name - Artist Name" {
		t.Errorf("= %q, want the title-artist join", got)
	}
	// format_label_all_placeholders (icon glyph substituted for PLAY_ICON).
	if got := mediaLabel("{{ status_icon }} {{ title }} by {{ artist }} from {{ album }} ({{ status }})", playing); got != "⏵ Song Name by Artist Name from Album Name (Playing)" {
		t.Errorf("= %q", got)
	}
	paused := mpris.Track{State: mpris.StatePaused}
	if got := mediaLabel("{{ status_icon }} {{ status }}", paused); got != "⏸ Paused" {
		t.Errorf("= %q, want the pause glyph and status", got)
	}
	if got := mediaLabel("{{ status }}", mpris.Track{}); got != "Stopped" {
		t.Errorf("zero track = %q, want Stopped", got)
	}
}

func TestTruncateLabelEllipsizes(t *testing.T) {
	if got := truncateLabel("short", 35); got != "short" {
		t.Errorf("short label = %q", got)
	}
	got := truncateLabel(strings.Repeat("x", 40), 35)
	if len([]rune(got)) != 35 || !strings.HasSuffix(got, "…") {
		t.Errorf("long label = %d runes ending %q, want 35 runes with an ellipsis", len([]rune(got)), got[len(got)-3:])
	}
	if got := truncateLabel("unchanged", 0); got != "unchanged" {
		t.Errorf("max=0 disables truncation, got %q", got)
	}
}

func TestMediaModuleRenders(t *testing.T) {
	cfg := config.Defaults()
	source := &fakeMediaSource{
		track: mpris.Track{
			Title:  strings.Repeat("t", 50),
			Artist: "Band",
			State:  mpris.StatePlaying,
		},
		ticks: make(chan struct{}, 2),
	}
	style := computeStyle(cfg, styling.Default())
	m := &mediaModule{
		ctx:    ModuleContext{Config: cfg, Font: testFont(t), Style: &style},
		source: source,
	}
	m.label = widget.NewLabel(m.ctx.Font, style.labelPx, "", style.fg)
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got := m.label.Text()
	if len([]rune(got)) != 35 || !strings.HasSuffix(got, "…") {
		t.Errorf("label = %q (%d runes), want default-format truncated to 35", got, len([]rune(got)))
	}

	// No player at all: an empty label.
	source.track = mpris.Track{}
	if err := m.refresh(); err != nil {
		t.Fatal(err)
	}
	if m.label.Text() != " - " {
		t.Errorf("no-player label = %q, want the empty placeholder join", m.label.Text())
	}
}

func TestNewMediaRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Media = nil
	if _, err := Create("media", ctx); err == nil {
		t.Fatal("nil mpris source: want an error, got a module")
	}
}

func TestLoadFileAppliesMedia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.media]\nformat = \"{{ artist }}: {{ title }}\"\nlabel-max-length = 20\nlabel-show = false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if c.Media.Format != "{{ artist }}: {{ title }}" || c.Media.LabelMaxLength != 20 || c.Media.LabelShow {
		t.Errorf("config = %+v", c.Media)
	}
}

func TestLoadFileRejectsBadMedia(t *testing.T) {
	for _, content := range []string{
		"[modules.media]\nlabel-max-length = -1\n",
		"[modules.media]\n[[modules.media.thresholds]]\nicon-color = \"accent\"\n",
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
