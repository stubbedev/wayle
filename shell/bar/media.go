package bar

import (
	"context"
	"errors"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/mpris"
)

// mediaStatusText is the _bar.ftl status vocabulary.
var mediaStatusText = map[mpris.PlaybackState]string{
	mpris.StatePlaying: "Playing",
	mpris.StatePaused:  "Paused",
	mpris.StateStopped: "Stopped",
}

// mediaLabel renders the format with the full placeholder set
// (helpers.rs's format_label): title, artist, album, status, and the
// status icons.
func mediaLabel(format string, track mpris.Track) string {
	replacements := [][2]string{
		{"{{ title }}", track.Title},
		{"{{title}}", track.Title},
		{"{{ artist }}", track.Artist},
		{"{{artist}}", track.Artist},
		{"{{ album }}", track.Album},
		{"{{album}}", track.Album},
		{"{{ status }}", mediaStatusText[track.State]},
		{"{{status}}", mediaStatusText[track.State]},
		{"{{ status_icon }}", mediaStatusIcon(track.State)},
		{"{{status_icon}}", mediaStatusIcon(track.State)},
	}
	for _, r := range replacements {
		format = strings.ReplaceAll(format, r[0], r[1])
	}
	return format
}

// mediaStatusIcon is the transport glyph set the Rust module embeds.
func mediaStatusIcon(state mpris.PlaybackState) string {
	switch state {
	case mpris.StatePlaying:
		return "⏵"
	case mpris.StatePaused:
		return "⏸"
	}
	return "⏹"
}

// truncateLabel applies label-max-length with an ellipsis
// (BarButtonBehavior's pango ellipsize-end, done manually).
func truncateLabel(label string, max int) string {
	if max <= 0 {
		return label
	}
	runes := []rune(label)
	if len(runes) <= max {
		return label
	}
	return string(runes[:max-1]) + "…"
}

// media is the module: the active player's now-playing label.
type mediaModule struct {
	ctx    ModuleContext
	source mpris.Source
	label  *widget.Label
}

func newMedia(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("media: requires the application loop")
	}
	if ctx.Media == nil {
		return nil, errors.New("media: no MPRIS source available")
	}
	m := &mediaModule{ctx: ctx, source: ctx.Media}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Media.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
		stop()
	}()
	return m, nil
}

// refresh re-reads and restyles the label.
func (m *mediaModule) refresh() error {
	track, err := m.source.Active(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Media
	label := ""
	if cfg.LabelShow {
		label = truncateLabel(mediaLabel(cfg.Format, track), cfg.LabelMaxLength)
	}
	m.label.SetText(label)
	m.label.SetColor(m.ctx.Style.fg)
	return nil
}

func (m *mediaModule) Root() widget.Widget { return m.label }
