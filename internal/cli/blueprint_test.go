package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// blueprintCapture records the last create/update body and list query for a
// fabricated team-scoped blueprints endpoint.
type blueprintCapture struct {
	createBody map[string]any
	updateBody map[string]any
	listQuery  url.Values
	deleted    string
}

func blueprintServer(t *testing.T, cap *blueprintCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/blueprints"
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"blueprints":[{"id":"bp-id","slug":"bp-1","title":"Rules","type":"claude-code","updated_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"bp-new","slug":"bp-1","title":"Created","type":"claude-code","updated_at":"2026-02-02T00:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	// Single item is addressed by project + slug.
	mux.HandleFunc(base+"/p-1/bp-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"bp-id","slug":"bp-1","title":"Rules","type":"claude-code","updated_at":"2026-02-01T00:00:00Z"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&cap.updateBody)
			_, _ = w.Write([]byte(`{"id":"bp-id","slug":"bp-1","title":"Rules v2","type":"claude-code","updated_at":"2026-02-03T00:00:00Z"}`))
		case http.MethodDelete:
			cap.deleted = "bp-1"
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc(base+"/p-1/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"blueprint not found","code":"not_found","request_id":"req-bp-404"}`))
	})
	return httptest.NewServer(mux)
}

func TestBlueprintCreateFromFile(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	dir := t.TempDir()
	f := dir + "/spec.md"
	if err := os.WriteFile(f, []byte("do the thing"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "blueprint", "create", "bp-1",
		"--project", "p-1", "--title", "Rules", "--body-file", f)
	if code != 0 {
		t.Fatalf("create exit = %d, out=%q", code, out)
	}
	if cap.createBody["project_id"] != "p-1" || cap.createBody["slug"] != "bp-1" ||
		cap.createBody["title"] != "Rules" || cap.createBody["content"] != "do the thing" {
		t.Errorf("create body wrong: %+v", cap.createBody)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("create output missing rendered response: %q", out)
	}
}

func TestBlueprintCreateValidation(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing project → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "blueprint", "create", "bp-1", "--title", "Rules", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", code)
	}
	// Missing content → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "blueprint", "create", "bp-1", "--project", "p-1", "--title", "Rules"); code != exitcode.UsageErr {
		t.Errorf("missing content exit = %d, want 2", code)
	}
	// Missing title → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "blueprint", "create", "bp-1", "--project", "p-1", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing title exit = %d, want 2", code)
	}
}

func TestBlueprintGetRequiresProjectAndNotFound(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// No project → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "blueprint", "get", "bp-1"); code != exitcode.UsageErr {
		t.Errorf("get without project exit = %d, want 2", code)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "get", "bp-1")
	if code != 0 || !strings.Contains(out, "bp-1") {
		t.Fatalf("get exit=%d out=%q", code, out)
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "get", "nope")
	if code != exitcode.RuntimeErr {
		t.Errorf("404 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "blueprint not found") || !strings.Contains(errOut, "req-bp-404") {
		t.Errorf("404 should render detail + request_id: %q", errOut)
	}
}

func TestBlueprintUpdate(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "update", "bp-1", "--title", "Rules v2")
	if code != 0 {
		t.Fatalf("update exit = %d, out=%q", code, out)
	}
	if cap.updateBody["title"] != "Rules v2" {
		t.Errorf("update body wrong: %+v", cap.updateBody)
	}
	// No fields → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "update", "bp-1"); code != exitcode.UsageErr {
		t.Errorf("empty update exit = %d, want 2", code)
	}
}

func TestBlueprintDelete(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Non-interactive without --yes → exit 2, no delete.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "delete", "bp-1"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--project", "p-1", "blueprint", "delete", "bp-1", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "bp-1" {
		t.Error("delete --yes did not delete")
	}
	if !strings.Contains(errOut, "Deleted blueprint bp-1") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}

func TestBlueprintListPaginationAndProjectFilter(t *testing.T) {
	var cap blueprintCapture
	srv := blueprintServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"blueprint", "list", "--limit", "3")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("project_id") != "p-9" || cap.listQuery.Get("limit") != "3" {
		t.Errorf("list query wrong: %v", cap.listQuery)
	}
	if !strings.Contains(out, `"slug":"bp-1"`) {
		t.Errorf("list json missing blueprint: %q", out)
	}
}
