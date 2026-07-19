package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

	id, err := Register(context.Background(), srv.Client(), srv.URL, "http://127.0.0.1:12345/callback")
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
}

func TestExchangeAndRefresh(t *testing.T) {
	srv := mockAS(t, &asState{})
	defer srv.Close()

	tok, err := ExchangeCode(context.Background(), srv.Client(), srv.URL+"/token",
		"client-1", "auth-code-1", "verifier-1", "http://127.0.0.1:1/callback", "https://api.example")
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
