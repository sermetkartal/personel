package hris

import (
	"errors"
	"fmt"
)

// ErrorKind categorizes errors from HRIS connectors so the sync orchestrator
// can decide whether to retry, back off, or page the DPO.
type ErrorKind int

const (
	// ErrorUnknown is the default — should be treated as transient but logged loudly.
	ErrorUnknown ErrorKind = iota
	// ErrorAuth indicates credentials are invalid or expired. Non-transient.
	// Pages the DPO immediately; do not retry until credentials are rotated.
	ErrorAuth
	// ErrorRateLimit indicates the upstream returned 429 or equivalent.
	// Exponential backoff with jitter; no page.
	ErrorRateLimit
	// ErrorNotFound indicates the requested record does not exist.
	ErrorNotFound
	// ErrorTransient is a network or 5xx error that will likely succeed on retry.
	ErrorTransient
	// ErrorPermanent is a 4xx error that will not succeed on retry.
	// Pages the DPO with full context.
	ErrorPermanent
	// ErrorContract indicates the upstream returned data that violates the
	// expected schema (e.g. missing required field). Non-transient.
	ErrorContract
)

// String returns a short identifier for logging + metrics.
func (k ErrorKind) String() string {
	switch k {
	case ErrorAuth:
		return "auth"
	case ErrorRateLimit:
		return "rate_limit"
	case ErrorNotFound:
		return "not_found"
	case ErrorTransient:
		return "transient"
	case ErrorPermanent:
		return "permanent"
	case ErrorContract:
		return "contract"
	default:
		return "unknown"
	}
}

// Error wraps an error with an ErrorKind and the connector name that produced it.
type Error struct {
	Kind      ErrorKind
	Connector string
	Cause     error
}

// Error returns a formatted message for logging.
func (e *Error) Error() string {
	return fmt.Sprintf("hris[%s]: %s: %v", e.Connector, e.Kind, e.Cause)
}

// Unwrap supports errors.Is / errors.As chains.
func (e *Error) Unwrap() error { return e.Cause }

// IsTransient reports whether the error is safe to retry.
func IsTransient(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Kind == ErrorTransient || e.Kind == ErrorRateLimit || e.Kind == ErrorUnknown
}

// IsAuth reports whether the error is credential-related.
func IsAuth(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Kind == ErrorAuth
}

// Wrap creates a new *Error with the given kind and cause.
func Wrap(connector string, kind ErrorKind, cause error) *Error {
	return &Error{Kind: kind, Connector: connector, Cause: cause}
}
