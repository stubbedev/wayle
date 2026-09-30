// Package secrets holds the credential-prompt contract shared by the
// NetworkManager secret agent and the VPN sign-ins that run inside it,
// the Go counterpart of crates/wayle-network's types/agent.rs and the
// VPN variants of error.rs.
//
// It exists on its own so the sign-in protocols (package openconnect)
// and the agent (package network) depend on one definition of what a
// prompt is instead of on each other.
package secrets

import (
	"context"
	"errors"
)

// Field is one credential NM (or a gateway) is missing, and how to ask
// for it (SecretField).
type Field struct {
	// Key is the NM settings key this fills, e.g. "password", "psk".
	Key string
	// Label is the English label; the UI translates by key where it has
	// a string and falls back to this.
	Label string
	// Secret masks the input.
	Secret bool
}

// Request is a credential prompt waiting on the user (SecretRequest).
type Request struct {
	// UUID of the connection profile the secrets are for.
	UUID string
	// Name is the profile's display name, for the prompt header.
	Name string
	// Setting is the settings block to fill: "vpn",
	// "802-11-wireless-security", "802-1x", ...
	Setting string
	// Message is the service's own wording (a 2FA challenge), empty
	// when there is none.
	Message string
	// Fields is what to ask for.
	Fields []Field
}

// Prompter shows a Request and waits for the user. It returns the
// values by key, or ok=false when the user dismissed the prompt or ctx
// ended first (the request was withdrawn or timed out); the form goes
// away in both cases.
type Prompter interface {
	Prompt(ctx context.Context, req Request) (values map[string]string, ok bool)
}

// AuthenticationFailedError is a gateway refusing an authentication,
// or asking for something wayle cannot do (VpnAuthenticationFailed).
// Its message is shown to the user verbatim; it discredits a cached
// password.
type AuthenticationFailedError struct{ Reason string }

func (e *AuthenticationFailedError) Error() string { return e.Reason }

// SignInIncompleteError is a sign-in that ended without the gateway
// refusing the password: a dismissed prompt, an unreachable gateway, a
// rejected later factor (VpnSignInIncomplete). The cached password
// survives it.
type SignInIncompleteError struct{ Reason string }

func (e *SignInIncompleteError) Error() string { return e.Reason }

// ProtocolUnsupportedError is a gateway answering in a shape wayle's
// sign-in does not recognise (VpnProtocolUnsupported): the agent turns
// it into NM's NoSecrets so another agent or the plugin's own dialog
// can try.
type ProtocolUnsupportedError struct{ Reason string }

func (e *ProtocolUnsupportedError) Error() string { return e.Reason }

// DiscreditsPassword reports whether err is evidence against a stored
// password: only a refusal is (discredits_password).
func DiscreditsPassword(err error) bool {
	var refused *AuthenticationFailedError
	return errors.As(err, &refused)
}
