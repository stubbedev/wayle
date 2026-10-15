package bar

import (
	"errors"
	"log"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
	"github.com/stubbedev/wayle/service/sway"
)

// windowTitleLabel is helpers.rs's format_label: the template over
// title/app, falling back to the _bar.ftl "Desktop" when the render is
// blank.
func windowTitleLabel(format, title, appID string) string {
	label := jinja.RenderOr(format, map[string]any{"title": title, "app": appID})
	if strings.TrimSpace(label) == "" {
		return i18n.T("bar-window-title-empty")
	}
	return label
}

// titlePrefix marks an icon mapping that matches the window title
// rather than its class.
const titlePrefix = "title:"

// windowTitleIcon is helpers.rs's resolve_icon: the user's title:
// mappings against the title, then the user's other mappings and the
// built-in ones against the app id, then icon-name.
func windowTitleIcon(cfg config.WindowTitleConfig, title, appID string) string {
	user := cfg.IconMappingsSorted()
	for _, m := range user {
		if pattern, ok := strings.CutPrefix(m.Pattern, titlePrefix); ok && glob.Wildcard(pattern, title) {
			return m.Icon
		}
	}
	for _, m := range user {
		if !strings.HasPrefix(m.Pattern, titlePrefix) && glob.Wildcard(m.Pattern, appID) {
			return m.Icon
		}
	}
	for _, m := range config.WindowTitleBuiltinIcons {
		if glob.Wildcard(m.Pattern, appID) {
			return m.Icon
		}
	}
	return cfg.IconName
}

// focusedWindow is sources/mod.rs's FocusedWindow: the focused window's
// title and app id, either possibly unset.
type focusedWindow struct {
	title, appID string
}

// focusSource is FocusedWindowSource: read is the focused window now
// (false while none has focus); each tick from subscribe is the cue to
// read again.
type focusSource struct {
	read      func() (focusedWindow, bool, error)
	subscribe func(ctx ModuleContext) (<-chan struct{}, func(), error)
	close     func()
}

// windowTitleSource is factory.rs's build_source: the compositor's
// source in Compositor::detect order (Hyprland, niri, mango, sway).
func windowTitleSource(ctx ModuleContext) (focusSource, error) {
	switch {
	case ctx.Hyprland != nil:
		return hyprlandFocusSource(ctx.Hyprland), nil
	case niri.IsRunning():
		conn, err := niri.Connect()
		if err != nil {
			return focusSource{}, err
		}
		return focusSource{
			read: func() (focusedWindow, bool, error) {
				windows, err := conn.Windows()
				if err != nil {
					return focusedWindow{}, false, err
				}
				for _, w := range windows {
					if !w.IsFocused {
						continue
					}
					var f focusedWindow
					if w.Title != nil {
						f.title = *w.Title
					}
					if w.AppID != nil {
						f.appID = *w.AppID
					}
					return f, true, nil
				}
				return focusedWindow{}, false, nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return niri.Subscribe() },
			close:     func() { _ = conn.Close() },
		}, nil
	case mango.IsRunning():
		w, err := mango.Watch()
		if err != nil {
			return focusSource{}, err
		}
		return focusSource{
			read: func() (focusedWindow, bool, error) {
				_, clients := w.State()
				for _, c := range clients {
					if c.IsFocused {
						return focusedWindow{title: c.Title, appID: c.AppID}, true, nil
					}
				}
				return focusedWindow{}, false, nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return w.Ticks(), nil, nil },
			close:     w.Close,
		}, nil
	case sway.IsRunning():
		conn, err := sway.Connect()
		if err != nil {
			return focusSource{}, err
		}
		return focusSource{
			read: func() (focusedWindow, bool, error) {
				windows, err := conn.Windows()
				if err != nil {
					return focusedWindow{}, false, err
				}
				for _, w := range windows {
					if w.Focused {
						return focusedWindow{title: w.Title, appID: w.AppID}, true, nil
					}
				}
				return focusedWindow{}, false, nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return sway.SubscribeTo("window") },
			close:     func() { _ = conn.Close() },
		}, nil
	}
	return focusSource{}, errors.New("window-title: unsupported compositor")
}

// hyprlandFocusSource reads the active window and re-reads on the
// active-window and title events.
func hyprlandFocusSource(conn *hyprland.Connection) focusSource {
	return focusSource{
		read: func() (focusedWindow, bool, error) {
			client, ok, err := conn.ActiveWindow()
			return focusedWindow{title: client.Title, appID: client.Class}, ok, err
		},
		subscribe: func(ctx ModuleContext) (<-chan struct{}, func(), error) {
			events, err := conn.Events(ctx.Life())
			if err != nil {
				return nil, nil, err
			}
			ticks := make(chan struct{}, 1)
			go func() {
				defer close(ticks)
				for event := range events {
					if event.Kind != hyprland.EventActiveWindow && event.Kind != hyprland.EventWindowTitleV2 {
						continue
					}
					select {
					case ticks <- struct{}{}:
					default:
					}
				}
			}()
			return ticks, nil, nil
		},
		close: func() {},
	}
}

// windowTitleModule is the module: the focused window's title and the
// icon its class or title maps to.
type windowTitleModule struct {
	ctx    ModuleContext
	source focusSource
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
	window focusedWindow
}

func newWindowTitle(ctx ModuleContext) (Module, error) {
	source, err := windowTitleSource(ctx)
	if err != nil {
		return nil, err
	}
	m := &windowTitleModule{ctx: ctx, source: source}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.WindowTitle.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		source.close()
		return nil, err
	}
	ticks, stop, err := source.subscribe(ctx)
	if err != nil {
		source.close()
		return nil, err
	}
	follow(ctx, ticks, func() {
		if stop != nil {
			stop()
		}
		source.close()
	}, func(struct{}) {
		if err := m.refresh(); err != nil {
			log.Printf("window-title: %v", err)
		}
	})
	return m, nil
}

// refresh reads the focused window (none reads as blank) and renders.
func (m *windowTitleModule) refresh() error {
	w, _, err := m.source.read()
	if err != nil {
		return err
	}
	m.window = w
	m.render()
	return nil
}

// render is update_display: the label and the resolved icon.
func (m *windowTitleModule) render() {
	cfg := m.ctx.Config.WindowTitle
	text := ""
	if cfg.LabelShow {
		text = windowTitleLabel(cfg.Format, m.window.title, m.window.appID)
	}
	m.label.SetText(text)
	if m.icon != nil {
		m.icon.SetThemeName(windowTitleIcon(cfg, m.window.title, m.window.appID))
	}
}

func (m *windowTitleModule) Root() widget.Widget { return m.root }
