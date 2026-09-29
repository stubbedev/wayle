package notifications

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Server serves org.freedesktop.Notifications on the session bus.
type Server struct {
	conn *dbus.Conn
	svc  *Service
}

// Serve requests the well-known name and exports the object; the
// returned release function drops both.
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
	if err := conn.Export(server, ObjPath, ControlInterface); err != nil {
		return nil, fmt.Errorf("notifications: export control: %w", err)
	}
	reply, err := conn.RequestName(Interface, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("notifications: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("notifications: %s is already owned", Interface)
	}
	return server, nil
}

// Release drops the name.
func (s *Server) Release() error {
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

// ControlInterface is wayle's own control surface on the same object:
// the `wayle notify` CLI drives this.
const ControlInterface = "com.wayle.Notifications1"

// DismissAll empties the history.
func (s *Server) DismissAll() *dbus.Error {
	s.svc.DismissAll()
	return nil
}

// Dismiss removes one notification.
func (s *Server) Dismiss(id uint32) *dbus.Error {
	s.svc.Close(id, Dismissed)
	return nil
}

// SetDND flips do-not-disturb.
func (s *Server) SetDND(enabled bool) *dbus.Error {
	s.svc.SetDND(enabled)
	return nil
}

// ToggleDND flips do-not-disturb, returning the new state.
func (s *Server) ToggleDND() (bool, *dbus.Error) {
	return s.svc.ToggleDND(), nil
}

// List snapshots the history as (id, app, summary, body) rows.
func (s *Server) List() ([]ControlRow, *dbus.Error) {
	notifs := s.svc.Notifications()
	rows := make([]ControlRow, 0, len(notifs))
	for _, n := range notifs {
		rows = append(rows, ControlRow{n.ID, n.AppName, n.Summary, n.Body})
	}
	return rows, nil
}

// ControlRow is one List row.
type ControlRow struct {
	ID      uint32
	App     string
	Summary string
	Body    string
}

// DND reports the flag.
func (s *Server) DND() (bool, *dbus.Error) { return s.svc.DND(), nil }

// PopupDuration reports the configured popup duration; the timers use
// the notification's own expire_timeout, so this mirrors the config.
func (s *Server) PopupDuration() (uint32, *dbus.Error) { return 5000, nil }

// Count reports the history size.
func (s *Server) Count() (uint32, *dbus.Error) { return uint32(s.svc.Count()), nil }

// PopupCount reports the visible popups.
func (s *Server) PopupCount() (uint32, *dbus.Error) {
	return uint32(len(s.svc.Popups())), nil
}

// Client drives the daemon's control interface from the CLI.
type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Connect dials the session bus.
func Connect() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("notifications: session bus: %w", err)
	}
	return &Client{conn: conn, obj: conn.Object(Interface, ObjPath)}, nil
}

// Close drops the bus connection.
func (c *Client) Close() error { return c.conn.Close() }

// SetDND flips do-not-disturb.
func (c *Client) SetDND(ctx context.Context, enabled bool) error {
	return c.obj.CallWithContext(ctx, ControlInterface+".SetDND", 0, enabled).Err
}

// ToggleDND flips do-not-disturb, returning the new state.
func (c *Client) ToggleDND(ctx context.Context) (bool, error) {
	var on bool
	err := c.obj.CallWithContext(ctx, ControlInterface+".ToggleDND", 0).Store(&on)
	return on, err
}

// DismissAll empties the history.
func (c *Client) DismissAll(ctx context.Context) error {
	return c.obj.CallWithContext(ctx, ControlInterface+".DismissAll", 0).Err
}

// Dismiss removes one notification.
func (c *Client) Dismiss(ctx context.Context, id uint32) error {
	return c.obj.CallWithContext(ctx, ControlInterface+".Dismiss", 0, id).Err
}

// Row is one listed notification.
type Row struct {
	ID      uint32
	App     string
	Summary string
	Body    string
}

// List snapshots the history.
func (c *Client) List(ctx context.Context) ([]Row, error) {
	var rows []ControlRow
	if err := c.obj.CallWithContext(ctx, ControlInterface+".List", 0).Store(&rows); err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, Row(r))
	}
	return out, nil
}

// Status reads the daemon's counters.
func (c *Client) Status(ctx context.Context) (count, popups uint32, dnd bool, err error) {
	if err = c.obj.CallWithContext(ctx, ControlInterface+".Count", 0).Store(&count); err != nil {
		return
	}
	if err = c.obj.CallWithContext(ctx, ControlInterface+".PopupCount", 0).Store(&popups); err != nil {
		return
	}
	err = c.obj.CallWithContext(ctx, ControlInterface+".DND", 0).Store(&dnd)
	return
}
