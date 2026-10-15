package promptcmd

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

const (
	renderOK = `{"rendered_body":"Hello prod","placeholders_missing":[],"warnings":[]}`
	// A strict render with unresolved references, in the shape the platform's
	// renderPrompt handler writes it. Fabricated.
	renderUnresolved = `{"title":"Unprocessable Entity","status":422,"code":"UNRESOLVED_REFERENCES",` +
		`"detail":"Unresolved prompt references: nope, gone",` +
		`"metadata":{"unresolved_references":["nope","gone"]},"request_id":"req-strict-422"}`
)

// renderResult is what one `render` invocation produced, plus the request body
// the fake server received.
type renderResult struct {
	stdout, stderr string
	code           int
	body           map[string]any
}

// runRender drives `render` under a stand-in root against a server that
// answers with status/resp and records the request body. rt carries the
// already-resolved --format/--jq, as the real root folds them into it. A
// returned error is appended to stderr, as the real root prints it. The
// --strict contract lives here (Sonar measures coverage per package).
func runRender(t *testing.T, rt config.Runtime, status int, resp string, args ...string) renderResult {
	t.Helper()
	var res renderResult
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/prompts/greet/render") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&res.body)
		ct := "application/json"
		if status >= 400 {
			ct = "application/problem+json"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
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
	root.AddCommand(newRender(resolve, getenv))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"render", "greet"}, args...))
	rt.BaseURL = srv.URL
	rt.Team = "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01"
	err := root.ExecuteContext(clictx.WithRuntime(context.Background(), &rt))
	if err != nil {
		errOut.WriteString(err.Error())
	}
	res.stdout, res.stderr, res.code = out.String(), errOut.String(), exitcode.FromError(err)
	return res
}

// --strict adds "strict": true next to the placeholders; without it the key is
// absent, so a pre-v0.15.0 platform sees exactly the request it always did.
// The output paths do not change: raw body by default, the envelope under
// --format / --jq.
func TestRenderStrictPayload(t *testing.T) {
	tests := []struct {
		name       string
		rt         config.Runtime
		args       []string
		wantStrict bool
		wantOut    string
	}{
		{name: "default omits strict", args: []string{"--var", "env=prod"}, wantOut: "Hello prod"},
		{name: "strict raw", args: []string{"--strict", "--var", "env=prod"}, wantStrict: true, wantOut: "Hello prod"},
		{name: "strict json", rt: config.Runtime{Format: "json"}, args: []string{"--strict", "--var", "env=prod"}, wantStrict: true, wantOut: `"rendered_body":"Hello prod"`},
		{name: "strict jq", rt: config.Runtime{JQ: ".placeholders_missing"}, args: []string{"--strict", "--var", "env=prod"}, wantStrict: true, wantOut: "[]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRender(t, tt.rt, http.StatusOK, renderOK, tt.args...)
			if res.code != 0 {
				t.Fatalf("exit = %d, stderr=%q", res.code, res.stderr)
			}
			if !strings.Contains(res.stdout, tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", res.stdout, tt.wantOut)
			}
			strict, present := res.body["strict"]
			if tt.wantStrict && strict != true {
				t.Errorf(`body["strict"] = %v, want true; body=%v`, strict, res.body)
			}
			if !tt.wantStrict && present {
				t.Errorf("strict key must be absent without --strict; body=%v", res.body)
			}
			ph, _ := res.body["placeholders"].(map[string]any)
			if ph["env"] != "prod" {
				t.Errorf("placeholders not sent: %v", res.body)
			}
		})
	}
}

// An unresolved reference under --strict is a 422 UNRESOLVED_REFERENCES: exit
// 1, the slugs and request_id on stderr (the problem detail names them), and
// nothing on stdout.
func TestRenderStrictUnresolved(t *testing.T) {
	for _, rt := range []config.Runtime{{}, {Format: "json"}} {
		res := runRender(t, rt, http.StatusUnprocessableEntity, renderUnresolved, "--strict")
		if res.code != exitcode.RuntimeErr {
			t.Errorf("format %q: exit = %d, want %d", rt.Format, res.code, exitcode.RuntimeErr)
		}
		if res.stdout != "" {
			t.Errorf("format %q: stdout must stay empty, got %q", rt.Format, res.stdout)
		}
		for _, want := range []string{"nope", "gone", "req-strict-422"} {
			if !strings.Contains(res.stderr, want) {
				t.Errorf("format %q: stderr missing %q: %q", rt.Format, want, res.stderr)
			}
		}
	}
}

func TestRenderHelpDocumentsStrict(t *testing.T) {
	cmd := newRender(nil, nil)
	f := cmd.Flags().Lookup("strict")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("--strict flag missing or not default false: %+v", f)
	}
	if !strings.Contains(cmd.Long, "--strict") || !strings.Contains(cmd.Long, "@prompt:slug") {
		t.Errorf("Long help does not explain strict mode:\n%s", cmd.Long)
	}
}
