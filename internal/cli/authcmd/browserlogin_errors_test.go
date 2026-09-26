package authcmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// newFailingLoginFixture wires a login fixture against a deployment whose
// discovery or registration endpoint answers with the given status. A zero
// status leaves that endpoint behaving normally.
func newFailingLoginFixture(t *testing.T, discoveryStatus, registerStatus int) *loginFixture {
	t.Helper()
	as := &loginAS{}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		if discoveryStatus != 0 {
			w.WriteHeader(discoveryStatus)
			return
		}
		writeJSON(w, map[string]any{
			"issuer":                 as.srv.URL,
			"authorization_endpoint": as.srv.URL + "/authorize",
			"token_endpoint":         as.srv.URL + "/token",
			"registration_endpoint":  as.srv.URL + "/register",
			"scopes_supported":       []string{"mcp"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(registerStatus)
	})
	as.srv = httptest.NewServer(mux)
	t.Cleanup(as.srv.Close)
	return newLoginFixtureAS(t, as)
}

// Each phase runBrowserLogin delegates to keeps its own error mapping: exit
// code and message are part of the login contract.
func TestRunBrowserLoginPhaseErrors(t *testing.T) {
	cases := []struct {
		name            string
		discoveryStatus int
		registerStatus  int
		wantCode        int
		wantMsg         string
	}{
		{"no oauth server", http.StatusNotFound, 0, exitcode.AuthErr, "no OAuth server"},
		{"discovery failure", http.StatusInternalServerError, 0, exitcode.RuntimeErr, "discovery"},
		{"registration failure", 0, http.StatusInternalServerError, exitcode.RuntimeErr, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFailingLoginFixture(t, tc.discoveryStatus, tc.registerStatus)
			err := f.run()
			var coded *exitcode.CodedError
			if !errors.As(err, &coded) || coded.Code != tc.wantCode {
				t.Fatalf("want exit %d, got %#v", tc.wantCode, err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q should mention %q", err, tc.wantMsg)
			}
			if e := f.savedEntry(t); e != nil {
				t.Errorf("no credential may be written on failure, got %+v", e)
			}
			assertStdoutEmpty(t, f)
		})
	}
}

func TestRunBrowserLoginSaveFailureIsRuntimeError(t *testing.T) {
	f := newLoginFixture(t, false, http.StatusOK)
	// A regular file where the store's directory should be makes Save fail.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.store.Path = filepath.Join(blocker, "credentials.json")

	err := f.run()
	var coded *exitcode.CodedError
	if !errors.As(err, &coded) || coded.Code != exitcode.RuntimeErr {
		t.Fatalf("want exit %d, got %#v", exitcode.RuntimeErr, err)
	}
	if strings.Contains(f.errOut.String(), "Logged in") {
		t.Errorf("a failed save must not report success, stderr: %q", f.errOut.String())
	}
}

func TestRunBrowserLoginReusesACoveringClient(t *testing.T) {
	f := newLoginFixture(t, false, http.StatusOK)
	const stored = "stored-client-fixture"
	if err := f.store.Save(fakeContext, cred.Entry{Type: cred.TypeOAuth, ClientID: stored, Scopes: []string{"mcp"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.run(); err != nil {
		t.Fatalf("login: %v", err)
	}
	if got := f.savedEntry(t).ClientID; got != stored {
		t.Errorf("a stored client covering the scopes must be reused, got client_id %q", got)
	}
}
