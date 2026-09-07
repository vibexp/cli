package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if p.Method != "S256" {
		t.Errorf("method = %q, want S256", p.Method)
	}
	if len(p.Verifier) < 43 { // 32 bytes base64url unpadded = 43 chars
		t.Errorf("verifier too short: %d", len(p.Verifier))
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge != S256(verifier)")
	}

	// Two generations must differ (entropy).
	p2, _ := GeneratePKCE()
	if p.Verifier == p2.Verifier {
		t.Error("two PKCE verifiers were identical")
	}
}

func TestDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == discoveryPath {
			_, _ = w.Write([]byte(`{
				"issuer":"https://as.example",
				"authorization_endpoint":"https://as.example/authorize",
				"token_endpoint":"https://as.example/token",
				"registration_endpoint":"https://as.example/register",
				"scopes_supported":["mcp"]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	meta, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if meta.TokenEndpoint != "https://as.example/token" || meta.RegistrationEndpoint != "https://as.example/register" {
		t.Errorf("metadata wrong: %+v", meta)
	}
}

func TestDiscoverNoAuthServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != ErrNoAuthServer {
		t.Fatalf("err = %v, want ErrNoAuthServer", err)
	}
}

func TestDiscoverResourceFallsBackToBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	got := DiscoverResource(context.Background(), srv.Client(), srv.URL+"/")
	if got != srv.URL {
		t.Errorf("resource = %q, want trimmed base URL %q", got, srv.URL)
	}
}

func TestRegister(t *testing.T) {
	var gotReq registrationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(`{"client_id":"client-xyz"}`))
	}))
	defer srv.Close()

	id, err := Register(context.Background(), srv.Client(), srv.URL, "http://127.0.0.1:12345/callback", []string{"mcp", "openid"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id != "client-xyz" {
		t.Errorf("client_id = %q", id)
	}
	if gotReq.TokenEndpointAuthMethod != "none" {
		t.Errorf("auth method = %q, want none (public client)", gotReq.TokenEndpointAuthMethod)
	}
	if len(gotReq.RedirectURIs) != 1 || !strings.HasPrefix(gotReq.RedirectURIs[0], "http://127.0.0.1:") {
		t.Errorf("redirect URIs wrong: %v", gotReq.RedirectURIs)
	}
	if !contains(gotReq.GrantTypes, "refresh_token") {
		t.Errorf("grant_types missing refresh_token: %v", gotReq.GrantTypes)
	}
	// The requested scopes must be declared at registration (RFC 7591 scope) so
	// a scope-enforcing authorization server grants them to this client.
	if gotReq.Scope != "mcp openid" {
		t.Errorf("scope = %q, want %q", gotReq.Scope, "mcp openid")
	}
}

func TestRegisterOmitsEmptyScope(t *testing.T) {
	var gotReq registrationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(`{"client_id":"client-noscope"}`))
	}))
	defer srv.Close()

	if _, err := Register(context.Background(), srv.Client(), srv.URL, "http://127.0.0.1:12345/callback", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if gotReq.Scope != "" {
		t.Errorf("scope = %q, want empty (omitted when no scopes)", gotReq.Scope)
	}
}

func TestExchangeAndRefresh(t *testing.T) {
	srv := mockAS(t, &asState{})
	defer srv.Close()

	tok, err := ExchangeCode(context.Background(), srv.Client(), CodeExchange{
		TokenEndpoint: srv.URL + "/token", ClientID: "client-1", Code: "auth-code-1",
		Verifier: "verifier-1", RedirectURI: "http://127.0.0.1:1/callback",
		Resource: "https://api.example", RequestedScopes: []string{"mcp"},
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", tok)
	}
	if !tok.Expiry.After(time.Now()) {
		t.Errorf("expiry not in the future: %v", tok.Expiry)
	}

	refreshed, err := Refresh(context.Background(), srv.Client(), srv.URL+"/token",
		"client-1", tok.RefreshToken, "https://api.example")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.RefreshToken == tok.RefreshToken {
		t.Error("refresh token was not rotated")
	}
}

func TestRefreshInvalidGrant(t *testing.T) {
	srv := mockAS(t, &asState{})
	defer srv.Close()
	_, err := Refresh(context.Background(), srv.Client(), srv.URL+"/token", "client-1", "bogus-refresh", "")
	if err != ErrInvalidGrant {
		t.Fatalf("err = %v, want ErrInvalidGrant", err)
	}
}

func TestFlowEndToEnd(t *testing.T) {
	srv := mockAS(t, &asState{})
	defer srv.Close()

	lis, redirectURI, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	meta := &Metadata{
		AuthorizationEndpoint: srv.URL + "/authorize",
		TokenEndpoint:         srv.URL + "/token",
	}
	// Simulated browser: on open, drive the callback with a code + the state.
	opener := func(rawURL string) error {
		u, perr := url.Parse(rawURL)
		if perr != nil {
			return perr
		}
		q := u.Query()
		if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("authorize URL missing PKCE challenge: %s", rawURL)
		}
		cb := q.Get("redirect_uri") + "?code=auth-code-1&state=" + url.QueryEscape(q.Get("state"))
		go func() {
			resp, _ := srv.Client().Get(cb)
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	flow := &Flow{
		HTTPClient:  srv.Client(),
		Meta:        meta,
		ClientID:    "client-1",
		RedirectURI: redirectURI,
		Resource:    "https://api.example",
		Scopes:      []string{"mcp"},
		Listener:    lis,
		OpenBrowser: opener,
		Timeout:     5 * time.Second,
	}
	tok, err := flow.Run(context.Background())
	if err != nil {
		t.Fatalf("flow.Run: %v", err)
	}
	if tok.AccessToken == "" {
		t.Errorf("no access token from flow")
	}
}

// TestFlowScopeEnforcingAS is the end-to-end regression for issue #35: an
// authorization server that grants each client only the scopes it declared at
// registration (fosite exact-match / Ory Hydra / Keycloak style) must accept
// the CLI's authorization request because Register now declares those scopes.
// A client registered without the scope is rejected with invalid_scope — the
// exact failure the DCR fix removes.
// scopeEnforcingAS models a server that grants per-client scopes from the DCR
// declaration and refuses any scope a client did not declare. setGrant lets a
// test rewrite a client's allow-list. Its token endpoint omits `scope`, which
// RFC 6749 §5.1 defines as "identical to the request", so Token.Scopes here
// exercises the fallback; the echoing branch is covered by
// TestExchangeCodeRecordsGrantedScopes.
func scopeEnforcingAS(t *testing.T) (srv *httptest.Server, setGrant func(clientID string, scopes []string)) {
	t.Helper()
	var mu sync.Mutex
	granted := map[string][]string{} // client_id -> scopes granted at registration
	nextID := 0
	st := &asState{}

	mux := http.NewServeMux()
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var req registrationRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		nextID++
		id := fmt.Sprintf("client-%d", nextID)
		granted[id] = strings.Fields(req.Scope) // grant exactly what was declared
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"client_id":%q}`, id)
	})
	// /authorize enforces the per-client allow-list, then redirects back to the
	// loopback callback with either a code or error=invalid_scope.
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirect := q.Get("redirect_uri")
		state := url.QueryEscape(q.Get("state"))
		mu.Lock()
		allow := granted[q.Get("client_id")]
		mu.Unlock()
		for _, s := range strings.Fields(q.Get("scope")) {
			if !contains(allow, s) {
				http.Redirect(w, r, redirect+"?error=invalid_scope&error_description=client+not+allowed+scope&state="+state, http.StatusFound)
				return
			}
		}
		http.Redirect(w, r, redirect+"?code=auth-code-1&state="+state, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}
		access, refresh := st.issue()
		writeToken(w, access, refresh, "")
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(clientID string, scopes []string) {
		mu.Lock()
		defer mu.Unlock()
		granted[clientID] = scopes
	}
}

// followRedirectOpener is a simulated browser: it follows the /authorize
// redirect through to the loopback callback.
func followRedirectOpener(srv *httptest.Server) BrowserOpener {
	return func(rawURL string) error {
		go func() {
			resp, _ := srv.Client().Get(rawURL)
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestFlowScopeEnforcingAS(t *testing.T) {
	srv, setGrant := scopeEnforcingAS(t)

	meta := &Metadata{
		AuthorizationEndpoint: srv.URL + "/authorize",
		TokenEndpoint:         srv.URL + "/token",
		RegistrationEndpoint:  srv.URL + "/register",
		ScopesSupported:       []string{"mcp"},
	}

	// Declare the negotiated scope at registration, then run the flow.
	scopes := NegotiateScopes([]string{"mcp"}, meta.ScopesSupported, nil)
	clientID, err := Register(context.Background(), srv.Client(), meta.RegistrationEndpoint, "http://127.0.0.1:1/callback", scopes)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	lis, redirectURI, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	opener := followRedirectOpener(srv)

	flow := &Flow{
		HTTPClient:  srv.Client(),
		Meta:        meta,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Scopes:      scopes,
		Listener:    lis,
		OpenBrowser: opener,
		Timeout:     5 * time.Second,
	}
	tok, err := flow.Run(context.Background())
	if err != nil {
		t.Fatalf("flow.Run against scope-enforcing AS: %v", err)
	}
	if tok.AccessToken == "" {
		t.Errorf("no access token from flow")
	}

	if len(tok.Scopes) != 1 || tok.Scopes[0] != "mcp" {
		t.Errorf("Token.Scopes = %v, want [mcp] (the first attempt should have succeeded)", tok.Scopes)
	}

	// Control: a client registered without the scope is barred from it —
	// proving the server truly enforces per-client scopes and the DCR
	// declaration is why the flow above succeeded on its first attempt. Since
	// issue #37 the flow no longer dies there: the invalid_scope callback
	// triggers the one-shot no-scope retry, which this AS admits.
	setGrant("client-unscoped", nil)
	lis2, redirectURI2, _ := Listen()
	var notices []string
	flow2 := &Flow{
		HTTPClient:  srv.Client(),
		Meta:        meta,
		ClientID:    "client-unscoped",
		RedirectURI: redirectURI2,
		Scopes:      []string{"mcp"},
		Listener:    lis2,
		OpenBrowser: opener,
		Timeout:     5 * time.Second,
		Notify:      func(msg string) { notices = append(notices, msg) },
	}
	tok2, err := flow2.Run(context.Background())
	if err != nil {
		t.Fatalf("unscoped client: got err %v, want success via the no-scope retry", err)
	}
	// The retry sent no scope and the server echoed none back, so §5.1's
	// "identical to the request" fallback yields none granted.
	if tok2.Scopes != nil {
		t.Errorf("Token.Scopes = %v, want nil — the retry, not the first attempt, is what got through", tok2.Scopes)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "mcp") {
		t.Errorf("notices = %v, want one line naming the rejected scope", notices)
	}
}

// authzRecord captures one /authorize request so the retry tests can assert on
// what changed between attempts.
type authzRecord struct {
	scope         string
	state         string
	codeChallenge string
	redirectURI   string
}

// retryAS is an authorization server whose verdict on each /authorize is
// decided by reject: a non-empty return value is redirected back as that OAuth
// error code, an empty one issues an authorization code. Every hit is recorded.
func retryAS(t *testing.T, reject func(scope string) string) (*httptest.Server, func() []authzRecord) {
	t.Helper()
	var mu sync.Mutex
	var seen []authzRecord
	st := &asState{}

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		seen = append(seen, authzRecord{
			scope:         q.Get("scope"),
			state:         q.Get("state"),
			codeChallenge: q.Get("code_challenge"),
			redirectURI:   q.Get("redirect_uri"),
		})
		mu.Unlock()
		redirect := q.Get("redirect_uri")
		state := url.QueryEscape(q.Get("state"))
		if code := reject(q.Get("scope")); code != "" {
			http.Redirect(w, r, redirect+"?error="+code+"&error_description=scope+not+permitted+for+this+client&state="+state,
				http.StatusFound)
			return
		}
		http.Redirect(w, r, redirect+"?code=auth-code-1&state="+state, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") == "" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}
		access, refresh := st.issue()
		// Omits `scope`; see the note in TestFlowScopeEnforcingAS.
		writeToken(w, access, refresh, "")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []authzRecord {
		mu.Lock()
		defer mu.Unlock()
		return append([]authzRecord(nil), seen...)
	}
}

// retryFlow builds a Flow requesting "mcp" against srv, with a browser that
// simply follows the authorization URL (and its redirect back to the loopback
// callback). Notices are appended to notices.
func retryFlow(t *testing.T, srv *httptest.Server, notices *[]string) *Flow {
	t.Helper()
	lis, redirectURI, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return &Flow{
		HTTPClient: srv.Client(),
		Meta: &Metadata{
			AuthorizationEndpoint: srv.URL + "/authorize",
			TokenEndpoint:         srv.URL + "/token",
		},
		ClientID:    "client-1",
		RedirectURI: redirectURI,
		Scopes:      []string{"mcp"},
		Listener:    lis,
		Timeout:     5 * time.Second,
		Notify:      func(msg string) { *notices = append(*notices, msg) },
		OpenBrowser: func(rawURL string) error {
			go func() {
				resp, _ := srv.Client().Get(rawURL)
				if resp != nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
	}
}

// TestFlowRetriesWithoutScopeOnInvalidScope is the core of issue #37: an AS
// that refuses the requested scope but issues a code when none is asked for
// must yield a token on the second attempt, over the same listener.
func TestFlowRetriesWithoutScopeOnInvalidScope(t *testing.T) {
	srv, records := retryAS(t, func(scope string) string {
		if scope != "" {
			return "invalid_scope"
		}
		return ""
	})
	var notices []string
	flow := retryFlow(t, srv, &notices)

	tok, err := flow.Run(context.Background())
	if err != nil {
		t.Fatalf("flow.Run: %v", err)
	}
	if tok.AccessToken == "" {
		t.Error("no access token from the retry")
	}
	if tok.Scopes != nil {
		t.Errorf("Token.Scopes = %v, want nil (the no-scope retry succeeded and the server echoed no grant)", tok.Scopes)
	}

	got := records()
	if len(got) != 2 {
		t.Fatalf("/authorize hits = %d, want 2", len(got))
	}
	if got[0].scope != "mcp" {
		t.Errorf("first attempt scope = %q, want mcp", got[0].scope)
	}
	if got[1].scope != "" {
		t.Errorf("retry scope = %q, want it omitted", got[1].scope)
	}
	// The redirect URI is pinned by the dynamic client registration, so the
	// retry must not re-bind a port.
	if got[0].redirectURI != got[1].redirectURI || got[0].redirectURI != flow.RedirectURI {
		t.Errorf("redirect_uri drifted across attempts: %q then %q (flow: %q)",
			got[0].redirectURI, got[1].redirectURI, flow.RedirectURI)
	}
	if got[0].state == got[1].state {
		t.Error("retry reused the first attempt's state nonce")
	}
	if got[0].codeChallenge == got[1].codeChallenge {
		t.Error("retry reused the first attempt's PKCE challenge")
	}
	if len(notices) != 1 || !strings.Contains(notices[0], `"mcp"`) {
		t.Errorf("notices = %v, want one line naming the rejected scope", notices)
	}
}

// TestFlowFailsAfterOneRetry bounds the retry: an AS that refuses regardless
// must not send the user through a third browser open.
func TestFlowFailsAfterOneRetry(t *testing.T) {
	srv, records := retryAS(t, func(string) string { return "invalid_scope" })
	var notices []string
	flow := retryFlow(t, srv, &notices)

	_, err := flow.Run(context.Background())
	var authz *AuthzError
	if !errors.As(err, &authz) {
		t.Fatalf("err = %v (%T), want *AuthzError", err, err)
	}
	if authz.Code != "invalid_scope" {
		t.Errorf("code = %q, want invalid_scope", authz.Code)
	}
	if !strings.Contains(authz.Error(), "scope not permitted for this client") {
		t.Errorf("error %q does not surface the server's description", authz.Error())
	}
	if n := len(records()); n != 2 {
		t.Errorf("/authorize hits = %d, want 2 (one retry, not a loop)", n)
	}
}

// TestFlowNoRetryOnOtherErrors keeps the retry scoped to invalid_scope —
// access_denied means the user said no, and reopening the browser would be
// hostile.
func TestFlowNoRetryOnOtherErrors(t *testing.T) {
	srv, records := retryAS(t, func(string) string { return "access_denied" })
	var notices []string
	flow := retryFlow(t, srv, &notices)

	_, err := flow.Run(context.Background())
	var authz *AuthzError
	if !errors.As(err, &authz) {
		t.Fatalf("err = %v (%T), want *AuthzError", err, err)
	}
	if authz.Code != "access_denied" {
		t.Errorf("code = %q, want access_denied", authz.Code)
	}
	if n := len(records()); n != 1 {
		t.Errorf("/authorize hits = %d, want 1 (no retry)", n)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
}

// TestFlowNoRetryWithoutScopes guards the other half of the retry condition:
// with nothing to drop, a second attempt would be byte-identical to the first,
// so it must not cost the user another browser window.
func TestFlowNoRetryWithoutScopes(t *testing.T) {
	srv, records := retryAS(t, func(string) string { return "invalid_scope" })
	var notices []string
	flow := retryFlow(t, srv, &notices)
	flow.Scopes = nil

	if _, err := flow.Run(context.Background()); err == nil {
		t.Fatal("err = nil, want invalid_scope")
	}
	if n := len(records()); n != 1 {
		t.Errorf("/authorize hits = %d, want 1 (nothing to drop, so no retry)", n)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
}

// getAndClose issues a GET and hands back the response body, closing it.
func getAndClose(t *testing.T, c *http.Client, rawURL string) (int, string) {
	t.Helper()
	resp, err := c.Get(rawURL)
	if err != nil {
		return 0, ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// assertNeutralPage asserts a superseded callback got HTTP 200 and the
// "no longer active" page — not a 404 and not a "failed" message.
func assertNeutralPage(t *testing.T, status int, body string) {
	t.Helper()
	if status != http.StatusOK {
		t.Errorf("stale callback status = %d, want 200", status)
	}
	if !strings.Contains(body, "no longer active") {
		t.Errorf("stale callback should render the neutral page, got %q", body)
	}
}

// supersededOpener drives the retry scenario: the first browser open is bounced
// back by the AS as invalid_scope; on the retry it replays the first attempt's
// callback (staleQuery) before driving the good one, so the stale hit is
// guaranteed to land while the retry is waiting.
func supersededOpener(t *testing.T, srv *httptest.Server, staleQuery string) BrowserOpener {
	t.Helper()
	var firstState string
	return func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		if firstState == "" {
			firstState = u.Query().Get("state")
			go func() { _, _ = getAndClose(t, srv.Client(), rawURL) }()
			return nil
		}
		stale := u.Query().Get("redirect_uri") + staleQuery + url.QueryEscape(firstState)
		go func() {
			status, body := getAndClose(t, srv.Client(), stale)
			assertNeutralPage(t, status, body)
			_, _ = getAndClose(t, srv.Client(), rawURL)
		}()
		return nil
	}
}

// TestFlowRetryIgnoresSupersededCallback proves a replay of the first attempt's
// callback — the user reloading the stale browser tab while the retry is in
// flight — is ignored rather than aborting the retry. The stale tab is sitting
// on an error callback, so a reload replays that; the full URL with the code
// can be replayed too, and both must be inert.
func TestFlowRetryIgnoresSupersededCallback(t *testing.T) {
	for _, tc := range []struct{ name, staleQuery string }{
		{"code replay", "?code=stolen&state="},
		{"error replay", "?error=access_denied&state="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := retryAS(t, func(scope string) string {
				if scope != "" {
					return "invalid_scope"
				}
				return ""
			})
			var notices []string
			flow := retryFlow(t, srv, &notices)
			flow.OpenBrowser = supersededOpener(t, srv, tc.staleQuery)

			tok, err := flow.Run(context.Background())
			if err != nil {
				t.Fatalf("Run = %v, want the retry to complete despite the stale callback", err)
			}
			if tok == nil || tok.AccessToken == "" {
				t.Fatalf("no token: %+v", tok)
			}
			if tok.Scopes != nil {
				t.Errorf("Token.Scopes = %v, want nil after a no-scope retry", tok.Scopes)
			}
		})
	}
}

func TestFlowStateMismatch(t *testing.T) {
	srv := mockAS(t, &asState{})
	defer srv.Close()
	lis, redirectURI, _ := Listen()
	meta := &Metadata{AuthorizationEndpoint: srv.URL + "/authorize", TokenEndpoint: srv.URL + "/token"}
	opener := func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		// Wrong state -> CSRF guard must reject.
		cb := u.Query().Get("redirect_uri") + "?code=auth-code-1&state=WRONG"
		go func() {
			resp, _ := srv.Client().Get(cb)
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	flow := &Flow{HTTPClient: srv.Client(), Meta: meta, ClientID: "c", RedirectURI: redirectURI,
		Listener: lis, OpenBrowser: opener, Timeout: 3 * time.Second}
	if _, err := flow.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected state mismatch error, got %v", err)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// grantAS answers the token endpoint with grantedScope verbatim; an empty
// string omits the `scope` member entirely (RFC 6749 §5.1: "identical to the
// request"). Fabricated throughout.
func grantAS(t *testing.T, grantedScope string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		writeToken(w, "access-1", "refresh-1", grantedScope)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestExchangeCodeRecordsGrantedScopes pins the whole §5.1 contract: a present
// `scope` is authoritative even when it narrows the request, and an absent one
// means the request's own scopes.
func TestExchangeCodeRecordsGrantedScopes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		granted   string
		requested []string
		want      []string
	}{
		{"narrowed grant wins over the request", "mcp", []string{"mcp", "offline_access"}, []string{"mcp"}},
		{"echoed identical grant", "mcp", []string{"mcp"}, []string{"mcp"}},
		{"omitted scope falls back to the request", "", []string{"mcp"}, []string{"mcp"}},
		{"omitted scope on a no-scope request grants none", "", nil, nil},
		{"broadened grant is still recorded as granted", "mcp admin", []string{"mcp"}, []string{"mcp", "admin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := grantAS(t, tc.granted)
			tok, err := ExchangeCode(context.Background(), srv.Client(), CodeExchange{
				TokenEndpoint: srv.URL + "/token", ClientID: "client-1", Code: "auth-code-1",
				Verifier: "verifier-1", RedirectURI: "http://127.0.0.1:1/callback",
				RequestedScopes: tc.requested,
			})
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			if !reflect.DeepEqual(tok.Scopes, tc.want) {
				t.Errorf("Token.Scopes = %v, want %v", tok.Scopes, tc.want)
			}
		})
	}
}

// TestRefreshCarriesNoRequestedScopeFallback documents why the refresh path has
// no fallback: a refresh request sends no scope, so an omitted response `scope`
// has nothing to be "identical to". Callers keep the authorization grant's set.
func TestRefreshCarriesNoRequestedScopeFallback(t *testing.T) {
	srv := grantAS(t, "")
	tok, err := Refresh(context.Background(), srv.Client(), srv.URL+"/token", "client-1", "refresh-0", "")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.Scopes != nil {
		t.Errorf("Token.Scopes = %v, want nil on a refresh with no echoed scope", tok.Scopes)
	}

	srv2 := grantAS(t, "mcp")
	tok2, err := Refresh(context.Background(), srv2.Client(), srv2.URL+"/token", "client-1", "refresh-0", "")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(tok2.Scopes) != 1 || tok2.Scopes[0] != "mcp" {
		t.Errorf("Token.Scopes = %v, want [mcp] when the refresh response echoes one", tok2.Scopes)
	}
}
