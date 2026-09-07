package authcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// Everything below is fabricated: this repository is public, so no staging URL,
// key or token may appear in a fixture.
const (
	fakeClientID = "dcr-client-fixture"
	fakeAccess   = "access-token-fixture"
	fakeRefresh  = "refresh-token-fixture"
	fakeContext  = "dev"
)

// loginAS is a scriptable deployment: RFC 8414 discovery, RFC 7591 DCR, the
// authorization and token endpoints, and the REST identity probe that
// runBrowserLogin makes after the flow. rejectScoped answers the first scoped
// authorization request with invalid_scope, which is what drives the retry;
// meAuth is the status the identity probe returns.
type loginAS struct {
	srv          *httptest.Server
	rejectScoped bool
	meStatus     int
	// grantOverride, when non-empty, is echoed as the token response's `scope`
	// regardless of what the authorization request asked for — a server that
	// applies its own default grant. Empty means RFC 6749 §5.1 behaviour: echo
	// back exactly the scope the request carried, omitting the member when it
	// carried none.
	grantOverride string

	mu        sync.Mutex
	lastScope string // scope the most recent authorization request carried
}

func newLoginAS(t *testing.T, rejectScoped bool, meStatus int) *loginAS {
	t.Helper()
	as := &loginAS{rejectScoped: rejectScoped, meStatus: meStatus}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                 as.srv.URL,
			"authorization_endpoint": as.srv.URL + "/authorize",
			"token_endpoint":         as.srv.URL + "/token",
			"registration_endpoint":  as.srv.URL + "/register",
			"scopes_supported":       []string{"mcp"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"client_id": fakeClientID})
	})
	// The injected opener drives the callback itself rather than following a
	// redirect, so this endpoint exists only so discovery advertises a real one.
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": "invalid_request"})
			return
		}
		body := map[string]any{
			"access_token": fakeAccess, "refresh_token": fakeRefresh,
			"token_type": "Bearer", "expires_in": 900,
		}
		// An omitted `scope` is RFC 6749 §5.1's "identical to the request".
		if granted := as.grantedScope(); granted != "" {
			body["scope"] = granted
		}
		writeJSON(w, body)
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, _ *http.Request) {
		if as.meStatus != http.StatusOK {
			w.WriteHeader(as.meStatus)
			w.Header().Set("Content-Type", "application/problem+json")
			writeJSON(w, map[string]any{"title": "unauthorized", "detail": "token not accepted", "request_id": "req-1"})
			return
		}
		writeJSON(w, map[string]any{
			"id": "u-1", "email": "dev@example.com", "name": "Dev User",
			"created_at": "2026-01-01T00:00:00Z", "is_instance_admin": false, "onboarding_completed": true,
		})
	})

	as.srv = httptest.NewServer(mux)
	t.Cleanup(as.srv.Close)
	return as
}

// grantedScope is what the token response should report as granted.
func (as *loginAS) grantedScope() string {
	if as.grantOverride != "" {
		return as.grantOverride
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.lastScope
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// scriptedOpener stands in for the real browser: it reads the authorization
// URL, and drives the loopback callback itself so the flow completes
// synchronously. A scoped request is bounced back as invalid_scope when the
// server is configured to reject one, which is what triggers the no-scope
// retry.
func scriptedOpener(t *testing.T, as *loginAS, hc *http.Client) func(string) error {
	t.Helper()
	return func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		q := u.Query()
		as.mu.Lock()
		as.lastScope = q.Get("scope")
		as.mu.Unlock()
		cb := q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state"))
		if as.rejectScoped && q.Get("scope") != "" {
			cb += "&error=invalid_scope&error_description=" + url.QueryEscape("scope not granted")
		} else {
			cb += "&code=auth-code-fixture"
		}
		go func() {
			resp, err := hc.Get(cb)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

// loginFixture wires a command whose context carries a runtime pointed at the
// fixture deployment, a temp-dir credential store, and captured out/err
// writers. It swaps the package's browser launcher for the scripted opener.
type loginFixture struct {
	cmd    *cobra.Command
	store  *cred.Store
	out    *bytes.Buffer
	errOut *bytes.Buffer
	as     *loginAS
}

func newLoginFixture(t *testing.T, rejectScoped bool, meStatus int) *loginFixture {
	t.Helper()
	return newLoginFixtureAS(t, newLoginAS(t, rejectScoped, meStatus))
}

func newLoginFixtureAS(t *testing.T, as *loginAS) *loginFixture {
	t.Helper()
	hc := as.srv.Client()

	prev := openBrowser
	openBrowser = func(rawURL string) error { return scriptedOpener(t, as, hc)(rawURL) }
	t.Cleanup(func() { openBrowser = prev })

	f := &loginFixture{
		as:     as,
		store:  &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	f.cmd = &cobra.Command{Use: "login"}
	f.cmd.SetOut(f.out)
	f.cmd.SetErr(f.errOut)
	f.cmd.SetContext(clictx.WithRuntime(context.Background(), &config.Runtime{
		ContextName: fakeContext, BaseURL: as.srv.URL, Timeout: 5 * time.Second,
	}))
	return f
}

// run drives runBrowserLogin with a getenv that reports a display server, so
// oauth.Headless does not short-circuit the flow on a headless CI runner.
func (f *loginFixture) run() error {
	return runBrowserLogin(f.cmd, func() (*cred.Store, error) { return f.store, nil },
		func(k string) string {
			if k == "DISPLAY" {
				return ":0"
			}
			return ""
		}, nil)
}

func (f *loginFixture) savedEntry(t *testing.T) *cred.Entry {
	t.Helper()
	e, err := f.store.Get(fakeContext)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	return e
}

// assertStdoutEmpty pins the output contract: stdout carries data only, so an
// interactive login writes nothing to it. Every notice belongs on stderr.
func assertStdoutEmpty(t *testing.T, f *loginFixture, wantOnStderr ...string) {
	t.Helper()
	if f.out.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", f.out.String())
	}
	for _, want := range wantOnStderr {
		if !strings.Contains(f.errOut.String(), want) {
			t.Errorf("stderr should carry %q, got %q", want, f.errOut.String())
		}
	}
}

func TestRunBrowserLoginHappyPathPersistsGrantedScopes(t *testing.T) {
	f := newLoginFixture(t, false, http.StatusOK)
	if err := f.run(); err != nil {
		t.Fatalf("runBrowserLogin: %v", err)
	}

	e := f.savedEntry(t)
	if e == nil {
		t.Fatal("no credential saved")
	}
	if e.Type != cred.TypeOAuth {
		t.Errorf("Type = %q, want %q", e.Type, cred.TypeOAuth)
	}
	if e.AccessToken != fakeAccess || e.RefreshToken != fakeRefresh {
		t.Errorf("tokens = %q/%q, want %q/%q", e.AccessToken, e.RefreshToken, fakeAccess, fakeRefresh)
	}
	if e.ClientID != fakeClientID {
		t.Errorf("ClientID = %q, want %q", e.ClientID, fakeClientID)
	}
	if e.ExpiresAt.IsZero() {
		t.Error("ExpiresAt must be set")
	}
	if len(e.Scopes) != 1 || e.Scopes[0] != "mcp" {
		t.Errorf("Scopes = %v, want [mcp] — the scopes the request actually carried", e.Scopes)
	}
	// The stored client is reusable next time, because its scopes cover mcp.
	if got := reusableClientID(f.store, fakeContext, []string{"mcp"}); got != fakeClientID {
		t.Errorf("reusableClientID = %q, want %q", got, fakeClientID)
	}
	assertStdoutEmpty(t, f, "Opening your browser", "Logged in to context")
}

func TestRunBrowserLoginRetryPersistsNoScopesAndForcesReregistration(t *testing.T) {
	f := newLoginFixture(t, true, http.StatusOK)
	if err := f.run(); err != nil {
		t.Fatalf("runBrowserLogin: %v", err)
	}

	e := f.savedEntry(t)
	if e == nil {
		t.Fatal("no credential saved")
	}
	// The scopes that actually worked — none, since only the no-scope retry
	// got through. Persisting the requested scopes here would be the bug.
	if e.Scopes != nil {
		t.Errorf("Scopes = %v, want nil after a no-scope retry", e.Scopes)
	}
	// Which is what makes the next login re-register rather than replay a
	// request this server is known to refuse.
	if got := reusableClientID(f.store, fakeContext, []string{"mcp"}); got != "" {
		t.Errorf("reusableClientID = %q, want %q so the next login re-registers", got, "")
	}
	assertStdoutEmpty(t, f, "rejected the requested scope", "Logged in to context")
}

func TestRunBrowserLoginRESTRejectionIsAuthErrorAndSavesNothing(t *testing.T) {
	f := newLoginFixture(t, false, http.StatusUnauthorized)
	err := f.run()
	if err == nil {
		t.Fatal("a REST 401 must fail the login")
	}
	var coded *exitcode.CodedError
	if !errors.As(err, &coded) || coded.Code != exitcode.AuthErr {
		t.Fatalf("want exit %d, got %#v", exitcode.AuthErr, err)
	}
	if !strings.Contains(err.Error(), "--with-api-key") {
		t.Errorf("error should guide to API keys: %v", err)
	}
	if e := f.savedEntry(t); e != nil {
		t.Errorf("no credential may be written when the token is rejected, got %+v", e)
	}
	assertStdoutEmpty(t, f)
}

// --- what gets persisted is the GRANT, not the request (issue #64) ---

func TestRunBrowserLoginPersistsNarrowedGrant(t *testing.T) {
	as := newLoginAS(t, false, http.StatusOK)
	// The request will carry "mcp"; the server grants strictly less.
	as.grantOverride = "openid"
	f := newLoginFixtureAS(t, as)
	if err := f.run(); err != nil {
		t.Fatalf("runBrowserLogin: %v", err)
	}

	e := f.savedEntry(t)
	if len(e.Scopes) != 1 || e.Scopes[0] != "openid" {
		t.Errorf("Scopes = %v, want [openid] — what the server granted, not what we asked for", e.Scopes)
	}
	// And the correction that follows: the stored client no longer covers mcp,
	// so the next login re-registers instead of replaying a narrowed grant.
	if got := reusableClientID(f.store, fakeContext, []string{"mcp"}); got != "" {
		t.Errorf("reusableClientID = %q, want %q — a narrowed grant must force re-registration", got, "")
	}
}

// A server that answers the no-scope retry with its own default grant has, in
// fact, granted that scope. Recording it — and therefore reusing the client
// next time — is the correction #64 asks for, not a regression of #65's
// re-registration behaviour, which still holds when the server echoes nothing.
func TestRunBrowserLoginRetryRecordsAServerDefaultGrant(t *testing.T) {
	as := newLoginAS(t, true, http.StatusOK)
	as.grantOverride = "mcp"
	f := newLoginFixtureAS(t, as)
	if err := f.run(); err != nil {
		t.Fatalf("runBrowserLogin: %v", err)
	}

	e := f.savedEntry(t)
	if len(e.Scopes) != 1 || e.Scopes[0] != "mcp" {
		t.Errorf("Scopes = %v, want [mcp] — the server's default grant is still a grant", e.Scopes)
	}
	if got := reusableClientID(f.store, fakeContext, []string{"mcp"}); got != fakeClientID {
		t.Errorf("reusableClientID = %q, want %q — the client did get mcp, so reuse is correct", got, fakeClientID)
	}
}
