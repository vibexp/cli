package blueprintcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// These drive the metadata write path inside this package (Sonar measures
// coverage per package); the cross-noun behaviour is covered end to end in
// internal/cli/metadata_write_test.go. All data is fabricated.

func fakeEnv(key string) string {
	if key == cred.EnvAPIKey {
		return "vxk_fabricated"
	}
	return ""
}

// metaServer serves the collection and one item (stored metadata {"a":"1"})
// and records the last write body and every method.
func metaServer(t *testing.T, body *map[string]any, methods *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		if r.Method != http.MethodGet {
			_ = json.NewDecoder(r.Body).Decode(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"bp-1","metadata":{"a":"1"}}`))
	}))
}

func runMeta(t *testing.T, srv *httptest.Server, cmd *cobra.Command, stdin string, args ...string) int {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Team: "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01", Project: "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61", Format: "json"})
	return exitcode.FromError(cmd.ExecuteContext(ctx))
}

func TestMetadataWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	resolve := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	tests := []struct {
		name    string
		update  bool
		args    []string
		want    string // metadata sent, "" = no write
		methods string
		code    int
	}{
		{"create", false, []string{"bp-1", "--title", "T", "--body-file", "-", "--metadata", "k=v"}, `{"k":"v"}`, "POST", 0},
		{"create malformed", false, []string{"bp-1", "--title", "T", "--body-file", "-", "--metadata", "bad"}, "", "", exitcode.UsageErr},
		{"create both stdin", false, []string{"bp-1", "--title", "T", "--body-file", "-", "--metadata-json", "-"}, "", "", exitcode.UsageErr},
		{"update merges", true, []string{"bp-1", "--metadata", "b=2"}, `{"a":"1","b":"2"}`, "GET PUT", 0},
		{"update replace", true, []string{"bp-1", "--replace-metadata", "--metadata", "c=3"}, `{"c":"3"}`, "PUT", 0},
		{"update nothing", true, []string{"bp-1"}, "", "", exitcode.UsageErr},
		{"update both stdin", true, []string{"bp-1", "--body-file", "-", "--metadata-json", "-"}, "", "", exitcode.UsageErr},
		{"update malformed", true, []string{"bp-1", "--metadata-json", "[1]"}, "", "", exitcode.UsageErr},
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
			if tc.want == "" {
				return
			}
			if got, _ := json.Marshal(body["metadata"]); string(got) != tc.want {
				t.Errorf("metadata = %s, want %s", got, tc.want)
			}
		})
	}
}
