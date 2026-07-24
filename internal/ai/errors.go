package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	pkgerrors "github.com/pkg/errors"
)

// ErrorCategory is a stable, safe provider failure classification.
type ErrorCategory string

const (
	ErrorConfiguration  ErrorCategory = "configuration"
	ErrorAuthentication ErrorCategory = "authentication"
	ErrorRateLimit      ErrorCategory = "rate_limit"
	ErrorTimeout        ErrorCategory = "timeout"
	ErrorUnavailable    ErrorCategory = "unavailable"
	ErrorMalformed      ErrorCategory = "malformed_response"
	// ErrorResponseTooLarge marks a provider response that exceeded the
	// enforced size limit.
	ErrorResponseTooLarge ErrorCategory = "response_too_large"
	ErrorInternal         ErrorCategory = "internal"
)

// ProviderError omits provider payloads and request content from its public message.
type ProviderError struct {
	Category ErrorCategory
	Message  string
	cause    error
}

func (e *ProviderError) Error() string { return e.Message }

func (e *ProviderError) Unwrap() error { return e.cause }

// NewProviderError creates a sanitized provider error.
func NewProviderError(category ErrorCategory, message string, cause error) error {
	return &ProviderError{Category: category, Message: message, cause: cause}
}

// CategoryOf returns the stable category for err.
func CategoryOf(err error) ErrorCategory {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Category
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	return ErrorInternal
}

// NormalizeHTTPError converts transport and status failures without including provider payloads.
func NormalizeHTTPError(err error, statusCode int) error {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return NewProviderError(ErrorTimeout, "AI provider request timed out", err)
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return NewProviderError(ErrorTimeout, "AI provider request timed out", err)
		}
		return NewProviderError(ErrorUnavailable, "AI provider is unavailable", err)
	}
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return NewProviderError(ErrorAuthentication, "AI provider authentication failed", nil)
	case statusCode == http.StatusTooManyRequests:
		return NewProviderError(ErrorRateLimit, "AI provider rate limit exceeded", nil)
	case statusCode >= 500:
		return NewProviderError(ErrorUnavailable, "AI provider is unavailable", nil)
	case statusCode >= 400:
		return NewProviderError(ErrorConfiguration, "AI provider rejected the request", nil)
	default:
		return nil
	}
}

// SafeMessage strips control characters from a fixed, non-provider message.
func SafeMessage(message string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, message)
}

var (
	// ErrProviderNotFound indicates that a requested provider ID does not exist.
	ErrProviderNotFound = pkgerrors.New("AI provider not found")
	// ErrCapabilityUnsupported indicates that the provider does not support the requested capability.
	ErrCapabilityUnsupported = pkgerrors.New("AI provider capability unsupported")
	// ErrSTTNotSupported indicates that the provider does not have a dedicated
	// speech-to-text endpoint. Use the audiollm package for multimodal audio
	// understanding when this is returned.
	ErrSTTNotSupported = pkgerrors.New("provider does not support speech-to-text capability")
	// ErrAudioLLMNotSupported indicates that the provider does not have a
	// multimodal-audio LLM available in this codebase.
	ErrAudioLLMNotSupported = pkgerrors.New("provider does not support multimodal audio capability")
)
