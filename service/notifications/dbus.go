package notifications

import (
	"fmt"
	"sync/atomic"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Server serves org.freedesktop.Notifications on the session bus and
// wayle's com.wayle.Notifications1 extension beside it.
type Server struct {
	conn        *dbus.Conn
	svc         *Service
	releaseExts func()
}

// Serve requests the well-known names and exports the objects; Release
// drops them.
func Serve(conn *dbus.Conn, svc *Service) (*Server, error) {
	svc.SetEmitter(func(signal string, args ...any) {
		if err := conn.Emit(ObjPath, signal, args...); err != nil {
			return
		}
	})
	server := &Server{conn: conn, svc: svc}
	if err := conn.Export(server, ObjPath, Interface); err != nil {
		return nil, fmt.Errorf("notifications: export: %w", err)
	}
	reply, err := conn.RequestName(Interface, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("notifications: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("notifications: %s is already owned", Interface)
	}
	release, err := ServeWayle(conn, svc)
	if err != nil {
		_, _ = conn.ReleaseName(Interface)
		return nil, err
	}
	server.releaseExts = release
	return server, nil
}

// Release drops the names.
func (s *Server) Release() error {
	if s.releaseExts != nil {
		s.releaseExts()
	}
	_, err := s.conn.ReleaseName(Interface)
	return err
}

// Notify handles the spec's Notify call.
func (s *Server) Notify(appName string, replacesID uint32, appIcon, summary, body string, actions []string, hints map[string]dbus.Variant, expireTimeout int32) (uint32, *dbus.Error) {
	return s.svc.Notify(appName, replacesID, appIcon, summary, body, actions, expireTimeout), nil
}

// CloseNotification handles the spec's CloseNotification call; an
// unknown id is not an error (the spec leaves it to the server).
func (s *Server) CloseNotification(id uint32) *dbus.Error {
	s.svc.Close(id, ClosedCall)
	return nil
}

// GetCapabilities handles the spec's GetCapabilities call.
func (s *Server) GetCapabilities() ([]string, *dbus.Error) {
	return Capabilities, nil
}

// GetServerInformation handles the spec's information call.
func (s *Server) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return ServerName, ServerVendor, ServerVersion, SpecVersion, nil
}

// wayle's control extension, which `wayle notify` drives
// (wayle-notification/src/wayle_daemon.rs).
const (
	WayleName = "com.wayle.Notifications1"
	WaylePath = dbus.ObjectPath("/com/wayle/Notifications")
)

// defaultPopupDuration is the service builder's popup_duration.
const defaultPopupDuration = 5000

// wayleDaemon is the com.wayle.Notifications1 object (WayleDaemon).
type wayleDaemon struct {
	svc           *Service
	popupDuration atomic.Uint32
}

// DismissAll empties the history.
func (d *wayleDaemon) DismissAll() *dbus.Error {
	d.svc.DismissAll()
	return nil
}

// Dismiss removes one notification as dismissed by the user.
func (d *wayleDaemon) Dismiss(id uint32) *dbus.Error {
	d.svc.Close(id, Dismissed)
	return nil
}

// SetDnd sets do-not-disturb.
func (d *wayleDaemon) SetDnd(enabled bool) *dbus.Error {
	d.svc.SetDND(enabled)
	return nil
}

// ToggleDnd flips do-not-disturb.
func (d *wayleDaemon) ToggleDnd() *dbus.Error {
	d.svc.ToggleDND()
	return nil
}

// SetPopupDuration stores the popup duration the property reports.
func (d *wayleDaemon) SetPopupDuration(ms uint32) *dbus.Error {
	d.popupDuration.Store(ms)
	return nil
}

// ListRow is one List row: (id, app, summary, body).
type ListRow struct {
	ID      uint32
	App     string
	Summary string
	Body    string
}

// List snapshots the history.
func (d *wayleDaemon) List() ([]ListRow, *dbus.Error) {
	notifs := d.svc.Notifications()
	rows := make([]ListRow, 0, len(notifs))
	for _, n := range notifs {
		rows = append(rows, ListRow{n.ID, n.AppName, n.Summary, n.Body})
	}
	return rows, nil
}

// ServeWayle exports com.wayle.Notifications1 over svc.
func ServeWayle(conn *dbus.Conn, svc *Service) (func(), error) {
	d := &wayleDaemon{svc: svc}
	d.popupDuration.Store(defaultPopupDuration)
	return dbusx.Serve(conn, dbusx.Service{
		Name:      WayleName,
		Path:      WaylePath,
		Interface: WayleName,
		Methods:   d,
		Properties: dbusx.Getters{
			"Dnd":           func() any { return svc.DND() },
			"PopupDuration": func() any { return d.popupDuration.Load() },
			"Count":         func() any { return uint32(svc.Count()) },
			"PopupCount":    func() any { return uint32(len(svc.Popups())) },
		},
	})
}
