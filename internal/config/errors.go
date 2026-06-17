package config

import "fmt"

// ConfigError signals a configuration problem (missing/invalid file or schema).
// It maps to exit code 2 with the "Error: " prefix in the CLI.
type ConfigError struct{ Msg string }

func (e *ConfigError) Error() string { return e.Msg }

// NewConfigError builds a ConfigError.
func NewConfigError(format string, args ...any) error {
	return &ConfigError{Msg: fmt.Sprintf(format, args...)}
}

// AuthError signals a missing/invalid GitHub token. Maps to exit 2 with the
// "Auth error: " prefix.
type AuthError struct{ Msg string }

func (e *AuthError) Error() string { return e.Msg }

// NewAuthError builds an AuthError.
func NewAuthError(format string, args ...any) error {
	return &AuthError{Msg: fmt.Sprintf(format, args...)}
}

// RateLimitError signals GitHub rate-limit exhaustion. Maps to exit 2 with the
// raw message (no prefix).
type RateLimitError struct{ Msg string }

func (e *RateLimitError) Error() string { return e.Msg }

// NewRateLimitError builds a RateLimitError.
func NewRateLimitError(format string, args ...any) error {
	return &RateLimitError{Msg: fmt.Sprintf(format, args...)}
}
