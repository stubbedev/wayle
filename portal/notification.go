package portal

import (
	"context"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/service/notifications"
)

// NotificationIface turns portal notifications into
// org.freedesktop.Notifications calls the shell's daemon shows, buttons
// included, and forwards the daemon's ActionInvoked back as the
// portal's (notification.rs).
const NotificationIface = "org.freedesktop.impl.portal.Notification"

// trackedNotification is what a daemon id maps back to.
type trackedNotification struct{ appID, id string }

// notifier is the interface's state: the daemon ids it created.
type notifier struct {
	conn    *dbus.Conn
	mu      sync.Mutex
	tracked map[uint32]trackedNotification
}

func newNotifier(conn *dbus.Conn) *notifier {
	return &notifier{conn: conn, tracked: map[uint32]trackedNotification{}}
}

func (n *notifier) iface() dbusx.Interface {
	return dbusx.Interface{
		Name:       NotificationIface,
		Methods:    notificationObject{n},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

func (n *notifier) daemon() dbus.BusObject {
	return n.conn.Object(notifications.Interface, notifications.ObjPath)
}

// forward re-emits the daemon's ActionInvoked for the notifications
// this portal created; the returned func stops it.
func (n *notifier) forward() (stop func(), err error) {
	match := []dbus.MatchOption{dbus.WithMatchSender(notifications.Interface), dbus.WithMatchInterface(notifications.Interface), dbus.WithMatchMember("ActionInvoked")}
	if err := n.conn.AddMatchSignal(match...); err != nil {
		return nil, err
	}
	signals := make(chan *dbus.Signal, 16)
	n.conn.Signal(signals)
	// godbus closes the channel itself when the connection goes, so the
	// loop ends on quit or that, and stop never closes it.
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			var s *dbus.Signal
			select {
			case <-quit:
				return
			case s = <-signals:
			}
			if s == nil {
				return
			}
			if s.Name != notifications.Interface+".ActionInvoked" || len(s.Body) != 2 {
				continue
			}
			id, _ := s.Body[0].(uint32)
			action, _ := s.Body[1].(string)
			n.mu.Lock()
			t, ok := n.tracked[id]
			n.mu.Unlock()
			if ok {
				_ = n.conn.Emit(ObjectPath, NotificationIface+".ActionInvoked", t.appID, t.id, action, []dbus.Variant{})
			}
		}
	}()
	return func() {
		_ = n.conn.RemoveMatchSignal(match...)
		n.conn.RemoveSignal(signals)
		close(quit)
		<-done
	}, nil
}

// notificationObject carries the interface's D-Bus methods.
type notificationObject struct{ n *notifier }

// AddNotification shows a notification: title (the app id without
// one), body, icon and the actions of default-action and buttons.
func (o notificationObject) AddNotification(appID, id string, notification Vardict) *dbus.Error {
	var daemonID uint32
	err := o.n.daemon().CallWithContext(context.Background(), notifications.Interface+".Notify", 0,
		appID, uint32(0), notificationIcon(notification), stringOr(notification, "title", appID),
		stringOr(notification, "body", ""), notificationActions(notification), map[string]dbus.Variant{}, int32(-1),
	).Store(&daemonID)
	if err != nil {
		warnf("notification: Notify failed: %v", err)
		return nil
	}
	o.n.mu.Lock()
	o.n.tracked[daemonID] = trackedNotification{appID, id}
	o.n.mu.Unlock()
	return nil
}

// RemoveNotification closes a notification this portal showed.
func (o notificationObject) RemoveNotification(appID, id string) *dbus.Error {
	o.n.mu.Lock()
	daemonID, found := uint32(0), false
	for d, t := range o.n.tracked {
		if t == (trackedNotification{appID, id}) {
			daemonID, found = d, true
			delete(o.n.tracked, d)
			break
		}
	}
	o.n.mu.Unlock()
	if found {
		if err := o.n.daemon().Call(notifications.Interface+".CloseNotification", 0, daemonID).Err; err != nil {
			warnf("notification: CloseNotification failed: %v", err)
		}
	}
	return nil
}

// notificationActions is the freedesktop [key, label, ...] list: the
// default-action first (labelled Default), then each button that has
// both an action and a label.
func notificationActions(notification Vardict) []string {
	actions := []string{}
	if def, ok := optString(notification, "default-action"); ok {
		actions = append(actions, def, "Default")
	}
	buttons, _ := notification["buttons"].Value().([]map[string]dbus.Variant)
	for _, b := range buttons {
		action, okA := optString(b, "action")
		label, okL := optString(b, "label")
		if okA && okL {
			actions = append(actions, action, label)
		}
	}
	return actions
}

// notificationIcon is a bare icon name, or the first name of a
// serialized themed GIcon ('themed', <['name', ...]>); anything else
// (a file or bytes icon) is no icon. The Rust match looked for the
// name array directly in the structure, missing the variant around
// it, so a themed icon never showed; this reads the variant.
func notificationIcon(notification Vardict) string {
	switch v := notification["icon"].Value().(type) {
	case string:
		return v
	case []any:
		if len(v) == 2 && v[0] == "themed" {
			if inner, ok := v[1].(dbus.Variant); ok {
				if names, ok := inner.Value().([]string); ok && len(names) > 0 {
					return names[0]
				}
			}
		}
	}
	return ""
}
