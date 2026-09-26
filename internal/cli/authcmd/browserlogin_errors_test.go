package authcmd

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// newFailingLoginFixture wires the standard login fixture, except that the
// discovery or registration endpoint answers with the given status. A zero
// status leaves that endpoint to the standard fixture.
func newFailingLoginFixture(t *testing.T, discoveryStatus, registerStatus int) *loginFixture {
	t.Helper()
	as := newLoginAS(t, false, http.StatusOK)
	next := as.srv.Config.Handler
	as.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/oauth-authorization-server" && discoveryStatus != 0:
			w.WriteHeader(discoveryStatus)
		case r.URL.Path == "/register" && registerStatus != 0:
			w.WriteHeader(registerStatus)
		default:
			next.ServeHTTP(w, r)
		}
	})
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
		{"registration failure", 0, http.StatusInternalServerError, exitcode.RuntimeErr, "dynamic client registration failed"},
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
