package core

import (
	"regexp"
	"strings"
)

// ErrorKind is a stable, provider-independent failure category.
type ErrorKind string

const (
	ErrorKindConfiguration  ErrorKind = "configuration"
	ErrorKindAuthentication ErrorKind = "authentication"
	ErrorKindProvider       ErrorKind = "provider"
	ErrorKindRateLimit      ErrorKind = "rate_limit"
	ErrorKindPermission     ErrorKind = "permission"
	ErrorKindTool           ErrorKind = "tool"
	ErrorKindMCP            ErrorKind = "mcp"
	ErrorKindLSP            ErrorKind = "lsp"
	ErrorKindPlatform       ErrorKind = "platform"
	ErrorKindCanceled       ErrorKind = "canceled"
	ErrorKindInternal       ErrorKind = "internal"
)

// Error carries a stable classification while retaining the internal cause.
// Cause is deliberately excluded from serialized events to avoid exposing
// provider responses, credentials, or implementation details.
type Error struct {
	Kind      ErrorKind `json:"kind"`
	Op        string    `json:"op,omitempty"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable,omitempty"`
	Cause     error     `json:"-"`
}

// Error returns the diagnostic form used for internal logs and wrapping.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	message := e.Message
	if message == "" {
		message = string(e.Kind)
	}
	if e.Op != "" {
		message = e.Op + ": " + message
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

// Unwrap exposes the internal causal chain to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|authorization|access[_-]?token|token|secret)(\s*[:=]\s*)([^\s,;]+)`)
	bearerPattern           = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	openAITokenPattern      = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{4,}\b`)
)

// UserMessage returns actionable context with common credential forms removed.
// It intentionally omits Cause because upstream errors are untrusted.
func (e *Error) UserMessage() string {
	if e == nil {
		return ""
	}

	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = defaultUserMessage(e.Kind)
	}
	message = secretAssignmentPattern.ReplaceAllString(message, "$1$2[REDACTED]")
	message = bearerPattern.ReplaceAllString(message, "Bearer [REDACTED]")
	return openAITokenPattern.ReplaceAllString(message, "[REDACTED]")
}

func defaultUserMessage(kind ErrorKind) string {
	switch kind {
	case ErrorKindConfiguration:
		return "configuration is invalid"
	case ErrorKindAuthentication:
		return "authentication failed"
	case ErrorKindRateLimit:
		return "request rate limit exceeded"
	case ErrorKindPermission:
		return "operation was not permitted"
	case ErrorKindCanceled:
		return "operation was canceled"
	default:
		return "operation failed"
	}
}
