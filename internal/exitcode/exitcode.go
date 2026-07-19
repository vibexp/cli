// Package exitcode defines the CLI's stable process exit codes and a typed
// error that carries the intended code, so the mapping lives in exactly one
// place (unwrapped once in cmd/vibexp/main.go).
package exitcode

import (
	"errors"
	"fmt"
)

// Stable exit codes. These are part of the CLI's scripting contract — do not
// renumber. Documented in docs/architecture.md.
const (
	OK         = 0 // success
	RuntimeErr = 1 // API or runtime error
	UsageErr   = 2 // invalid usage (bad flags/args)
	AuthErr    = 4 // authentication/authorization failure
)

// CodedError wraps an error with the process exit code it should produce.
type CodedError struct {
	Code int
	Err  error
}

func (e *CodedError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error to errors.Is / errors.As.
func (e *CodedError) Unwrap() error { return e.Err }

// New wraps err with the given exit code.
func New(code int, err error) *CodedError {
	return &CodedError{Code: code, Err: err}
}

// Newf wraps a formatted message with the given exit code.
func Newf(code int, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Err: fmt.Errorf(format, args...)}
}

// Usage is a convenience constructor for usage (exit 2) errors.
func Usage(format string, args ...any) *CodedError {
	return Newf(UsageErr, format, args...)
}

// Auth is a convenience constructor for auth (exit 4) errors.
func Auth(format string, args ...any) *CodedError {
	return Newf(AuthErr, format, args...)
}

// FromError resolves the process exit code for err:
//   - nil                 -> OK (0)
//   - wraps a CodedError  -> that code
//   - anything else       -> RuntimeErr (1)
func FromError(err error) int {
	if err == nil {
		return OK
	}
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return RuntimeErr
}
