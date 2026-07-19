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

// memoryServer emulates the team-scoped memories CRUD endpoints and records the
// last create/update body and list query.
type memoryCapture struct {
	createBody map[string]any
	updateBody map[string]any
	listQuery  url.Values
	deleted    string
}

func memoryServer(t *testing.T, cap *memoryCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/memories"
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"memories":[{"id":"m-1","project_id":"p-1","status":"active","updated_at":"2026-02-01T00:00:00Z","text":"hello world"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"m-new","project_id":"p-1","status":"active","updated_at":"2026-02-02T00:00:00Z","text":"created"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(base+"/m-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"m-1","project_id":"p-1","status":"active","updated_at":"2026-02-01T00:00:00Z","text":"hello world"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&cap.updateBody)
			_, _ = w.Write([]byte(`{"id":"m-1","project_id":"p-1","status":"archived","updated_at":"2026-02-03T00:00:00Z","text":"hello world"}`))
		case http.MethodDelete:
			cap.deleted = "m-1"
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc(base+"/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"memory not found","code":"not_found","request_id":"req-mem-404"}`))
	})
	return httptest.NewServer(mux)
}

func TestMemoryCreateFromFileAndProject(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	dir := t.TempDir()
	f := dir + "/body.md"
	if err := os.WriteFile(f, []byte("# note\nbody text"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "memory", "create", "--project", "p-1", "--body-file", f)
	if code != 0 {
		t.Fatalf("create exit = %d, out=%q", code, out)
	}
	if cap.createBody["project_id"] != "p-1" || cap.createBody["text"] != "# note\nbody text" {
		t.Errorf("create body wrong: %+v", cap.createBody)
	}
	if !strings.Contains(out, "m-new") {
		t.Errorf("create output missing new id: %q", out)
	}
}

func TestMemoryCreateFromStdin(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "from stdin body", "memory", "create", "--project", "p-1", "--body-file", "-")
	if code != 0 {
		t.Fatalf("create exit = %d", code)
	}
	if cap.createBody["text"] != "from stdin body" {
		t.Errorf("stdin body not sent: %+v", cap.createBody)
	}
}

func TestMemoryCreateRequiresContentAndProject(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()

	// No project anywhere → exit 2.
	cfgNoTeam, csNoTeam := apiFixture(t, srv.URL, "the-team") // team set, project not
	if _, _, code := runAuth(t, cfgNoTeam, csNoTeam, nil, "x", "memory", "create", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", code)
	}
	// Project given but no content → exit 2.
	if _, _, code := runAuth(t, cfgNoTeam, csNoTeam, nil, "", "memory", "create", "--project", "p-1"); code != exitcode.UsageErr {
		t.Errorf("missing content exit = %d, want 2", code)
	}
}

func TestMemoryGetAndNotFound(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "memory", "get", "m-1")
	if code != 0 || !strings.Contains(out, "m-1") {
		t.Fatalf("get exit=%d out=%q", code, out)
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "memory", "get", "nope")
	if code != exitcode.RuntimeErr {
		t.Errorf("404 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "memory not found") || !strings.Contains(errOut, "req-mem-404") {
		t.Errorf("404 should render detail + request_id: %q", errOut)
	}
}

func TestMemoryUpdate(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "memory", "update", "m-1", "--status", "archived")
	if code != 0 {
		t.Fatalf("update exit = %d, out=%q", code, out)
	}
	if cap.updateBody["status"] != "archived" {
		t.Errorf("update body wrong: %+v", cap.updateBody)
	}
	// No fields → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "memory", "update", "m-1"); code != exitcode.UsageErr {
		t.Errorf("empty update exit = %d, want 2", code)
	}
}

func TestMemoryDelete(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Non-interactive without --yes → exit 2, no delete.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "memory", "delete", "m-1"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	// With --yes → deletes.
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "memory", "delete", "m-1", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "m-1" {
		t.Error("delete --yes did not delete")
	}
	if !strings.Contains(errOut, "Deleted memory m-1") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}

func TestMemoryListPaginationAndProjectFilter(t *testing.T) {
	var cap memoryCapture
	srv := memoryServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"memory", "list", "--limit", "3")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("project_id") != "p-9" || cap.listQuery.Get("limit") != "3" {
		t.Errorf("list query wrong: %v", cap.listQuery)
	}
	if !strings.Contains(out, `"id":"m-1"`) {
		t.Errorf("list json missing memory: %q", out)
	}
}
