package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/mpris"
)

// The status glyphs helpers.rs embeds (Nerd Font media icons).
const (
	mediaPlayGlyph  = "󰐊"
	mediaPauseGlyph = "󰏤"
	mediaStopGlyph  = "󰓛"
)

// mediaNoPlayerLabel is the label while no player is active.
const mediaNoPlayerLabel = "--"

// mediaStatusText is the _bar.ftl status vocabulary.
func mediaStatusText(state mpris.PlaybackState) string {
	switch state {
	case mpris.StatePlaying:
		return i18n.T("bar-media-playing")
	case mpris.StatePaused:
		return i18n.T("bar-media-paused")
	}
	return i18n.T("bar-media-stopped")
}

// mediaStatusGlyph maps the state onto its glyph.
func mediaStatusGlyph(state mpris.PlaybackState) string {
	switch state {
	case mpris.StatePlaying:
		return mediaPlayGlyph
	case mpris.StatePaused:
		return mediaPauseGlyph
	}
	return mediaStopGlyph
}

// mediaLabel is helpers.rs format_label: title, artist, album, status,
// and status_icon.
func mediaLabel(format string, p mpris.Player) string {
	return jinja.RenderOr(format, map[string]any{
		"title": p.Title, "artist": p.Artist, "album": p.Album,
		"status": mediaStatusText(p.State), "status_icon": mediaStatusGlyph(p.State),
	})
}

// mediaIconEnv is what the icon pick reads beyond the config: whether
// a theme icon exists and a desktop entry's Icon key. Tests fake both.
type mediaIconEnv struct {
	exists      func(name string) bool
	desktopIcon func(entry string) (string, bool)
}

// liveMediaIconEnv reads the real icon theme and XDG applications dirs.
func liveMediaIconEnv() mediaIconEnv {
	return mediaIconEnv{
		exists: widget.ThemeIconExists,
		desktopIcon: func(entry string) (string, bool) {
			return desktopentry.Icon(entry, desktopentry.ApplicationDirs())
		},
	}
}

// resolveMediaIcon is helpers.rs resolve_icon.
func resolveMediaIcon(cfg config.MediaConfig, p mpris.Player) string {
	entrySymbolic := func() string {
		if p.DesktopEntry != "" {
			return p.DesktopEntry + "-symbolic"
		}
		return cfg.IconName
	}
	switch cfg.IconType {
	case config.MediaIconTypeDefault:
		return cfg.IconName
	case config.MediaIconTypeApplication:
		return entrySymbolic()
	case config.MediaIconTypeSpinningDisc:
		return cfg.SpinningDiscIcon
	}
	for _, m := range cfg.PlayerIconMappings() {
		if glob.Wildcard(m.Pattern, p.BusName) {
			return m.Icon
		}
	}
	for _, m := range config.MediaBuiltinIcons {
		if glob.Wildcard(m.Pattern, p.BusName) {
			return m.Icon
		}
	}
	return entrySymbolic()
}

// mediaIconName is helpers.rs build_icon: application mode reads the
// desktop entry's icon; the other modes resolve and keep the result
// only when the theme has it, application-mapped trying the desktop
// entry next; icon-name is the last resort.
func mediaIconName(cfg config.MediaConfig, p mpris.Player, env mediaIconEnv) string {
	if cfg.IconType == config.MediaIconTypeApplication {
		if icon, ok := env.desktopIcon(p.DesktopEntry); ok {
			return icon
		}
		return cfg.IconName
	}
	if resolved := resolveMediaIcon(cfg, p); env.exists(resolved) {
		return resolved
	}
	if cfg.IconType == config.MediaIconTypeApplicationMapped {
		if icon, ok := env.desktopIcon(p.DesktopEntry); ok {
			return icon
		}
	}
	return cfg.IconName
}

// mediaModule is the now-playing button: the active player's icon and
// label, following the service.
type mediaModule struct {
	ctx    ModuleContext
	source mpris.Source
	env    mediaIconEnv
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
	// chrome is the module box around the button: the disc classes are
	// ancestor selectors (`.media-disc menubutton ... image`), so they
	// must sit above the menubutton, on the wrapper.
	chrome classer
}

func newMedia(ctx ModuleContext) (Module, error) {
	if ctx.Media == nil {
		return nil, errors.New("media: no MPRIS source available")
	}
	m := &mediaModule{ctx: ctx, source: ctx.Media, env: liveMediaIconEnv()}
	m.build()
	ticks, stop := ctx.Media.Subscribe()
	follow(ctx, ticks, stop, func(struct{}) { m.refresh() })
	return m, nil
}

// build assembles the icon+label tree and paints the first state.
func (m *mediaModule) build() {
	cfg := m.ctx.Config.Media
	m.label = widget.NewLabel(m.ctx.Font, m.ctx.Style.labelPx, "", m.ctx.Style.fg)
	m.icon = moduleIcon(m.ctx, cfg.Icon())
	m.root = assembleModule(m.ctx, m.icon, m.label)
	m.refresh()
}

// typeClass is the module box's type class, the ancestor the disc
// selectors hang from.
func (m *mediaModule) typeClass() string { return "media" }

// setChrome takes the module box appendModule wraps the button in;
// the disc classes re-apply now that the ancestor exists.
func (m *mediaModule) setChrome(c classer) {
	m.chrome = c
	m.refresh()
}

// refresh is update_cmd's PlayerChanged/MetadataChanged/
// PlaybackStateChanged: label, icon, and the disc classes.
func (m *mediaModule) refresh() {
	cfg := m.ctx.Config.Media
	p, ok := m.source.Active()
	label, icon := mediaNoPlayerLabel, cfg.IconName
	if ok {
		label, icon = mediaLabel(cfg.Format, p), mediaIconName(cfg, p, m.env)
	}
	if !cfg.LabelShow {
		label = ""
	}
	m.label.SetText(label)
	if m.icon != nil {
		m.icon.SetThemeName(icon)
	}
	setClass(m.chrome, "media-disc", ok && cfg.IconType == config.MediaIconTypeSpinningDisc)
	setClass(m.chrome, "media-spinning", ok && p.State == mpris.StatePlaying)
}

// classer is a widget carrying style classes (every gelm node).
type classer interface {
	AddClass(names ...string)
	RemoveClass(names ...string)
}

// setClass toggles one style class; a nil classer is a no-op.
func setClass(w classer, class string, on bool) {
	if w == nil {
		return
	}
	if on {
		w.AddClass(class)
		return
	}
	w.RemoveClass(class)
}

func (m *mediaModule) Root() widget.Widget { return m.root }
