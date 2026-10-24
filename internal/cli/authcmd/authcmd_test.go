package authcmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// The identity check is a platform request too, so it must identify the CLI
// exactly as the shared Doer does: the platform keys the cli channel on it.
func TestFetchIdentitySendsTheSharedUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","email":"a@b.c","name":"A","created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,"onboarding_completed":true}`))
	}))
	defer srv.Close()

	if _, err := fetchIdentity(context.Background(), srv.URL, "vxk_test_key_abcdef", 5*time.Second); err != nil {
		t.Fatalf("fetchIdentity: %v", err)
	}
	if gotUA != api.UserAgent() {
		t.Errorf("User-Agent = %q, want %q", gotUA, api.UserAgent())
	}
	if !strings.HasPrefix(gotUA, "VibeXP-CLI/") {
		t.Errorf("User-Agent = %q, want VibeXP-CLI/ prefix", gotUA)
	}
}

func TestRequireBaseURLNamesEnvAndContext(t *testing.T) {
	_, err := requireBaseURL(&config.Runtime{})
	if code := exitcode.FromError(err); code != exitcode.UsageErr {
		t.Fatalf("exit = %d, want %d (err %v)", code, exitcode.UsageErr, err)
	}
	for _, want := range []string{config.EnvBaseURL, "set-context <name> "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q: %q", want, err)
		}
	}

	got, err := requireBaseURL(&config.Runtime{BaseURL: "https://example.com"})
	if err != nil || got != "https://example.com" {
		t.Errorf("requireBaseURL = %q, %v", got, err)
	}
}
