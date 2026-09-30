package network

import (
	"context"

	"github.com/stubbedev/wayle/service/network/openconnect"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// SignIn is the agent's seam to the native openconnect sign-ins: an
// openconnect VPN's secrets are the result of a sign-in, not something
// a person can type, so the agent performs it rather than putting an
// un-fillable box on screen. NativeSignIn is package openconnect; tests
// substitute a fake. A nil SignIn leaves every openconnect request to
// another agent (NoSecrets).
type SignIn interface {
	// ProfileFrom recognises an openconnect profile in NM's connection
	// dictionary.
	ProfileFrom(conn ConnectionDict, uuid, name string) (openconnect.Profile, bool)
	// IsSupported reports whether wayle signs into this profile itself.
	IsSupported(p openconnect.Profile) bool
	// Authenticate signs in and returns the plugin's secrets.
	Authenticate(ctx context.Context, p openconnect.Profile, requestNew, unattended bool, prompter secrets.Prompter) (map[string]string, error)
	// Forget drops whatever is cached for a deleted profile.
	Forget(uuid string)
	// DeliverSSOCallback hands a globalprotectcallback: URI to a
	// waiting browser sign-in, reporting whether one was waiting.
	DeliverSSOCallback(uri string) bool
}

// NativeSignIn is the real sign-in, package openconnect.
var NativeSignIn SignIn = nativeSignIn{}

type nativeSignIn struct{}

func (nativeSignIn) ProfileFrom(conn ConnectionDict, uuid, name string) (openconnect.Profile, bool) {
	return openconnect.ProfileFrom(conn, uuid, name)
}

func (nativeSignIn) IsSupported(p openconnect.Profile) bool { return openconnect.IsSupported(p) }

func (nativeSignIn) Authenticate(ctx context.Context, p openconnect.Profile, requestNew, unattended bool, prompter secrets.Prompter) (map[string]string, error) {
	return openconnect.Authenticate(ctx, p, requestNew, unattended, prompter)
}

func (nativeSignIn) Forget(uuid string) { openconnect.Forget(uuid) }

func (nativeSignIn) DeliverSSOCallback(uri string) bool { return openconnect.DeliverSSOCallback(uri) }
