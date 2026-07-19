package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	vibexp "github.com/vibexp/api-client-go"

	"github.com/vibexp/cli/internal/exitcode"
)

// FieldError is one field-level validation failure.
type FieldError struct {
	Field   string
	Message string
	Code    string
}

// Error is the single uniform CLI error produced from any non-2xx API response.
// It carries the RFC 7807 problem detail, field-level validation errors, and the
// request id (always surfaced for supportability).
type Error struct {
	Status    int
	Code      string
	Detail    string
	RequestID string
	Fields    []FieldError
}

// Error renders the problem for the user: detail, then any field errors, then
// the request id.
func (e *Error) Error() string {
	var b strings.Builder
	if e.Detail != "" {
		b.WriteString(e.Detail)
	} else {
		b.WriteString(http.StatusText(e.Status))
	}
	for _, f := range e.Fields {
		fmt.Fprintf(&b, "\n  - %s: %s", f.Field, f.Message)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, "\n(request_id: %s)", e.RequestID)
	}
	return b.String()
}

// ExitCode maps the HTTP status to a CLI exit code: 401/403 → auth (4),
// everything else → runtime (1). Satisfies exitcode.ExitCoder.
func (e *Error) ExitCode() int {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return exitcode.AuthErr
	}
	return exitcode.RuntimeErr
}

// Check turns any non-2xx response into an *Error, decoding the shared
// application/problem+json ErrorResponse body. 2xx returns nil. Non-7807 bodies
// (proxies, gateways) fall back to the HTTP status text.
func Check(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	e := &Error{Status: status}
	var er vibexp.ErrorResponse
	if err := json.Unmarshal(body, &er); err == nil {
		e.Code = er.Code
		e.Detail = er.Detail
		e.RequestID = er.RequestId
		if er.ValidationErrors != nil {
			for _, v := range *er.ValidationErrors {
				e.Fields = append(e.Fields, FieldError{Field: v.Field, Message: v.Message, Code: v.Code})
			}
		}
	}
	if e.Detail == "" {
		e.Detail = http.StatusText(status)
	}
	return e
}
