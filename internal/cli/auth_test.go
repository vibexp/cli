package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

const goodKey = "vxk_good_key_abcdef123456"

// meServer emulates GET /api/v1/auth/me: 200 for the good bearer, else a 401
// RFC 7807 problem document.
func meServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "Bearer "+goodKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id":"user-123","email":"dev@example.com","name":"Dev User",
				"created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,
				"onboarding_completed":true
			}`))
			return
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{
			"type":"about:blank","title":"Unauthorized","status":401,
			"detail":"invalid API key","code":"unauthorized",
			"request_id":"req-abc","timestamp":"2026-01-01T00:00:00Z"
		}`))
	}))
}

// authFixture wires a config store with a "dev" context pointed at server plus
// an isolated credential store.
func authFixture(t *testing.T, serverURL string) (*config.Store, *cred.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Store{Path: filepath.Join(dir, "config.yaml")}
	if err := cfg.Save(&config.File{
		CurrentContext: "dev",
		Contexts:       []config.Context{{Name: "dev", BaseURL: serverURL}},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return cfg, &cred.Store{Path: filepath.Join(dir, "credentials.json")}
}

// runAuth executes the command tree with the given stdin, returning
// stdout/stderr and exit code.
func runAuth(t *testing.T, cfg *config.Store, cs *cred.Store, getenv config.Getenv, stdin string, args ...string) (string, string, int) {
	t.Helper()
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	logDir := t.TempDir()
	root := NewRootCommand(Options{Store: cfg, CredStore: cs, Getenv: getenv, LogDir: logDir})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	// Mirror Execute(): a returned error is surfaced on stderr.
	if err != nil {
		errBuf.WriteString("Error: " + err.Error())
	}
	return out.String(), errBuf.String(), exitcode.FromError(err)
}

func TestAuthLoginStatusLogout(t *testing.T) {
	srv := meServer(t)
	defer srv.Close()
	cfg, cs := authFixture(t, srv.URL)

	// login with a valid key piped on stdin.
	_, errOut, code := runAuth(t, cfg, cs, nil, goodKey+"\n", "auth", "login", "--with-api-key")
	if code != 0 {
		t.Fatalf("login exit = %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "dev@example.com") {
		t.Errorf("login should confirm identity, got %q", errOut)
	}
	// key persisted.
	if e, _ := cs.Get("dev"); e == nil || e.APIKey != goodKey {
		t.Fatalf("key not persisted: %+v", e)
	}

	// status shows identity + fingerprint, never the raw key.
	out, _, code := runAuth(t, cfg, cs, nil, "", "auth", "status")
	if code != 0 {
		t.Fatalf("status exit = %d", code)
	}
	if !strings.Contains(out, "dev@example.com") || !strings.Contains(out, cred.Fingerprint(goodKey)) {
		t.Errorf("status missing identity/fingerprint: %q", out)
	}
	if strings.Contains(out, goodKey) {
		t.Errorf("status leaked the raw key: %q", out)
	}

	// logout removes the entry.
	_, _, code = runAuth(t, cfg, cs, nil, "", "auth", "logout")
	if code != 0 {
		t.Fatalf("logout exit = %d", code)
	}
	if e, _ := cs.Get("dev"); e != nil {
		t.Errorf("logout did not remove entry: %+v", e)
	}
}

func TestAuthLoginInvalidKeyExit4(t *testing.T) {
	srv := meServer(t)
	defer srv.Close()
	cfg, cs := authFixture(t, srv.URL)

	_, errOut, code := runAuth(t, cfg, cs, nil, "wrong_key_abcdef\n", "auth", "login", "--with-api-key")
	if code != exitcode.AuthErr {
		t.Fatalf("invalid key exit = %d, want 4", code)
	}
	if !strings.Contains(errOut, "invalid API key") || !strings.Contains(errOut, "req-abc") {
		t.Errorf("expected RFC7807 detail + request_id, got %q", errOut)
	}
	// nothing persisted on failure.
	if e, _ := cs.Get("dev"); e != nil {
		t.Errorf("no credential should be stored after failed login: %+v", e)
	}
}

func TestAuthEnvPrecedenceOverStored(t *testing.T) {
	srv := meServer(t)
	defer srv.Close()
	cfg, cs := authFixture(t, srv.URL)
	// Store a bad key, but env supplies the good one — env must win.
	_ = cs.Save("dev", cred.Entry{Type: cred.TypeAPIKey, APIKey: "stored_bad_key_abcdef"})

	getenv := func(k string) string {
		if k == cred.EnvAPIKey {
			return goodKey
		}
		return ""
	}
	out, _, code := runAuth(t, cfg, cs, getenv, "", "auth", "status")
	if code != 0 {
		t.Fatalf("status exit = %d, want 0 (env key valid)", code)
	}
	if !strings.Contains(out, "environment") || !strings.Contains(out, "dev@example.com") {
		t.Errorf("status should report env method + identity: %q", out)
	}
}

func TestAuthLoginRedactsKeyInLog(t *testing.T) {
	srv := meServer(t)
	defer srv.Close()
	cfg, cs := authFixture(t, srv.URL)
	logDir := t.TempDir()

	root := NewRootCommand(Options{Store: cfg, CredStore: cs, Getenv: func(string) string { return "" }, LogDir: logDir})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(goodKey + "\n"))
	root.SetArgs([]string{"--debug", "auth", "login", "--with-api-key"})
	if code := exitcode.FromError(root.ExecuteContext(context.Background())); code != 0 {
		t.Fatalf("login exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "cli.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(data), goodKey) {
		t.Errorf("log leaked the API key")
	}
}
