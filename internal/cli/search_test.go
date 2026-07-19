package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type searchCapture struct {
	body map[string]any
}

func searchServer(t *testing.T, cap *searchCapture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/the-team/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&cap.body)
		_, _ = w.Write([]byte(`{"results":[{"type":"prompt","id":"p-1","slug":"greet","title":"Greeting","score":0.91,"project_name":"Proj","excerpt":"hello"},{"type":"memory","id":"m-1","slug":"","title":"Note","score":0.72,"project_name":"Proj","excerpt":"note"}],"page":1,"per_page":10,"total_count":2,"total_pages":1}`))
	})
	return httptest.NewServer(mux)
}

func TestSearchMixedResultsTableAndJSON(t *testing.T) {
	var cap searchCapture
	srv := searchServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Table (TSV) output: mixed types, both rendered.
	out, _, code := runAuth(t, cfg, cs, nil, "", "search", "hello", "--type", "prompts", "--type", "memories", "--limit", "5")
	if code != 0 {
		t.Fatalf("search exit = %d, out=%q", code, out)
	}
	if cap.body["query"] != "hello" {
		t.Errorf("query not sent: %+v", cap.body)
	}
	types, _ := cap.body["types"].([]any)
	if len(types) != 2 || types[0] != "prompts" || types[1] != "memories" {
		t.Errorf("types not sent: %+v", cap.body["types"])
	}
	if cap.body["per_page"] != float64(5) {
		t.Errorf("--limit should map to per_page: %+v", cap.body["per_page"])
	}
	if !strings.Contains(out, "prompt") || !strings.Contains(out, "memory") {
		t.Errorf("table should render mixed types: %q", out)
	}

	// JSON output preserves the raw envelope.
	outJSON, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "search", "hello")
	if code != 0 {
		t.Fatalf("search json exit = %d", code)
	}
	if !strings.Contains(outJSON, `"results"`) || !strings.Contains(outJSON, `"score":0.91`) {
		t.Errorf("json should be the raw envelope: %q", outJSON)
	}
}

func TestSearchProjectScope(t *testing.T) {
	var cap searchCapture
	srv := searchServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "--project", "p-9", "search", "hello")
	if code != 0 {
		t.Fatalf("search exit = %d", code)
	}
	if cap.body["project_id"] != "p-9" {
		t.Errorf("--project should scope search: %+v", cap.body)
	}
}
