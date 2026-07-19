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
