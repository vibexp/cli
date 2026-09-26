package searchcmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// Fabricated ids behind the "acme" team and "web-app" project slugs.
const (
	teamID    = "5d6e7f80-9a1b-4c2d-8e3f-405162738495"
	projectID = "7a8b9c0d-1e2f-4a3b-9c4d-5e6f7a8b9c0d"
)

type slugSeen struct{ path, query, body string }

// slugServer resolves the slugs and records the scoped request.
func slugServer(t *testing.T, seen *slugSeen) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/teams":
			_, _ = w.Write([]byte(`{"teams":[{"id":"` + teamID + `","slug":"acme"}],"total_count":1}`))
		case "/api/v1/" + teamID + "/projects/web-app":
			_, _ = w.Write([]byte(`{"id":"` + projectID + `"}`))
		case "/api/v1/" + teamID + "/search":
			b, _ := io.ReadAll(r.Body)
			*seen = slugSeen{path: r.URL.Path, query: r.URL.RawQuery, body: string(b)}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title":"Not Found","status":404}`))
		}
	}))
}

func runSlug(t *testing.T, srv *httptest.Server, rt *config.Runtime) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	getenv := func(k string) string {
		if k == cred.EnvAPIKey {
			return "vxk_fabricated"
		}
		return ""
	}
	cmd := New(func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }, getenv)
	cmd.SetArgs([]string{"hello"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	rt.BaseURL, rt.Format = srv.URL, "json"
	return exitcode.FromError(cmd.ExecuteContext(clictx.WithRuntime(context.Background(), rt)))
}

func TestSlugsResolveToUUIDs(t *testing.T) {
	var seen slugSeen
	srv := slugServer(t, &seen)
	defer srv.Close()

	if code := runSlug(t, srv, &config.Runtime{Team: "acme", Project: "web-app"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(seen.body, `"project_id":"`+projectID+`"`) {
		t.Errorf("the search payload should scope to the project UUID: %+v", seen)
	}
	if code := runSlug(t, srv, &config.Runtime{Team: "acme", Project: "gone"}); code != exitcode.UsageErr {
		t.Errorf("an unknown slug should exit 2, got %d", code)
	}
}
