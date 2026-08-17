package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// artifactCapture records the last create/update body and list query for a
// fabricated team-scoped, project-addressed artifacts endpoint.
type artifactCapture struct {
	createBody map[string]any
	updateBody map[string]any
	listQuery  url.Values
	deleted    string
}

func artifactServer(t *testing.T, cap *artifactCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/artifacts"
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"artifacts":[{"id":"art-id","slug":"art-1","title":"Report","project_id":"p-1","updated_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"art-new","slug":"art-1","title":"Created","project_id":"p-1","updated_at":"2026-02-02T00:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	// Single item is addressed by project + slug.
	mux.HandleFunc(base+"/p-1/art-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"art-id","slug":"art-1","title":"Report","project_id":"p-1","updated_at":"2026-02-01T00:00:00Z"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&cap.updateBody)
			_, _ = w.Write([]byte(`{"id":"art-id","slug":"art-1","title":"Report v2","project_id":"p-1","updated_at":"2026-02-03T00:00:00Z"}`))
		case http.MethodDelete:
			cap.deleted = "art-1"
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc(base+"/p-1/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"artifact not found","code":"not_found","request_id":"req-art-404"}`))
	})
	return httptest.NewServer(mux)
}

func TestArtifactCreateFromFile(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	dir := t.TempDir()
	f := dir + "/report.md"
	if err := os.WriteFile(f, []byte("# report body"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "artifact", "create", "art-1",
		"--project", "p-1", "--title", "Report", "--body-file", f)
	if code != 0 {
		t.Fatalf("create exit = %d, out=%q", code, out)
	}
	if cap.createBody["project_id"] != "p-1" || cap.createBody["slug"] != "art-1" ||
		cap.createBody["title"] != "Report" || cap.createBody["content"] != "# report body" {
		t.Errorf("create body wrong: %+v", cap.createBody)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("create output missing rendered response: %q", out)
	}
}

func TestArtifactCreateValidation(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing project → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "artifact", "create", "art-1", "--title", "Report", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", code)
	}
	// Missing content → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "artifact", "create", "art-1", "--project", "p-1", "--title", "Report"); code != exitcode.UsageErr {
		t.Errorf("missing content exit = %d, want 2", code)
	}
	// Missing title → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "artifact", "create", "art-1", "--project", "p-1", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing title exit = %d, want 2", code)
	}
}

// TestArtifactProjectResolutionMatrix exercises the project-resolution chain:
// --project flag, VIBEXP_PROJECT env, and missing (→ exit 2). Context default is
// covered by the config package's own resolution tests.
func TestArtifactProjectResolutionMatrix(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Flag.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "get", "art-1"); code != 0 {
		t.Errorf("get with --project flag exit = %d, want 0", code)
	}
	// Env.
	env := func(k string) string {
		if k == config.EnvProject {
			return "p-1"
		}
		return ""
	}
	if _, _, code := runAuth(t, cfg, cs, env, "", "artifact", "get", "art-1"); code != 0 {
		t.Errorf("get with VIBEXP_PROJECT env exit = %d, want 0", code)
	}
	// Missing → exit 2 with guidance.
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "artifact", "get", "art-1")
	if code != exitcode.UsageErr {
		t.Errorf("get without project exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "no project set") {
		t.Errorf("missing-project error should guide the user: %q", errOut)
	}
}

func TestArtifactGetNotFound(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "get", "art-1")
	if code != 0 || !strings.Contains(out, "art-1") {
		t.Fatalf("get exit=%d out=%q", code, out)
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "get", "nope")
	if code != exitcode.RuntimeErr {
		t.Errorf("404 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "artifact not found") || !strings.Contains(errOut, "req-art-404") {
		t.Errorf("404 should render detail + request_id: %q", errOut)
	}
}

func TestArtifactUpdate(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "update", "art-1", "--title", "Report v2")
	if code != 0 {
		t.Fatalf("update exit = %d, out=%q", code, out)
	}
	if cap.updateBody["title"] != "Report v2" {
		t.Errorf("update body wrong: %+v", cap.updateBody)
	}
	if _, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "update", "art-1"); code != exitcode.UsageErr {
		t.Errorf("empty update exit = %d, want 2", code)
	}
}

func TestArtifactDelete(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "delete", "art-1"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "artifact", "delete", "art-1", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "art-1" {
		t.Error("delete --yes did not delete")
	}
	if !strings.Contains(errOut, "Deleted artifact art-1") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}

func TestArtifactListPaginationAndProjectFilter(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"artifact", "list", "--limit", "3")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("project_id") != "p-9" || cap.listQuery.Get("limit") != "3" {
		t.Errorf("list query wrong: %v", cap.listQuery)
	}
	if !strings.Contains(out, `"slug":"art-1"`) {
		t.Errorf("list json missing artifact: %q", out)
	}
}

func TestArtifactListMetadataFilter(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json",
		"artifact", "list", "--metadata", "env=prod")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("metadata") != `{"env":["prod"]}` {
		t.Errorf("metadata param = %q", cap.listQuery.Get("metadata"))
	}
}

// TestArtifactListStaleFilter asserts the freshness filter merges into a path
// that already carries project scope.
func TestArtifactListStaleFilter(t *testing.T) {
	var cap artifactCapture
	srv := artifactServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"artifact", "list", "--stale")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if got := cap.listQuery.Get("freshness"); got != "stale" {
		t.Errorf("freshness param = %q, want stale", got)
	}
	if got := cap.listQuery.Get("project_id"); got != "p-9" {
		t.Errorf("project_id = %q, want p-9 (the filter must not drop it)", got)
	}
}
