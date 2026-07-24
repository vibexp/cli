package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// relationCapture records what the fake relations API received.
type relationCapture struct {
	createBody map[string]any
	listQuery  url.Values
	confirmed  string
	deleted    string
	seeded     bool
}

// relationServer emulates the team-scoped relations endpoints (v0.8.0) with
// fabricated data.
func relationServer(t *testing.T, cap *relationCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/relations"
	const relationJSON = `{"id":"rel-1","team_id":"t-1","project_id":"p-1","from_type":"artifact","from_id":"a-1","to_type":"blueprint","to_id":"b-1","relation_type":"governed-by","origin":"human","status":"suggested","created_at":"2026-07-21T09:00:00Z","updated_at":"2026-07-21T09:00:00Z"}`

	mux := http.NewServeMux()
	// List (GET) and create (POST) on the collection.
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"relations":[{"relation_id":"rel-1","relation_type":"governed-by","direction":"outgoing","origin":"human","status":"confirmed","resource_type":"blueprint","resource_id":"b-1","title":"Go coding standards","project_id":"p-1","slug":"go-coding-standards","created_at":"2026-07-21T09:00:00Z"}],"total_count":1,"page":1,"per_page":20,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.createBody)
			// Idempotent: an existing edge returns 200, a new one 201.
			if cap.createBody["to_id"] == "existing" {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusCreated)
			}
			_, _ = w.Write([]byte(relationJSON))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	// Sub-routes: /{id}, /{id}/confirm, /seed.
	mux.HandleFunc(base+"/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rest := strings.TrimPrefix(r.URL.Path, base+"/")
		switch {
		case rest == "seed" && r.Method == http.MethodPost:
			cap.seeded = true
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(rest, "/confirm") && r.Method == http.MethodPost:
			id := strings.TrimSuffix(rest, "/confirm")
			if id == "already" {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"title":"Conflict","status":409,"detail":"relation is already confirmed","code":"conflict","request_id":"req-rel-409"}`))
				return
			}
			cap.confirmed = id
			_, _ = w.Write([]byte(`{"id":"rel-1","team_id":"t-1","project_id":"p-1","from_type":"artifact","from_id":"a-1","to_type":"blueprint","to_id":"b-1","relation_type":"governed-by","origin":"human","status":"confirmed","created_at":"2026-07-21T09:00:00Z","updated_at":"2026-07-21T09:05:00Z"}`))
		case r.Method == http.MethodDelete:
			if rest == "nope" {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"relation not found","code":"not_found","request_id":"req-rel-404"}`))
				return
			}
			cap.deleted = rest
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return httptest.NewServer(mux)
}

func TestRelationListColumnsAndQuery(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "relations", "list", "blueprint", "b-1", "--limit", "5")
	if code != 0 {
		t.Fatalf("list exit = %d, out=%q", code, out)
	}
	if cap.listQuery.Get("resource_type") != "blueprint" || cap.listQuery.Get("resource_id") != "b-1" {
		t.Errorf("list did not pass resource_type/resource_id: %v", cap.listQuery)
	}
	if cap.listQuery.Get("limit") != "5" {
		t.Errorf("list did not pass pagination: %v", cap.listQuery)
	}
	// Table/TSV columns render the resolved neighbor.
	for _, want := range []string{"governed-by", "outgoing", "Go coding standards", "confirmed"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q: %q", want, out)
		}
	}
}

func TestRelationListJSONPassthrough(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "relations", "list", "memory", "m-1")
	if code != 0 {
		t.Fatalf("list json exit = %d", code)
	}
	if !strings.Contains(out, `"relation_id":"rel-1"`) || !strings.Contains(out, `"total_count":1`) {
		t.Errorf("json output should be the raw body: %q", out)
	}
}

func TestRelationListRequiresTwoArgs(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// cobra's Args validation surfaces as a generic error (exit 1), the same as
	// every other ExactArgs command in the CLI; only flag errors map to exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "relations", "list", "blueprint"); code != exitcode.RuntimeErr {
		t.Errorf("list with one arg exit = %d, want 1", code)
	}
}

func TestRelationCreateFlags(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "",
		"relations", "create",
		"--from-type", "artifact", "--from-id", "a-1",
		"--to-type", "blueprint", "--to-id", "b-1",
		"--relation-type", "governed-by")
	if code != 0 {
		t.Fatalf("create exit = %d, out=%q", code, out)
	}
	// origin defaults to human.
	if cap.createBody["origin"] != "human" || cap.createBody["from_type"] != "artifact" || cap.createBody["to_id"] != "b-1" {
		t.Errorf("create body wrong: %+v", cap.createBody)
	}
	if !strings.Contains(out, "rel-1") {
		t.Errorf("create output missing edge id: %q", out)
	}
}

func TestRelationCreateIdempotent200(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// to_id "existing" makes the server answer 200 (edge already existed).
	_, _, code := runAuth(t, cfg, cs, nil, "",
		"relations", "create",
		"--from-type", "artifact", "--from-id", "a-1",
		"--to-type", "blueprint", "--to-id", "existing",
		"--relation-type", "governed-by", "--origin", "ai")
	if code != 0 {
		t.Fatalf("idempotent create exit = %d", code)
	}
}

func TestRelationCreateRequiresFlags(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing --to-id → exit 2, nothing sent.
	if _, _, code := runAuth(t, cfg, cs, nil, "",
		"relations", "create",
		"--from-type", "artifact", "--from-id", "a-1",
		"--to-type", "blueprint",
		"--relation-type", "governed-by"); code != exitcode.UsageErr {
		t.Errorf("missing flag exit = %d, want 2", code)
	}
	if cap.createBody != nil {
		t.Error("must not POST when a required flag is missing")
	}
}

func TestRelationConfirm(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "relations", "confirm", "rel-1")
	if code != 0 {
		t.Fatalf("confirm exit = %d, out=%q", code, out)
	}
	if cap.confirmed != "rel-1" {
		t.Errorf("confirm did not hit the endpoint: %q", cap.confirmed)
	}
	if !strings.Contains(out, "confirmed") {
		t.Errorf("confirm output should show the confirmed edge: %q", out)
	}
}

func TestRelationConfirmAlready409(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "relations", "confirm", "already")
	if code != exitcode.RuntimeErr {
		t.Errorf("409 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "already confirmed") || !strings.Contains(errOut, "req-rel-409") {
		t.Errorf("409 should render detail + request_id: %q", errOut)
	}
}

func TestRelationDelete(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Non-interactive without --yes → exit 2, no delete.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "relations", "delete", "rel-1"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	// With --yes → deletes.
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "relations", "delete", "rel-1", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "rel-1" {
		t.Errorf("delete --yes did not delete: %q", cap.deleted)
	}
	if !strings.Contains(errOut, "Deleted relation rel-1") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}

func TestRelationSeed(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "relations", "seed")
	if code != 0 {
		t.Fatalf("seed exit = %d", code)
	}
	if !cap.seeded {
		t.Error("seed did not hit the endpoint")
	}
	if !strings.Contains(errOut, "background") {
		t.Errorf("seed should confirm background run: %q", errOut)
	}
}

func TestRelationListMissingTeamExit2(t *testing.T) {
	var cap relationCapture
	srv := relationServer(t, &cap)
	defer srv.Close()
	// No team in context, env, or flag → exit 2.
	cfg, cs := apiFixture(t, srv.URL, "")
	if _, _, code := runAuth(t, cfg, cs, nil, "", "relations", "list", "blueprint", "b-1"); code != exitcode.UsageErr {
		t.Errorf("missing team exit = %d, want 2", code)
	}
}
