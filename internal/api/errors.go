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
	// Lock is the lock that refused the write; set only for a 423
	// RESOURCE_WRITE_LOCKED, from the problem's metadata.
	Lock *LockInfo
}

// CodeWriteLocked is the problem code of a write refused because the prompt,
// blueprint or artifact is locked (423, platform v0.15.0).
const CodeWriteLocked = "RESOURCE_WRITE_LOCKED"

// LockInfo is the lock named in a RESOURCE_WRITE_LOCKED problem's metadata.
type LockInfo struct {
	ResourceType string `json:"resource_type"`
	// Field is the frozen field the update tried to change; empty on a delete.
	Field    string `json:"field"`
	Comment  string `json:"comment"`
	LockedBy struct {
		DisplayName string `json:"display_name"`
	} `json:"locked_by"`
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
	if e.Lock != nil {
		e.Lock.writeTo(&b)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, "\n(request_id: %s)", e.RequestID)
	}
	return b.String()
}

// writeTo narrates the lock: who holds it and why, the frozen field an update
// tried to change, and how to release it.
func (l *LockInfo) writeTo(b *strings.Builder) {
	if l.LockedBy.DisplayName != "" {
		fmt.Fprintf(b, "\n  locked by: %s", l.LockedBy.DisplayName)
	}
	if l.Comment != "" {
		fmt.Fprintf(b, "\n  lock comment: %s", l.Comment)
	}
	if l.Field != "" {
		fmt.Fprintf(b, "\n  frozen field: %s", l.Field)
	}
	noun := l.ResourceType
	if noun == "" {
		noun = "<resource>"
	}
	fmt.Fprintf(b, "\n  unlock with: vibexp %s unlock <slug>", noun)
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
		if e.Code == CodeWriteLocked {
			e.Lock = decodeLock(body)
		}
	}
	if e.Detail == "" {
		e.Detail = http.StatusText(status)
	}
	return e
}

// decodeLock reads the lock from a RESOURCE_WRITE_LOCKED problem's metadata.
// A problem without one still gets the unlock hint, so it never returns nil.
func decodeLock(body []byte) *LockInfo {
	var p struct {
		Metadata LockInfo `json:"metadata"`
	}
	_ = json.Unmarshal(body, &p) // the body already decoded as a problem above
	return &p.Metadata
}
