package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

func TestCheckSuccessReturnsNil(t *testing.T) {
	if err := Check(200, []byte(`{"ok":true}`)); err != nil {
		t.Errorf("Check(200) = %v, want nil", err)
	}
	if err := Check(204, nil); err != nil {
		t.Errorf("Check(204) = %v, want nil", err)
	}
}

func TestCheckProblemWithValidationErrors(t *testing.T) {
	body := `{
		"code":"validation_failed","detail":"the request is invalid",
		"request_id":"req-99","status":422,
		"validation_errors":[
			{"field":"name","message":"is required","code":"required"},
			{"field":"email","message":"is malformed","code":"format"}
		]
	}`
	err := Check(422, []byte(body))
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("err type = %T, want *Error", err)
	}
	if apiErr.Detail != "the request is invalid" || apiErr.RequestID != "req-99" {
		t.Errorf("detail/request_id wrong: %+v", apiErr)
	}
	if len(apiErr.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(apiErr.Fields))
	}
	msg := apiErr.Error()
	for _, want := range []string{"the request is invalid", "name: is required", "email: is malformed", "request_id: req-99"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}

func TestCheckExitCodes(t *testing.T) {
	cases := map[int]int{
		401: exitcode.AuthErr,
		403: exitcode.AuthErr,
		404: exitcode.RuntimeErr,
		500: exitcode.RuntimeErr,
		429: exitcode.RuntimeErr,
	}
	for status, wantCode := range cases {
		err := Check(status, []byte(`{"detail":"x","request_id":"r"}`))
		if got := exitcode.FromError(err); got != wantCode {
			t.Errorf("status %d exit = %d, want %d", status, got, wantCode)
		}
	}
}

func TestCheckNonJSONBodyFallsBack(t *testing.T) {
	err := Check(502, []byte("<html>Bad Gateway</html>"))
	apiErr := err.(*Error)
	if apiErr.Detail != http.StatusText(http.StatusBadGateway) {
		t.Errorf("detail = %q, want %q", apiErr.Detail, http.StatusText(http.StatusBadGateway))
	}
}

// A 423 RESOURCE_WRITE_LOCKED is narrated from the problem's metadata: who
// holds the lock and why, the frozen field (updates only), and the unlock
// hint. Fabricated bodies in the platform's ResourceWriteLockedError shape.
func TestCheckWriteLockedNarratesTheLock(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    []string
		notWant []string
	}{
		{
			name: "update refused on a frozen field",
			body: `{"status":423,"code":"RESOURCE_WRITE_LOCKED","detail":"cannot change status: this artifact is locked by Ada: \"release agent\"",
				"request_id":"req-423","metadata":{"resource_type":"artifact","field":"status","comment":"release agent",
				"locked_by":{"user_id":"u-1","display_name":"Ada"}}}`,
			want: []string{"cannot change status", "locked by: Ada", "lock comment: release agent",
				"frozen field: status", "unlock with: vibexp artifact unlock <slug>", "request_id: req-423"},
		},
		{
			name: "delete refused: no field",
			body: `{"status":423,"code":"RESOURCE_WRITE_LOCKED","detail":"cannot delete: this prompt is locked by Ada",
				"request_id":"req-424","metadata":{"resource_type":"prompt","comment":"in use",
				"locked_by":{"user_id":"u-1","display_name":"Ada"}}}`,
			want:    []string{"cannot delete", "locked by: Ada", "lock comment: in use", "unlock with: vibexp prompt unlock <slug>", "request_id: req-424"},
			notWant: []string{"frozen field"},
		},
		{
			name:    "no metadata still gets the hint",
			body:    `{"status":423,"code":"RESOURCE_WRITE_LOCKED","detail":"locked"}`,
			want:    []string{"locked", "unlock with: vibexp <resource> unlock <slug>"},
			notWant: []string{"locked by:", "lock comment:", "frozen field"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(http.StatusLocked, []byte(tc.body))
			apiErr, ok := err.(*Error)
			if !ok || apiErr.Lock == nil {
				t.Fatalf("err = %#v, want *Error with Lock", err)
			}
			if apiErr.ExitCode() != exitcode.RuntimeErr {
				t.Errorf("exit = %d, want 1", apiErr.ExitCode())
			}
			msg := apiErr.Error()
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("message missing %q:\n%s", w, msg)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(msg, w) {
					t.Errorf("message has %q:\n%s", w, msg)
				}
			}
		})
	}
}

// Only RESOURCE_WRITE_LOCKED carries the lock; other errors, including the
// 409s lock/unlock answer, are unchanged.
func TestCheckOtherErrorsHaveNoLock(t *testing.T) {
	for _, body := range []string{
		`{"status":409,"code":"RESOURCE_LOCKED","detail":"already locked","metadata":{"resource_type":"prompt"}}`,
		`{"status":404,"code":"NOT_FOUND","detail":"not found"}`,
	} {
		err := Check(409, []byte(body)).(*Error)
		if err.Lock != nil || strings.Contains(err.Error(), "unlock with") {
			t.Errorf("%s: Lock = %+v, message %q", body, err.Lock, err.Error())
		}
	}
}
