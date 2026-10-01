package weather

import (
	"errors"
	"fmt"
)

// ErrorKind is service.rs WeatherErrorKind: what the UI shows for a
// failed fetch, without implementation details.
type ErrorKind int

// Error kinds.
const (
	ErrOther ErrorKind = iota
	ErrAPIKeyMissing
	ErrLocationNotFound
	ErrNetwork
	ErrRateLimited
)

// Error is error.rs's Error: one failed geocode or fetch.
type Error struct {
	Kind ErrorKind
	// Provider names the failing endpoint ("open-meteo", "geocoding").
	Provider string
	// Query is the unresolved location of ErrLocationNotFound.
	Query string
	// Status is the HTTP status of a non-2xx reply, 0 otherwise.
	Status int
	msg    string
	cause  error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *Error) Unwrap() error { return e.cause }

// Retryable is error.rs is_retryable: transport failures, server
// errors, and rate limits are retried; parse, location, and key errors
// are not.
func (e *Error) Retryable() bool {
	switch e.Kind {
	case ErrNetwork, ErrRateLimited:
		return true
	case ErrOther:
		return e.Status >= 500
	}
	return false
}

func httpError(provider string, cause error) *Error {
	return &Error{Kind: ErrNetwork, Provider: provider, msg: "HTTP request to " + provider + " failed", cause: cause}
}

func statusError(provider string, status int) *Error {
	return &Error{Kind: ErrOther, Provider: provider, Status: status, msg: fmt.Sprintf("%s returned HTTP %d", provider, status)}
}

func parseError(provider, reason string) *Error {
	return &Error{Kind: ErrOther, Provider: provider, msg: "cannot parse " + provider + " response: " + reason}
}

func rateLimited(provider string) *Error {
	return &Error{Kind: ErrRateLimited, Provider: provider, msg: provider + " rate limit exceeded"}
}

func apiKeyMissing(provider string) *Error {
	return &Error{Kind: ErrAPIKeyMissing, Provider: provider, msg: provider + " requires an API key in config"}
}

func locationNotFound(query string) *Error {
	return &Error{Kind: ErrLocationNotFound, Query: query, msg: "location '" + query + "' not found"}
}

// kindOf is WeatherErrorKind::from: the UI category of any error.
func kindOf(err error) ErrorKind {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Kind
	}
	return ErrOther
}
