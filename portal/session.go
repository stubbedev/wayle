package portal

import (
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// SessionIface is the object ScreenCast, RemoteDesktop, GlobalShortcuts
// and InputCapture put at a session handle in CreateSession; it lives
// until the app or the backend closes it.
const SessionIface = "org.freedesktop.impl.portal.Session"

// session is one live Session object: its cleanup (stop streams,
// release devices) runs exactly once, on the app's Close() or at
// shutdown.
type session struct {
	once     sync.Once
	onClose  func()
	unexport func()
}

// closeOnce runs the cleanup the first time; it reports whether this
// call ran it.
func (s *session) closeOnce() bool {
	ran := false
	s.once.Do(func() {
		ran = true
		s.onClose()
	})
	return ran
}

// sessions is the backend's registry of live sessions by path, so
// shutdown can run every cleanup without each interface exposing its
// own store (session.rs registry).
type sessions struct {
	conn *dbus.Conn
	mu   sync.Mutex
	live map[dbus.ObjectPath]*session
}

func newSessions(conn *dbus.Conn) *sessions {
	return &sessions{conn: conn, live: map[dbus.ObjectPath]*session{}}
}

// mount exports a Session at path; onClose runs exactly once, when the
// app closes it or the backend shuts down.
func (r *sessions) mount(path dbus.ObjectPath, onClose func()) error {
	s := &session{onClose: onClose}
	unexport, err := dbusx.Export(r.conn, path, dbusx.Interface{
		Name:       SessionIface,
		Methods:    sessionObject{r: r, path: path, s: s},
		Properties: dbusx.Getters{"version": func() any { return uint32(2) }},
	})
	if err != nil {
		return err
	}
	s.unexport = unexport
	r.mu.Lock()
	r.live[path] = s
	r.mu.Unlock()
	return nil
}

// clearAll runs the cleanup of every live session (process shutdown;
// the objects go with the connection).
func (r *sessions) clearAll() {
	r.mu.Lock()
	live := r.live
	r.live = map[dbus.ObjectPath]*session{}
	r.mu.Unlock()
	for _, s := range live {
		s.closeOnce()
	}
}

// sessionObject carries the Session's D-Bus methods.
type sessionObject struct {
	r    *sessions
	path dbus.ObjectPath
	s    *session
}

// Close runs the cleanup, emits Closed and unexports the session.
func (o sessionObject) Close() *dbus.Error {
	if !o.s.closeOnce() {
		return nil
	}
	o.r.mu.Lock()
	delete(o.r.live, o.path)
	o.r.mu.Unlock()
	_ = o.r.conn.Emit(o.path, SessionIface+".Closed")
	o.s.unexport()
	return nil
}

// store is per-session state keyed by the session path (session.rs
// SessionStore).
type store[T any] struct {
	mu sync.Mutex
	m  map[dbus.ObjectPath]T
}

func newStore[T any]() *store[T] { return &store[T]{m: map[dbus.ObjectPath]T{}} }

// set inserts or replaces the state for path.
func (s *store[T]) set(path dbus.ObjectPath, value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[path] = value
}

// get returns the state for path.
func (s *store[T]) get(path dbus.ObjectPath) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[path]
	return v, ok
}

// remove drops and returns the state for path.
func (s *store[T]) remove(path dbus.ObjectPath) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[path]
	delete(s.m, path)
	return v, ok
}

// update changes the state for path in place, when there is one.
func (s *store[T]) update(path dbus.ObjectPath, fn func(*T)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[path]; ok {
		fn(&v)
		s.m[path] = v
	}
}
