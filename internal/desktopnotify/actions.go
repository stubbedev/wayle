package desktopnotify

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// Action is one notification button: Key is what Ask reports, Label
// what the daemon shows.
type Action struct {
	Key   string
	Label string
}

// Ask posts a notification with buttons and waits for the user
// (notify.rs's notify_with_actions + NotifyReceipt::action): the key of
// the button pressed, or ok=false when the notification closed or
// expired without one, or ctx ended first. timeout is the daemon's
// expire timeout. The icon also goes in the image-path hint, as
// libnotify does, which wayle's own popup reads.
func (s *Sender) Ask(ctx context.Context, appName, summary, body, appIcon string, actions []Action, timeout time.Duration) (key string, ok bool, err error) {
	// Subscribe before sending, so the answer cannot race the id.
	match := []dbus.MatchOption{dbus.WithMatchInterface(busName), dbus.WithMatchObjectPath(path)}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return "", false, fmt.Errorf("desktopnotify: match signals: %w", err)
	}
	defer func() { _ = s.conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 16)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)

	flat := make([]string, 0, 2*len(actions))
	for _, a := range actions {
		flat = append(flat, a.Key, a.Label)
	}
	hints := map[string]dbus.Variant{}
	if appIcon != "" {
		hints["image-path"] = dbus.MakeVariant(appIcon)
	}
	var id uint32
	call := s.conn.Object(busName, path).CallWithContext(ctx, method, 0,
		appName, uint32(0), appIcon, summary, body, flat, hints, int32(timeout.Milliseconds()))
	if call.Err != nil {
		return "", false, fmt.Errorf("desktopnotify: Notify: %w", call.Err)
	}
	if err := call.Store(&id); err != nil {
		return "", false, fmt.Errorf("desktopnotify: Notify reply: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return "", false, nil
		case sig, open := <-signals:
			if !open {
				return "", false, nil
			}
			if len(sig.Body) == 0 {
				continue
			}
			if got, _ := sig.Body[0].(uint32); got != id {
				continue
			}
			switch sig.Name {
			case busName + ".ActionInvoked":
				if len(sig.Body) > 1 {
					if key, isString := sig.Body[1].(string); isString {
						return key, true, nil
					}
				}
			case busName + ".NotificationClosed":
				return "", false, nil
			}
		}
	}
}
