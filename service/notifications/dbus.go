package notifications

import (
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
