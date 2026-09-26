package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// These drive the page walk and RunList inside this package (coverage is
// measured per package); the command-level behaviour is covered end to end in
// internal/cli/list_pagination_test.go. All data is fabricated.

func fakeEnv(key string) string {
	if key == cred.EnvAPIKey {
		return "vxk_fabricated"
	}
	return ""
}

func credResolver(t *testing.T) resource.CredResolver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	return func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
}

func rawClient(t *testing.T, srv *httptest.Server) *api.RawClient {
	t.Helper()
	store, _ := credResolver(t)()
	c, err := api.NewRaw(context.Background(), &config.Runtime{BaseURL: srv.URL}, store, fakeEnv)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pageServer serves `total` things `size` per page (ignoring the requested
// limit when clamp is set), with the metadata written by meta.
func pageServer(t *testing.T, total, size int, meta func(page int) string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if *hits > 10 {
			t.Error("walk did not stop")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			page, _ = strconv.Atoi(p)
		}
		var things []string
		for i := (page - 1) * size; i < page*size && i < total; i++ {
			things = append(things, fmt.Sprintf(`{"id":"t%d"}`, i+1))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"things":[%s]%s}`, strings.Join(things, ","), meta(page))
	}))
}

func TestFetchAllPagesStopRules(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		size      int
		path      string
		meta      func(page int) string
		wantItems int
		wantHits  int
	}{
		{"total_pages", 5, 2, "/things?limit=2", func(p int) string { return fmt.Sprintf(`,"page":%d,"per_page":2,"total_pages":3`, p) }, 5, 3},
		{"server per_page beats the limit", 5, 2, "/things?limit=50", func(p int) string { return fmt.Sprintf(`,"page":%d,"per_page":2,"total_count":5`, p) }, 5, 3},
		{"page_size", 5, 2, "/things", func(p int) string { return fmt.Sprintf(`,"page":%d,"page_size":2,"total_count":5`, p) }, 5, 3},
		{"no page metadata", 4, 2, "/things?limit=2", func(int) string { return `,"total_count":4` }, 2, 1},
		{"server echoes another page", 6, 2, "/things?limit=2", func(int) string { return `,"page":1` }, 4, 2},
		{"starts at the requested page", 5, 2, "/things?limit=2&page=2", func(p int) string { return fmt.Sprintf(`,"page":%d,"total_pages":3`, p) }, 3, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := 0
			srv := pageServer(t, tt.total, tt.size, tt.meta, &hits)
			defer srv.Close()
			items, notList, err := resource.FetchAllPages(context.Background(), rawClient(t, srv), tt.path, nil)
			if err != nil || notList != nil {
				t.Fatalf("FetchAllPages err=%v notList=%s", err, notList)
			}
			if len(items) != tt.wantItems || hits != tt.wantHits {
				t.Errorf("items=%d hits=%d, want %d and %d", len(items), hits, tt.wantItems, tt.wantHits)
			}
		})
	}
}

func TestFetchAllPagesNotAListAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/single":
			_, _ = w.Write([]byte(`{"id":"x"}`))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":400,"detail":"limit too large","request_id":"req-f"}`))
		}
	}))
	defer srv.Close()
	c := rawClient(t, srv)

	items, notList, err := resource.FetchAllPages(context.Background(), c, "/single", nil)
	if err != nil || items != nil || string(notList) != `{"id":"x"}` {
		t.Errorf("non-list: items=%v notList=%s err=%v", items, notList, err)
	}
	if _, _, err := resource.FetchAllPages(context.Background(), c, "/bad", nil); !strings.Contains(fmt.Sprint(err), "limit too large") {
		t.Errorf("400 not surfaced: %v", err)
	}
	if _, _, err := resource.FetchAllPages(context.Background(), c, "%zz", nil); exitcode.FromError(err) != exitcode.UsageErr {
		t.Errorf("bad path exit = %d, want 2", exitcode.FromError(err))
	}
}

// runList executes a built list command against srv and returns stdout, stderr
// and the exit code.
func runList(t *testing.T, srv *httptest.Server, format string, args ...string) (string, string, int) {
	t.Helper()
	cmd := resource.NewListCommand(credResolver(t), fakeEnv, "List things", resource.ListConfig{
		PathFor: func(*config.Runtime) (string, error) { return "/api/v1/things", nil },
		Spec: output.TableSpec{
			Rows:    resource.ListRows("things"),
			Columns: []output.Column{{Header: "ID", Path: ".id"}},
		},
	})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Format: format})
	err := cmd.ExecuteContext(ctx)
	return out.String(), errOut.String(), exitcode.FromError(err)
}

func TestRunListHintAllAndBadRequest(t *testing.T) {
	hits := 0
	srv := pageServer(t, 3, 2, func(p int) string { return fmt.Sprintf(`,"page":%d,"per_page":2,"total_pages":2,"total_count":3`, p) }, &hits)
	defer srv.Close()

	out, errOut, code := runList(t, srv, "json")
	if code != 0 || !strings.Contains(errOut, "showing page 1 of 2 (2 of 3); use --page, --limit (max 100) or --all") {
		t.Errorf("single page: code=%d stderr=%q", code, errOut)
	}
	if strings.Contains(out, "showing page") {
		t.Error("hint leaked onto stdout")
	}

	out, errOut, code = runList(t, srv, "json", "--all", "--limit", "2")
	var items []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &items) != nil || len(items) != 3 {
		t.Errorf("--all: code=%d out=%s stderr=%q", code, out, errOut)
	}

	out, _, code = runList(t, srv, "text", "--all", "--limit", "2")
	if code != 0 || !strings.Contains(out, "t1") || !strings.Contains(out, "t3") {
		t.Errorf("--all table: code=%d out=%q", code, out)
	}

	if _, _, code := runList(t, srv, "json", "--all", "--page", "2"); code != exitcode.UsageErr {
		t.Errorf("--all --page exit = %d, want 2", code)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"detail":"limit must be at most 100"}`))
	}))
	defer bad.Close()
	for _, args := range [][]string{{"--limit", "500"}, {"--all", "--limit", "500"}} {
		if _, _, code := runList(t, bad, "json", args...); code != exitcode.UsageErr {
			t.Errorf("%v: 400 exit = %d, want 2", args, code)
		}
	}
}

func TestRunListAllNotAList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"only"}`))
	}))
	defer srv.Close()
	out, _, code := runList(t, srv, "json", "--all")
	if code != 0 || !strings.Contains(out, `"only"`) {
		t.Errorf("non-list --all: code=%d out=%q", code, out)
	}
}
