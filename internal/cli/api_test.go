package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// apiServer emulates enough of the API for `vibexp api` tests.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/things", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"items":[{"id":"t1"},{"id":"t2"}]}`))
		case http.MethodPost:
			body, _ := readAll(r)
			// Echo back what we received so the test can assert it.
			resp := map[string]any{
				"content_type": r.Header.Get("Content-Type"),
				"auth":         r.Header.Get("Authorization"),
				"custom":       r.Header.Get("X-Custom"),
				"body":         json.RawMessage(body),
			}
			b, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/the-team/scoped", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"team":"the-team"}`))
	})
	mux.HandleFunc("/api/v1/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"no such resource","code":"not_found","request_id":"req-404"}`))
	})
	mux.HandleFunc("/api/v1/denied", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"Forbidden","status":403,"detail":"nope","code":"forbidden","request_id":"req-403"}`))
	})
	mux.HandleFunc("/api/v1/paged", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		// pages 1,2 return the full limit (2); page 3 returns 1 (short → last).
		var ids []string
		switch page {
		case 1:
			ids = []string{"p1", "p2"}
		case 2:
			ids = []string{"p3", "p4"}
		case 3:
			ids = []string{"p5"}
		}
		items := make([]string, len(ids))
		for i, id := range ids {
			items[i] = fmt.Sprintf(`{"id":%q}`, id)
		}
		_, _ = w.Write([]byte(`{"items":[` + strings.Join(items, ",") + `],"page":` + strconv.Itoa(page) + `}`))
	})
	return httptest.NewServer(mux)
}

func readAll(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

// apiFixture wires a config context + a seeded API-key credential.
func apiFixture(t *testing.T, serverURL, team string) (*config.Store, *cred.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Store{Path: filepath.Join(dir, "config.yaml")}
	ctx := config.Context{Name: "dev", BaseURL: serverURL}
	if team != "" {
		ctx.DefaultTeam = team
	}
	if err := cfg.Save(&config.File{CurrentContext: "dev", Contexts: []config.Context{ctx}}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	cs := &cred.Store{Path: filepath.Join(dir, "credentials.json")}
	_ = cs.Save("dev", cred.Entry{Type: cred.TypeAPIKey, APIKey: "vxk_api_test_key_123456"})
	return cfg, cs
}

func TestAPIGetRendersJSON(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	out, _, code := runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/things")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, `"id":"t1"`) {
		t.Errorf("output missing item: %q", out)
	}
}

func TestAPIPostWithStdinBodyAndHeader(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	out, _, code := runAuth(t, cfg, cs, nil, `{"hello":"world"}`,
		"api", "POST", "/api/v1/things", "--input", "-", "--header", "X-Custom: yes")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	var got struct {
		ContentType string          `json:"content_type"`
		Auth        string          `json:"auth"`
		Custom      string          `json:"custom"`
		Body        json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse echo: %v\n%s", err, out)
	}
	if got.ContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.ContentType)
	}
	if got.Auth != "Bearer vxk_api_test_key_123456" {
		t.Errorf("auth = %q", got.Auth)
	}
	if got.Custom != "yes" {
		t.Errorf("custom header = %q, want yes", got.Custom)
	}
	if strings.TrimSpace(string(got.Body)) != `{"hello":"world"}` {
		t.Errorf("body = %s", got.Body)
	}
}

func TestAPITeamSubstitution(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/{team}/scoped")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	if !strings.Contains(out, `"team":"the-team"`) {
		t.Errorf("team not substituted: %q", out)
	}
}

func TestAPITeamMissingIsUsageError(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "") // no team

	_, _, code := runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/{team}/scoped")
	if code != exitcode.UsageErr {
		t.Errorf("missing team exit = %d, want 2", code)
	}
}

func TestAPIErrorParityExitCodes(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/missing")
	if code != exitcode.RuntimeErr {
		t.Errorf("404 exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "no such resource") || !strings.Contains(errOut, "req-404") {
		t.Errorf("404 should render detail + request_id: %q", errOut)
	}

	_, _, code = runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/denied")
	if code != exitcode.AuthErr {
		t.Errorf("403 exit = %d, want 4", code)
	}
}

func TestAPIBadMethodIsUsageError(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")
	if _, _, code := runAuth(t, cfg, cs, nil, "", "api", "FROB", "/api/v1/things"); code != exitcode.UsageErr {
		t.Errorf("bad method exit = %d, want 2", code)
	}
}

func TestAPIPaginateMergesPages(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	out, _, code := runAuth(t, cfg, cs, nil, "", "api", "GET", "/api/v1/paged?limit=2", "--paginate")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	var items []map[string]string
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("paginate output not a JSON array: %v\n%s", err, out)
	}
	if len(items) != 5 {
		t.Fatalf("merged items = %d, want 5 (union of all pages)", len(items))
	}
	if items[0]["id"] != "p1" || items[4]["id"] != "p5" {
		t.Errorf("merged order wrong: %+v", items)
	}
}

func TestAPIPaginateRejectsNonGet(t *testing.T) {
	srv := apiServer(t)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")
	if _, _, code := runAuth(t, cfg, cs, nil, "", "api", "POST", "/api/v1/things", "--paginate"); code != exitcode.UsageErr {
		t.Errorf("--paginate POST exit = %d, want 2", code)
	}
}
