// Package portal is the xdg-desktop-portal client side wayle needs: the
// Request/Response handshake and the ScreenCast session the recorder
// captures through (wayle-recorder portal.rs, which uses ashpd).
package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Portal bus identity.
const (
	BusName       = "org.freedesktop.portal.Desktop"
	ObjectPath    = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	RequestIface  = "org.freedesktop.portal.Request"
	SessionIface  = "org.freedesktop.portal.Session"
	responseSig   = RequestIface + ".Response"
	requestPrefix = "/org/freedesktop/portal/desktop/request/"
)

// Response codes of org.freedesktop.portal.Request.Response.
const (
	ResponseSuccess   uint32 = 0
	ResponseCancelled uint32 = 1
	ResponseOther     uint32 = 2
)

// ErrCancelled is a request the user dismissed.
var ErrCancelled = errors.New("portal: request cancelled")

// token returns a fresh handle_token: the spec allows [A-Za-z0-9_].
func token() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "wayle_" + hex.EncodeToString(b[:])
}

// requestPath is where the portal will put the Request object for a
// handle_token: the sender's unique name, ':' dropped and '.' as '_'.
func requestPath(conn *dbus.Conn, handle string) dbus.ObjectPath {
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	return dbus.ObjectPath(requestPrefix + sender + "/" + handle)
}

// call runs one request-style portal method: it subscribes to the
// Response signal on the predicted Request path before calling (the
// spec's race-free order), then waits for the response's results. A
// cancelled or failed response is an error; ctx cancellation closes the
// request.
func call(ctx context.Context, conn *dbus.Conn, method string, options map[string]dbus.Variant, args ...any) (map[string]dbus.Variant, error) {
	handle := token()
	options["handle_token"] = dbus.MakeVariant(handle)
	path := requestPath(conn, handle)
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(RequestIface), dbus.WithMatchMember("Response")}
	if err := conn.AddMatchSignal(match...); err != nil {
		return nil, fmt.Errorf("portal: %s: match: %w", method, err)
	}
	defer func() { _ = conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	var got dbus.ObjectPath
	callArgs := append(args, options)
	if err := conn.Object(BusName, ObjectPath).CallWithContext(ctx, method, 0, callArgs...).Store(&got); err != nil {
		return nil, fmt.Errorf("portal: %s: %w", method, err)
	}
	if got != path {
		// An old portal that ignores handle_token: follow its path.
		path = got
		_ = conn.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(RequestIface), dbus.WithMatchMember("Response"))
		defer func() {
			_ = conn.RemoveMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(RequestIface), dbus.WithMatchMember("Response"))
		}()
	}
	for {
		select {
		case <-ctx.Done():
			_ = conn.Object(BusName, path).Call(RequestIface+".Close", dbus.FlagNoReplyExpected).Err
			return nil, ctx.Err()
		case sig := <-signals:
			if sig == nil || sig.Path != path || sig.Name != responseSig || len(sig.Body) != 2 {
				continue
			}
			code, _ := sig.Body[0].(uint32)
			results, _ := sig.Body[1].(map[string]dbus.Variant)
			switch code {
			case ResponseSuccess:
				return results, nil
			case ResponseCancelled:
				return nil, fmt.Errorf("%s: %w", method, ErrCancelled)
			}
			return nil, fmt.Errorf("portal: %s: request failed (response %d)", method, code)
		}
	}
}
