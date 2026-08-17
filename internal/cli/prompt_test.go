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

// promptCapture records the last create/update/render body and list query for a
// fabricated team-scoped prompts endpoint.
type promptCapture struct {
	createBody map[string]any
	updateBody map[string]any
	renderBody map[string]any
	listQuery  url.Values
	deleted    string
}

func promptServer(t *testing.T, cap *promptCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team/prompts"
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.listQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"prompts":[{"id":"pr-id","slug":"greet","name":"Greeting","status":"active","updated_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"pr-new","slug":"greet","name":"Created","status":"active","updated_at":"2026-02-02T00:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(base+"/greet", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"pr-id","slug":"greet","name":"Greeting","status":"active","updated_at":"2026-02-01T00:00:00Z"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&cap.updateBody)
			_, _ = w.Write([]byte(`{"id":"pr-id","slug":"greet","name":"Greeting v2","status":"active","updated_at":"2026-02-03T00:00:00Z"}`))
		case http.MethodDelete:
			cap.deleted = "greet"
			w.WriteHeader(http.StatusNoContent)
		}
	})
	// render success — echoes the placeholders into a rendered body.
	mux.HandleFunc(base+"/greet/render", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&cap.renderBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rendered_body":"Hello prod in eu","placeholders_missing":[],"warnings":[]}`))
	})
	// render with a missing required variable → 422 validation error.
	mux.HandleFunc(base+"/needs-var/render", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"Unprocessable Entity","status":422,"detail":"missing required placeholder","code":"validation_error","request_id":"req-render-422","validation_errors":[{"field":"placeholders.env","message":"required"}]}`))
	})
	mux.HandleFunc(base+"/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"prompt not found","code":"not_found","request_id":"req-pr-404"}`))
	})
	return httptest.NewServer(mux)
}

func TestPromptCreateFromFile(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	dir := t.TempDir()
	f := dir + "/body.md"
	if err := os.WriteFile(f, []byte("Hello {{env}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "create", "greet",
		"--project", "p-1", "--name", "Greeting", "--body-file", f, "--label", "a", "--label", "b")
	if code != 0 {
		t.Fatalf("create exit = %d, out=%q", code, out)
	}
	if cap.createBody["project_id"] != "p-1" || cap.createBody["slug"] != "greet" ||
		cap.createBody["name"] != "Greeting" || cap.createBody["body"] != "Hello {{env}}" {
		t.Errorf("create body wrong: %+v", cap.createBody)
	}
	labels, _ := cap.createBody["labels"].([]any)
	if len(labels) != 2 || labels[0] != "a" || labels[1] != "b" {
		t.Errorf("labels not sent: %+v", cap.createBody["labels"])
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("create output missing rendered response: %q", out)
	}
}

func TestPromptCreateValidation(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing project → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "prompt", "create", "greet", "--name", "G", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", code)
	}
	// Missing body → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "create", "greet", "--project", "p-1", "--name", "G"); code != exitcode.UsageErr {
		t.Errorf("missing body exit = %d, want 2", code)
	}
	// Missing name → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "x", "prompt", "create", "greet", "--project", "p-1", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("missing name exit = %d, want 2", code)
	}
}

func TestPromptGetAndNotFound(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "get", "greet")
	if code != 0 || !strings.Contains(out, "greet") {
		t.Fatalf("get exit=%d out=%q", code, out)
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "prompt", "get", "nope")
	if code != exitcode.RuntimeErr {
		t.Errorf("404 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "prompt not found") || !strings.Contains(errOut, "req-pr-404") {
		t.Errorf("404 should render detail + request_id: %q", errOut)
	}
}

func TestPromptUpdate(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "update", "greet", "--name", "Greeting v2")
	if code != 0 {
		t.Fatalf("update exit = %d, out=%q", code, out)
	}
	if cap.updateBody["name"] != "Greeting v2" {
		t.Errorf("update body wrong: %+v", cap.updateBody)
	}
	if _, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "update", "greet"); code != exitcode.UsageErr {
		t.Errorf("empty update exit = %d, want 2", code)
	}
}

func TestPromptDelete(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "delete", "greet"); code != exitcode.UsageErr {
		t.Errorf("delete without --yes exit = %d, want 2", code)
	}
	if cap.deleted != "" {
		t.Error("must not delete without confirmation")
	}
	_, errOut, code := runAuth(t, cfg, cs, nil, "", "prompt", "delete", "greet", "--yes")
	if code != 0 {
		t.Fatalf("delete --yes exit = %d", code)
	}
	if cap.deleted != "greet" {
		t.Error("delete --yes did not delete")
	}
	if !strings.Contains(errOut, "Deleted prompt greet") {
		t.Errorf("delete should confirm: %q", errOut)
	}
}

func TestPromptListPaginationAndProjectFilter(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"prompt", "list", "--limit", "3")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if cap.listQuery.Get("project_id") != "p-9" || cap.listQuery.Get("limit") != "3" {
		t.Errorf("list query wrong: %v", cap.listQuery)
	}
	if !strings.Contains(out, `"slug":"greet"`) {
		t.Errorf("list json missing prompt: %q", out)
	}
}

// TestPromptRenderRaw is the stdout-purity contract: the rendered body is
// written byte-for-byte with no added decoration, so it is safe to pipe.
func TestPromptRenderRaw(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "render", "greet",
		"--var", "env=prod", "--var", "region=eu")
	if code != 0 {
		t.Fatalf("render exit = %d, out=%q", code, out)
	}
	if out != "Hello prod in eu" {
		t.Errorf("render stdout not pure rendered body: %q", out)
	}
	ph, _ := cap.renderBody["placeholders"].(map[string]any)
	if ph["env"] != "prod" || ph["region"] != "eu" {
		t.Errorf("placeholders not sent: %+v", cap.renderBody["placeholders"])
	}
}

// TestPromptRenderDuplicateVarLastWins documents the last-wins rule for a
// repeated --var key.
func TestPromptRenderDuplicateVarLastWins(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "render", "greet",
		"--var", "env=dev", "--var", "env=prod")
	if code != 0 {
		t.Fatalf("render exit = %d", code)
	}
	ph, _ := cap.renderBody["placeholders"].(map[string]any)
	if ph["env"] != "prod" {
		t.Errorf("duplicate --var should keep last: %+v", ph)
	}
}

func TestPromptRenderFormatJSON(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "prompt", "render", "greet", "--var", "env=prod")
	if code != 0 {
		t.Fatalf("render --format json exit = %d", code)
	}
	// The full API envelope is passed through untouched.
	if !strings.Contains(out, `"rendered_body":"Hello prod in eu"`) || !strings.Contains(out, "placeholders_missing") {
		t.Errorf("render json should be the raw envelope: %q", out)
	}
}

// TestPromptRenderJQ verifies --jq routes render through the structured path
// (operating on the API envelope) rather than the raw rendered-body shortcut.
func TestPromptRenderJQ(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "render", "greet",
		"--var", "env=prod", "--jq", ".rendered_body")
	if code != 0 {
		t.Fatalf("render --jq exit = %d, out=%q", code, out)
	}
	if !strings.Contains(out, "Hello prod in eu") {
		t.Errorf("render --jq should operate on the envelope: %q", out)
	}
}

func TestPromptRenderMissingVariable(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "prompt", "render", "needs-var")
	if code != exitcode.RuntimeErr {
		t.Errorf("422 render exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "missing required placeholder") || !strings.Contains(errOut, "req-render-422") {
		t.Errorf("render validation should surface detail + request_id: %q", errOut)
	}
	if !strings.Contains(errOut, "placeholders.env") {
		t.Errorf("render validation should surface the field: %q", errOut)
	}
}

func TestPromptRenderInvalidVar(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// No '=' → usage error, no request sent.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "prompt", "render", "greet", "--var", "novalue"); code != exitcode.UsageErr {
		t.Errorf("invalid --var exit = %d, want 2", code)
	}
	if cap.renderBody != nil {
		t.Error("must not call render with an invalid --var")
	}
}

// TestPromptListStaleFilter covers the one filter listPrompts accepts.
func TestPromptListStaleFilter(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "prompt", "list", "--stale")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if got := cap.listQuery.Get("freshness"); got != "stale" {
		t.Errorf("freshness param = %q, want stale", got)
	}
}

// TestPromptListRejectsMetadataFlag is the guard that we never bind a filter
// the endpoint ignores: listPrompts has no metadata param, so --metadata must
// fail as an unknown flag rather than silently returning the full list.
func TestPromptListRejectsMetadataFlag(t *testing.T) {
	var cap promptCapture
	srv := promptServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "prompt", "list", "--metadata", "env=prod")
	if code != exitcode.UsageErr {
		t.Fatalf("exit = %d, want %d (unknown flag)", code, exitcode.UsageErr)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Errorf("stderr = %q, want an unknown-flag error", errOut)
	}
}
