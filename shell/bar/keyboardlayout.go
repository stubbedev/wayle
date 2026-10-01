package bar

import (
	"errors"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/hyprland"
)

// The keyboard-input source logic from keyboard_input/sources/hyprland.rs:
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

// keyboardLayout is the module: the main keyboard's active layout.
type keyboardLayout struct {
	ctx   ModuleContext
	conn  *hyprland.Connection
	label *widget.Label
}

func newKeyboardLayout(ctx ModuleContext) (Module, error) {
	if ctx.Hyprland == nil {
		return nil, errors.New("keyboard-input: the compositor is not Hyprland")
	}
	m := &keyboardLayout{ctx: ctx, conn: ctx.Hyprland}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	events, err := ctx.Hyprland.Events(ctx.Life())
	if err != nil {
		return nil, err
	}
	go func() {
		for range events {
			// The event payload is not trustworthy (per-device quirks);
			// re-read the devices like the Rust watcher does.
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
	}()
	return m, nil
}

// refresh re-queries the devices and updates the label.
func (m *keyboardLayout) refresh() error {
	devices, err := m.conn.Devices()
	if err != nil {
		return err
	}
	layout := mainKeyboardLayout(devices)
	cfg := m.ctx.Config.KeyboardInput
	label := ""
	if cfg.LabelShow && layout != "" {
		label = keyboardLayoutLabel(layout, cfg.Format, cfg.LayoutAliasMap)
	}
	m.label.SetText(label)
	return nil
}

func (m *keyboardLayout) Root() widget.Widget { return m.label }
