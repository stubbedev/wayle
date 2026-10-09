package bar

import (
	"errors"
	"log"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
	"github.com/stubbedev/wayle/service/sway"
)

// The Hyprland source logic from keyboard_input/sources/hyprland.rs:
// Hyprland names the throwaway keyboards that wtype and friends create
// for a single synthetic keystroke, and reports placeholder keymaps
// while a keyboard has no usable xkb state.
const (
	virtualKeyboardPrefix = "hl-virtual-keyboard-"
	// placeholder keymaps.
	noneKeymap  = "none"
	errorKeymap = "error"
)

// mainKeyboardLayout picks the active layout: the main non-virtual
// keyboard's keymap, else the first non-virtual keyboard with a real
// keymap. Empty string when no keyboard qualifies.
func mainKeyboardLayout(devices hyprland.Devices) string {
	fallback := ""
	for _, keyboard := range devices.Keyboards {
		if strings.HasPrefix(keyboard.Name, virtualKeyboardPrefix) {
			continue
		}
		if keyboard.Main && keyboard.ActiveKeymap != noneKeymap && keyboard.ActiveKeymap != errorKeymap {
			return keyboard.ActiveKeymap
		}
		if fallback == "" && keyboard.ActiveKeymap != noneKeymap && keyboard.ActiveKeymap != errorKeymap {
			fallback = keyboard.ActiveKeymap
		}
	}
	return fallback
}

// keyboardLayoutLabel renders the format with layout and alias (the
// alias falls back to the layout itself).
func keyboardLayoutLabel(layout, format string, aliasMap map[string]string) string {
	alias, ok := aliasMap[layout]
	if !ok {
		alias = layout
	}
	return jinja.RenderOr(format, map[string]any{"layout": layout, "alias": alias})
}

// layoutSource is sources/mod.rs's KeyboardLayoutSource: where the
// active layout comes from on this compositor. read re-queries it;
// each tick from subscribe is the cue to read again.
type layoutSource struct {
	read      func() (string, error)
	subscribe func(ctx ModuleContext) (<-chan struct{}, func(), error)
	close     func()
}

// keyboardLayoutSource is factory.rs's build_source: the compositor's
// source in Compositor::detect order (Hyprland, niri, mango, sway).
func keyboardLayoutSource(ctx ModuleContext) (layoutSource, error) {
	switch {
	case ctx.Hyprland != nil:
		return hyprlandLayoutSource(ctx.Hyprland), nil
	case niri.IsRunning():
		conn, err := niri.Connect()
		if err != nil {
			return layoutSource{}, err
		}
		return layoutSource{
			read: func() (string, error) {
				layouts, err := conn.KeyboardLayouts()
				if err != nil {
					return "", err
				}
				name, _ := layouts.Current()
				return name, nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return niri.Subscribe() },
			close:     func() { _ = conn.Close() },
		}, nil
	case mango.IsRunning():
		w, err := mango.Watch()
		if err != nil {
			return layoutSource{}, err
		}
		return layoutSource{
			read: func() (string, error) {
				monitors, _ := w.State()
				return mango.KeyboardLayout(monitors), nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return w.Ticks(), nil, nil },
			close:     w.Close,
		}, nil
	case sway.IsRunning():
		conn, err := sway.Connect()
		if err != nil {
			return layoutSource{}, err
		}
		return layoutSource{
			read: func() (string, error) {
				inputs, err := conn.Inputs()
				if err != nil {
					return "", err
				}
				return sway.KeyboardLayout(inputs), nil
			},
			subscribe: func(ModuleContext) (<-chan struct{}, func(), error) { return sway.SubscribeTo("input") },
			close:     func() { _ = conn.Close() },
		}, nil
	}
	return layoutSource{}, errors.New("keyboard-input: unsupported compositor")
}

// hyprlandLayoutSource reads hyprland's devices on every event: the
// event payload is not trustworthy (per-device quirks), so the Rust
// watcher re-reads too.
func hyprlandLayoutSource(conn *hyprland.Connection) layoutSource {
	return layoutSource{
		read: func() (string, error) {
			devices, err := conn.Devices()
			if err != nil {
				return "", err
			}
			return mainKeyboardLayout(devices), nil
		},
		subscribe: func(ctx ModuleContext) (<-chan struct{}, func(), error) {
			events, err := conn.Events(ctx.Life())
			if err != nil {
				return nil, nil, err
			}
			ticks := make(chan struct{}, 1)
			go func() {
				defer close(ticks)
				for range events {
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

// keyboardLayout is the module: the active keyboard layout.
type keyboardLayout struct {
	ctx    ModuleContext
	source layoutSource
	label  *widget.Label
}

func newKeyboardLayout(ctx ModuleContext) (Module, error) {
	source, err := keyboardLayoutSource(ctx)
	if err != nil {
		return nil, err
	}
	m := &keyboardLayout{ctx: ctx, source: source}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
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
			log.Printf("keyboard-input: %v", err)
		}
	})
	return m, nil
}

// refresh re-reads the layout and updates the label.
func (m *keyboardLayout) refresh() error {
	layout, err := m.source.read()
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.KeyboardInput
	label := ""
	if cfg.LabelShow && layout != "" {
		label = keyboardLayoutLabel(layout, cfg.Format, cfg.LayoutAliasMap)
	}
	m.label.SetText(label)
	return nil
}

func (m *keyboardLayout) Root() widget.Widget { return m.label }
