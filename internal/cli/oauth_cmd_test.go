package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

func TestBrowserLoginHeadlessExit4(t *testing.T) {
	cfg, cs := authFixture(t, "http://127.0.0.1:1") // base URL unused; headless trips first
	getenv := func(k string) string {
		if k == "VIBEXP_NO_BROWSER" {
			return "1"
		}
		return ""
	}
	_, errOut, code := runAuth(t, cfg, cs, getenv, "", "auth", "login")
	if code != exitcode.AuthErr {
		t.Fatalf("headless login exit = %d, want 4", code)
	}
	if !strings.Contains(errOut, "--with-api-key") {
		t.Errorf("expected API-key guidance, got %q", errOut)
	}
}

func TestBrowserLoginNoAuthServerExit4(t *testing.T) {
	// A deployment with no discovery endpoint -> guide to API keys.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	cfg, cs := authFixture(t, srv.URL)
	// Force non-headless (pretend a display exists) so discovery runs and 404s.
	getenv := func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	_, errOut, code := runAuth(t, cfg, cs, getenv, "", "auth", "login")
	if code != exitcode.AuthErr {
		t.Fatalf("no-auth-server login exit = %d, want 4", code)
	}
	if !strings.Contains(errOut, "--with-api-key") {
		t.Errorf("expected API-key guidance, got %q", errOut)
	}
}

// oauthDeployment is a mock deployment: OAuth discovery + a rotating token
// endpoint + a REST identity endpoint that accepts only the freshly-issued
// access token.
func oauthDeployment(t *testing.T) (*httptest.Server, *sync.Map) {
	t.Helper()
	valid := &sync.Map{}   // refresh token -> true
	current := &sync.Map{} // "access" -> latest issued access token
	valid.Store("seed-refresh", true)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base +
			`/authorize","token_endpoint":"` + base + `/token","registration_endpoint":"` + base + `/register"}`))
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rt := r.Form.Get("refresh_token")
		if _, ok := valid.Load(rt); !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		valid.Delete(rt)
		valid.Store("rotated-refresh", true)
		current.Store("access", "fresh-access")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":900}`))
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		want, _ := current.Load("access")
		if want != nil && r.Header.Get("Authorization") == "Bearer "+want.(string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"u1","email":"dev@example.com","name":"Dev","created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,"onboarding_completed":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"bad token","code":"unauthorized","request_id":"r1"}`))
	})
	return httptest.NewServer(mux), valid
}

func TestOAuthStatusTransparentRefresh(t *testing.T) {
	srv, _ := oauthDeployment(t)
	defer srv.Close()

	dir := t.TempDir()
	cfg := &config.Store{Path: filepath.Join(dir, "config.yaml")}
	_ = cfg.Save(&config.File{CurrentContext: "dev", Contexts: []config.Context{{Name: "dev", BaseURL: srv.URL}}})
	cs := &cred.Store{Path: filepath.Join(dir, "credentials.json")}
	// Expired OAuth session; refresh token is accepted by the mock.
	_ = cs.Save("dev", cred.Entry{
		Type: cred.TypeOAuth, ClientID: "client-1", AccessToken: "stale-access",
		RefreshToken: "seed-refresh", ExpiresAt: time.Now().Add(-time.Hour),
	})

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "auth", "status")
	if code != 0 {
		t.Fatalf("status exit = %d, want 0; stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "dev@example.com") || !strings.Contains(out, "browser login") {
		t.Errorf("status output wrong: %q", out)
	}
	// The rotated refresh token must be persisted.
	e, _ := cs.Get("dev")
	if e == nil || e.RefreshToken != "rotated-refresh" || e.AccessToken != "fresh-access" {
		t.Errorf("rotation not persisted: %+v", e)
	}
}
