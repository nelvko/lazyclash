// Package diagnostic preserves a message's identity until it is presented.
package diagnostic

import (
	"errors"
	"fmt"
)

// Error has a stable message code and an English diagnostic representation.
// Args must obey the caller's existing credential redaction policy.
type Error struct {
	Code   string
	Format string
	Args   []any
	cause  error
}

func New(code, format string, args ...any) error {
	cause := errors.New(format)
	if len(args) > 0 {
		cause = fmt.Errorf(format, args...)
	}
	return &Error{Code: code, Format: format, Args: args, cause: cause}
}

func (e *Error) Error() string { return e.cause.Error() }
func (e *Error) Unwrap() error { return e.cause }
