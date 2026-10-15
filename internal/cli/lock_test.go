package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A write refused by a lock exits 1 and its stderr, through the real root,
// narrates the lock from the 423 problem's metadata. Fabricated data.
func TestWriteLockedNarratedEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusLocked)
		_, _ = w.Write([]byte(`{"status":423,"code":"RESOURCE_WRITE_LOCKED","detail":"cannot delete: this prompt is locked by Ada: \"release agent\"",
			"request_id":"req-locked","metadata":{"resource_type":"prompt","comment":"release agent",
			"locked_by":{"user_id":"u-1","display_name":"Ada"}}}`))
	}))
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "prompt", "delete", "greet", "--yes")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%q", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	for _, want := range []string{"cannot delete", "locked by: Ada", "lock comment: release agent",
		"unlock with: vibexp prompt unlock <slug>", "request_id: req-locked"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}
