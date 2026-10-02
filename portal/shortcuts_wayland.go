package portal

import "github.com/stubbedev/gelm/app"

// waylandShortcuts registers on the compositor's
// hyprland-global-shortcuts-v1 through the portal's Wayland loop.
type waylandShortcuts struct {
	loop    *waylandLoop
	onEvent func(key string, pressed bool, sec uint64)
}

// waylandShortcutsStarter checks the protocol on loop; onEvent runs off
// the loop for each press and release of a registered key.
func waylandShortcutsStarter(loop *waylandLoop) func(func(string, bool, uint64)) (shortcutDevice, error) {
	return func(onEvent func(key string, pressed bool, sec uint64)) (shortcutDevice, error) {
		err := loop.do(func(a *app.Application) error {
			if !a.GlobalShortcutsAvailable() {
				return app.ErrGlobalShortcutsUnavailable
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return waylandShortcuts{loop, onEvent}, nil
	}
}

func (w waylandShortcuts) register(key, id, appID, description, trigger string) (func(), error) {
	var s *app.GlobalShortcut
	err := w.loop.do(func(a *app.Application) error {
		var err error
		s, err = a.RegisterGlobalShortcut(id, appID, description, trigger, func(pressed bool, sec uint64) {
			go w.onEvent(key, pressed, sec)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return func() { _ = w.loop.do(func(*app.Application) error { s.Destroy(); return nil }) }, nil
}
