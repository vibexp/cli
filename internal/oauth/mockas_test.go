package oauth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// asState is a minimal stateful authorization server for tests: it issues
// rotating refresh tokens, invalidates used ones (reuse/rotation), and counts
// refreshes so concurrency tests can assert "exactly one".
type asState struct {
	mu           sync.Mutex
	counter      int
	refreshCount int
	validRefresh map[string]bool
}

func (s *asState) issue() (access, refresh string) {
	s.counter++
	access = fmt.Sprintf("access-%d", s.counter)
	refresh = fmt.Sprintf("refresh-%d", s.counter)
	if s.validRefresh == nil {
		s.validRefresh = map[string]bool{}
	}
	s.validRefresh[refresh] = true
	return access, refresh
}

// mockAS returns a test server exposing /token (authorization_code +
// refresh_token grants) and a permissive /authorize.
func mockAS(t *testing.T, st *asState) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		st.mu.Lock()
		defer st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
				return
			}
			access, refresh := st.issue()
			writeToken(w, access, refresh)
		case "refresh_token":
			rt := r.Form.Get("refresh_token")
			if !st.validRefresh[rt] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token reuse or expired"}`))
				return
			}
			delete(st.validRefresh, rt) // rotation: old token single-use
			st.refreshCount++
			access, refresh := st.issue()
			writeToken(w, access, refresh)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unsupported_grant_type"}`))
		}
	})
	return httptest.NewServer(mux)
}

func writeToken(w http.ResponseWriter, access, refresh string) {
	fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":900,"scope":"mcp"}`, access, refresh)
}
