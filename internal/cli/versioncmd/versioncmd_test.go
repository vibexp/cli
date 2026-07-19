package versioncmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
)

func run(t *testing.T, rt *config.Runtime) string {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	ctx := context.Background()
	if rt != nil {
		ctx = clictx.WithRuntime(ctx, rt)
	}
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.String()
}

func TestVersionShowsServerSha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","sha":"deadbeef"}`))
		}
	}))
	defer srv.Close()

	out := run(t, &config.Runtime{BaseURL: srv.URL})
	if !strings.Contains(out, "vibexp ") {
		t.Errorf("missing CLI version line: %q", out)
	}
	if !strings.Contains(out, "server: deadbeef") {
		t.Errorf("missing server sha: %q", out)
	}
}

func TestVersionOfflineDegradesGracefully(t *testing.T) {
	// Unreachable server → no error, just CLI info, no server line.
	out := run(t, &config.Runtime{BaseURL: "http://127.0.0.1:1"})
	if !strings.Contains(out, "vibexp ") {
		t.Errorf("missing CLI version line: %q", out)
	}
	if strings.Contains(out, "server:") {
		t.Errorf("should not show server line when offline: %q", out)
	}
}

func TestVersionNoContextNoServerLine(t *testing.T) {
	out := run(t, nil)
	if strings.Contains(out, "server:") {
		t.Errorf("no context → no server line: %q", out)
	}
}
