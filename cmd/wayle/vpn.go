package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/shellipc"
)

// runVPN is `wayle vpn` (wayle/src/cli/vpn.rs): the browser's answer to
// a GlobalProtect SAML sign-in. Not something to run by hand: wayle
// registers a desktop entry for the globalprotectcallback: scheme
// pointing at `wayle vpn sso-callback`, so the identity provider's
// final redirect reaches the sign-in waiting inside the shell.
func runVPN(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("vpn needs a command: sso-callback")
	}
	switch args[0] {
	case "sso-callback":
		if len(args) != 2 {
			return errors.New("sso-callback needs the globalprotectcallback: URI the browser was sent to")
		}
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return fmt.Errorf("D-Bus session unavailable: %w", err)
		}
		defer func() { _ = conn.Close() }()
		return ssoCallback(conn, args[1], out)
	default:
		return fmt.Errorf("unknown vpn command %q", args[0])
	}
}

// ssoCallback forwards a globalprotectcallback: URI to the running
// shell. It fails when the shell is not running or no sign-in was
// waiting for a callback.
func ssoCallback(conn *dbus.Conn, uri string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shellipc.VPNSSOCallback(ctx, conn, uri); err != nil {
		return fmt.Errorf("VPN sign-in callback failed: %w", err)
	}
	fmt.Fprintln(out, "Sign-in completed; you can close the browser tab.")
	return nil
}
