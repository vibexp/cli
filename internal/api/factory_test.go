package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

func TestFactoryWiresAuthAndUserAgent(t *testing.T) {
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","email":"a@b.c","name":"A","created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,"onboarding_completed":true}`))
	}))
	defer srv.Close()

	cs := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if err := cs.Save("dev", cred.Entry{Type: cred.TypeAPIKey, APIKey: "vxk_test_key_abcdef"}); err != nil {
		t.Fatalf("seed cred: %v", err)
	}
	rt := &config.Runtime{ContextName: "dev", BaseURL: srv.URL}

	client, err := New(context.Background(), rt, cs, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := client.GetMeWithResponse(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if resp.JSON200 == nil {
		t.Fatalf("expected 200, got %s", resp.Status())
	}
	if gotAuth != "Bearer vxk_test_key_abcdef" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "vibexp-cli/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestFactoryEnvKeyOverridesStored(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u","email":"a@b.c","name":"A","created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,"onboarding_completed":true}`))
	}))
	defer srv.Close()

	cs := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	_ = cs.Save("dev", cred.Entry{Type: cred.TypeAPIKey, APIKey: "stored-key-abcdef"})
	rt := &config.Runtime{ContextName: "dev", BaseURL: srv.URL}
	getenv := func(k string) string {
		if k == cred.EnvAPIKey {
			return "env-key-abcdef"
		}
		return ""
	}
	client, err := New(context.Background(), rt, cs, getenv)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _ = client.GetMeWithResponse(context.Background())
	if gotAuth != "Bearer env-key-abcdef" {
		t.Errorf("Authorization = %q, want env key", gotAuth)
	}
}

func TestFactoryNoCredentialIsAuthError(t *testing.T) {
	cs := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	rt := &config.Runtime{ContextName: "dev", BaseURL: "http://127.0.0.1:1"}
	_, err := New(context.Background(), rt, cs, func(string) string { return "" })
	if got := exitcode.FromError(err); got != exitcode.AuthErr {
		t.Errorf("no-credential New exit = %d, want 4", got)
	}
}

func TestFactoryNoBaseURLIsUsageError(t *testing.T) {
	cs := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	_, err := New(context.Background(), &config.Runtime{ContextName: "dev"}, cs, func(string) string { return "" })
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("no-base-URL New exit = %d, want 2", got)
	}
}
