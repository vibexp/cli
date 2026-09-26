package memorycmd

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestTitleWrite drives --title through create/update. All data is fabricated.
func TestTitleWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	resolve := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	long := strings.Repeat("é", maxTitleLen+1)
	tests := []struct {
		name    string
		update  bool
		args    []string
		want    string // title sent as JSON, "" = key absent
		methods string
		code    int
	}{
		{"create", false, []string{"--body-file", "-", "--title", "Deploy checklist"}, `"Deploy checklist"`, "POST", 0},
		{"create without --title omits it", false, []string{"--body-file", "-"}, "", "POST", 0},
		{"create at the limit", false, []string{"--body-file", "-", "--title", strings.Repeat("é", maxTitleLen)}, `"` + strings.Repeat("é", maxTitleLen) + `"`, "POST", 0},
		{"create too long", false, []string{"--body-file", "-", "--title", long}, "", "", exitcode.UsageErr},
		{"update sets", true, []string{"m-1", "--title", "Renamed"}, `"Renamed"`, "PUT", 0},
		{"update clears", true, []string{"m-1", "--title", ""}, `null`, "PUT", 0},
		{"update without --title keeps it", true, []string{"m-1", "--status", "active"}, "", "PUT", 0},
		{"update too long", true, []string{"m-1", "--title", long}, "", "", exitcode.UsageErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			var methods []string
			srv := metaServer(t, &body, &methods)
			defer srv.Close()
			cmd := newCreate(resolve, fakeEnv)
			if tc.update {
				cmd = newUpdate(resolve, fakeEnv)
			}
			if code := runMeta(t, srv, cmd, "content", tc.args...); code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
			if got := strings.Join(methods, " "); got != tc.methods {
				t.Errorf("requests = %q, want %q", got, tc.methods)
			}
			got, ok := body["title"]
			if tc.want == "" {
				if ok {
					t.Errorf("title = %v, want the key absent", got)
				}
				return
			}
			if b, _ := json.Marshal(got); string(b) != tc.want {
				t.Errorf("title = %s, want %s", b, tc.want)
			}
		})
	}
}

// TestTitleDetailView pins the TITLE column in `memory get`, empty (not
// "null") for an untitled memory.
func TestTitleDetailView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	resolve := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	for _, tc := range []struct{ title, want string }{
		{`"Deploy checklist"`, "Deploy checklist"},
		{`null`, ""},
	} {
		t.Run(tc.title, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"m-1","project_id":"p-1","status":"active","text":"hello","title":` + tc.title + `}`))
			}))
			defer srv.Close()
			cmd := newGet(resolve, fakeEnv)
			var out bytes.Buffer
			cmd.SetArgs([]string{"m-1"})
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Team: "the-team", Format: "text"})
			if err := cmd.ExecuteContext(ctx); err != nil {
				t.Fatal(err)
			}
			// --format=text is headerless TSV in detailColumns order.
			row := strings.Split(strings.TrimRight(out.String(), "\n"), "\t")
			if len(row) != len(detailColumns) || detailColumns[len(detailColumns)-1].Header != "TITLE" {
				t.Fatalf("row = %q, want %d cells ending in TITLE", out.String(), len(detailColumns))
			}
			if got := row[len(row)-1]; got != tc.want {
				t.Errorf("TITLE = %q, want %q", got, tc.want)
			}
		})
	}
}
