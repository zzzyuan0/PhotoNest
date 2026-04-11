package ai

import (
	"errors"
	"strings"
)

type Boundary string

const (
	BoundaryRemoteService Boundary = "remote-service"
	BoundaryLocalSidecar  Boundary = "local-sidecar"
)

type ErrorKind string

const (
	ErrorKindTimeout              ErrorKind = "timeout"
	ErrorKindTransient            ErrorKind = "transient"
	ErrorKindRateLimited          ErrorKind = "rate_limited"
	ErrorKindPolicyBlocked        ErrorKind = "policy_blocked"
	ErrorKindProviderUnavailable  ErrorKind = "provider_unavailable"
	ErrorKindLocalSidecarFailed   ErrorKind = "local_sidecar_failed"
	ErrorKindUnclassifiedProvider ErrorKind = "unclassified_provider_error"
)

type ClassifiedError struct {
	Kind      ErrorKind
	Boundary  Boundary
	Message   string
	Retryable bool
	Temporary bool
	Cause     error
}

func (e ClassifiedError) Error() string {
	return e.Message
}

func (e ClassifiedError) Unwrap() error {
	return e.Cause
}

func ClassifyError(err error, boundary Boundary) ClassifiedError {
	if err == nil {
		return ClassifiedError{}
	}

	var classified ClassifiedError
	if errors.As(err, &classified) {
		return classified
	}

	message := err.Error()
	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "deadline"), strings.Contains(lower, "timeout"):
		return ClassifiedError{
			Kind:      ErrorKindTimeout,
			Boundary:  boundary,
			Message:   message,
			Retryable: true,
			Temporary: true,
			Cause:     err,
		}
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "too many requests"):
		return ClassifiedError{
			Kind:      ErrorKindRateLimited,
			Boundary:  boundary,
			Message:   message,
			Retryable: true,
			Temporary: true,
			Cause:     err,
		}
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "sidecar"):
		return ClassifiedError{
			Kind:      ErrorKindLocalSidecarFailed,
			Boundary:  boundary,
			Message:   message,
			Retryable: true,
			Temporary: true,
			Cause:     err,
		}
	case strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"):
		return ClassifiedError{
			Kind:      ErrorKindProviderUnavailable,
			Boundary:  boundary,
			Message:   message,
			Retryable: false,
			Temporary: false,
			Cause:     err,
		}
	default:
		return ClassifiedError{
			Kind:      ErrorKindUnclassifiedProvider,
			Boundary:  boundary,
			Message:   message,
			Retryable: true,
			Temporary: false,
			Cause:     err,
		}
	}
}
