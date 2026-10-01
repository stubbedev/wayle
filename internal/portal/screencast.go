package portal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
)

// ScreenCastIface is org.freedesktop.portal.ScreenCast.
const ScreenCastIface = "org.freedesktop.portal.ScreenCast"

// ScreenCast source types, cursor modes, and persist modes (the
// portal's bit values).
const (
	SourceMonitor          uint32 = 1
	CursorHidden           uint32 = 1
	CursorEmbedded         uint32 = 2
	PersistUntilRevoked    uint32 = 2
	restoreTokenFile              = "screencast.token"
	screenCastNoStreamsErr        = "screencast returned no streams"
)

// Stream is one negotiated ScreenCast stream.
type Stream struct {
	// Remote is the PipeWire remote the stream plays on; the caller
	// owns it and keeps it open for the capture's lifetime.
	Remote *os.File
	// Node is the PipeWire node id of the monitor.
	Node uint32
	// Width and Height are the stream size, zero when not reported.
	Width, Height int32
	// session is the portal Session object, closed by Close.
	session dbus.ObjectPath
	conn    *dbus.Conn
}

// Close ends the portal session and the PipeWire remote.
func (s *Stream) Close() {
	if s.conn != nil && s.session != "" {
		_ = s.conn.Object(BusName, s.session).Call(SessionIface+".Close", dbus.FlagNoReplyExpected).Err
	}
	if s.Remote != nil {
		_ = s.Remote.Close()
	}
}

// OpenScreenCast is open_screencast: one monitor, the cursor embedded
// or hidden, persisted until revoked with the restore token kept in
// stateDir (so the picker shows only on the first capture). stateDir
// "" keeps no token.
func OpenScreenCast(ctx context.Context, conn *dbus.Conn, showCursor bool, stateDir string) (*Stream, error) {
	created, err := call(ctx, conn, ScreenCastIface+".CreateSession", map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(token()),
	})
	if err != nil {
		return nil, err
	}
	session, err := sessionHandle(created)
	if err != nil {
		return nil, err
	}
	s := &Stream{session: session, conn: conn}
	fail := func(err error) (*Stream, error) {
		s.Close()
		return nil, err
	}
	cursor := CursorHidden
	if showCursor {
		cursor = CursorEmbedded
	}
	selectOpts := map[string]dbus.Variant{
		"types":        dbus.MakeVariant(SourceMonitor),
		"multiple":     dbus.MakeVariant(false),
		"cursor_mode":  dbus.MakeVariant(cursor),
		"persist_mode": dbus.MakeVariant(PersistUntilRevoked),
	}
	if tok := loadRestoreToken(stateDir); tok != "" {
		selectOpts["restore_token"] = dbus.MakeVariant(tok)
	}
	if _, selErr := call(ctx, conn, ScreenCastIface+".SelectSources", selectOpts, session); selErr != nil {
		return fail(selErr)
	}
	started, err := call(ctx, conn, ScreenCastIface+".Start", map[string]dbus.Variant{}, session, "")
	if err != nil {
		return fail(err)
	}
	if tok, ok := started["restore_token"].Value().(string); ok && tok != "" {
		saveRestoreToken(stateDir, tok)
	}
	if readErr := s.readStream(started); readErr != nil {
		return fail(readErr)
	}
	var fd dbus.UnixFD
	err = conn.Object(BusName, ObjectPath).CallWithContext(ctx, ScreenCastIface+".OpenPipeWireRemote", 0,
		session, map[string]dbus.Variant{}).Store(&fd)
	if err != nil {
		return fail(fmt.Errorf("portal: OpenPipeWireRemote: %w", err))
	}
	s.Remote = os.NewFile(uintptr(fd), "pipewire-remote")
	return s, nil
}

// sessionHandle reads CreateSession's session_handle (an object path,
// or a string on older portals).
func sessionHandle(results map[string]dbus.Variant) (dbus.ObjectPath, error) {
	switch v := results["session_handle"].Value().(type) {
	case dbus.ObjectPath:
		return v, nil
	case string:
		return dbus.ObjectPath(v), nil
	}
	return "", errors.New("portal: CreateSession returned no session handle")
}

// readStream takes the first stream of Start's a(ua{sv}) "streams".
func (s *Stream) readStream(results map[string]dbus.Variant) error {
	streams, ok := results["streams"].Value().([][]any)
	if !ok || len(streams) == 0 || len(streams[0]) != 2 {
		return errors.New("portal: " + screenCastNoStreamsErr)
	}
	node, ok := streams[0][0].(uint32)
	if !ok {
		return errors.New("portal: " + screenCastNoStreamsErr)
	}
	s.Node = node
	if props, ok := streams[0][1].(map[string]dbus.Variant); ok {
		if size, ok := props["size"].Value().([]any); ok && len(size) == 2 {
			s.Width, _ = size[0].(int32)
			s.Height, _ = size[1].(int32)
		}
	}
	return nil
}

// loadRestoreToken is load_restore_token.
func loadRestoreToken(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(stateDir, restoreTokenFile)) //nolint:gosec // wayle's own state dir
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// saveRestoreToken is save_restore_token: best effort.
func saveRestoreToken(stateDir, tok string) {
	if stateDir == "" {
		return
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(stateDir, restoreTokenFile), []byte(tok), 0o600)
}
