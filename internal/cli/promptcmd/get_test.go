package promptcmd

import (
	"bytes"
	"context"
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

// runGet drives `get` under a stand-in root carrying the global --format/--jq
// flags, against a server that answers every request with resp. All data is
// fabricated.
func runGet(t *testing.T, resp string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	resolve := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	getenv := func(key string) string {
		if key == cred.EnvAPIKey {
			return "vxk_fabricated"
		}
		return ""
	}
	root := &cobra.Command{Use: "vibexp", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("format", "", "")
	root.PersistentFlags().String("jq", "", "")
	root.AddCommand(newGet(resolve, getenv))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"get"}, args...))
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Team: "the-team"})
	code = exitcode.FromError(root.ExecuteContext(ctx))
	return out.String(), errOut.String(), code
}

func TestGetBody(t *testing.T) {
	const prompt = `{"slug":"greet","name":"Greeting","status":"active","body":"Hi {{name}}\nsee @blueprint/x\n","related":[{"relation_type":"governed-by","direction":"outgoing","resource_type":"blueprint","title":"X"}]}`
	const body = "Hi {{name}}\nsee @blueprint/x\n"
	tests := []struct {
		name      string
		resp      string
		args      []string
		code      int
		stdout    string
		stderrHas string
	}{
		{"raw body", prompt, []string{"greet", "--body"}, 0, body, ""},
		{"relations to stderr", prompt, []string{"greet", "--body", "--show-relations"}, 0, body, "related (1)"},
		{"with --format", prompt, []string{"greet", "--body", "--format", "json"}, exitcode.UsageErr, "", ""},
		{"with --jq", prompt, []string{"greet", "--body", "--jq", ".body"}, exitcode.UsageErr, "", ""},
		{"unparsable response", `[]`, []string{"greet", "--body"}, exitcode.RuntimeErr, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := runGet(t, tc.resp, tc.args...)
			if code != tc.code {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tc.code, errOut)
			}
			if out != tc.stdout {
				t.Errorf("stdout = %q, want %q", out, tc.stdout)
			}
			if !strings.Contains(errOut, tc.stderrHas) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tc.stderrHas)
			}
		})
	}
}

// Without --body the metadata row renders as before and the body stays hidden.
func TestGetWithoutBodyUnchanged(t *testing.T) {
	out, _, code := runGet(t, `{"slug":"greet","name":"Greeting","status":"active","body":"SECRET-BODY"}`, "greet", "--format", "text")
	if code != 0 || !strings.Contains(out, "greet") || strings.Contains(out, "SECRET-BODY") {
		t.Errorf("exit=%d stdout=%q", code, out)
	}
}
