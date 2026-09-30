package secrets

import (
	"errors"
	"fmt"
	"testing"
)

func TestDiscreditsPasswordOnlyOnRefusal(t *testing.T) {
	refused := fmt.Errorf("sign in: %w", &AuthenticationFailedError{Reason: "wrong password"})
	if !DiscreditsPassword(refused) {
		t.Error("a wrapped refusal must discredit the password")
	}
	for _, err := range []error{
		&SignInIncompleteError{Reason: "sign-in dismissed"},
		&ProtocolUnsupportedError{Reason: "unknown form"},
		errors.New("dial: connection refused"),
		nil,
	} {
		if DiscreditsPassword(err) {
			t.Errorf("DiscreditsPassword(%v) = true, want false", err)
		}
	}
}

func TestErrorsCarryTheReasonVerbatim(t *testing.T) {
	if got := (&AuthenticationFailedError{Reason: "account expired"}).Error(); got != "account expired" {
		t.Errorf("message = %q", got)
	}
}
