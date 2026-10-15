package statuscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

// Fabricated ids: the team behind the "acme" slug, and one custom status.
const (
	teamID   = "5d6e7f80-9a1b-4c2d-8e3f-405162738495"
	statusID = "3c4d5e6f-7a8b-4c9d-8e0f-1a2b3c4d5e6f"
)

const listBody = `{"statuses":[` +
	`{"resource_type":"artifact","slug":"active","name":"Active","is_system":true,"is_live":true,"usage_count":4},` +
	`{"resource_type":"artifact","slug":"draft","name":"Draft","is_system":true,"is_live":false,"usage_count":1},` +
	`{"id":"` + statusID + `","resource_type":"artifact","slug":"in-review","name":"In review","is_system":false,"is_live":true,"usage_count":2}]}`

type request struct{ method, path, query, body string }

// fakeServer answers the team lookup and the status endpoints, recording every
// status request. reply overrides the response for a method other than GET.
type fakeServer struct {
	*httptest.Server
	seen  []request
	reply func(w http.ResponseWriter, r *http.Request)
}

func newServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/teams" {
			_, _ = w.Write([]byte(`{"teams":[{"id":"` + teamID + `","slug":"acme"}],"total_count":1}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.seen = append(f.seen, request{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		switch {
		case r.Method != http.MethodGet && f.reply != nil:
			f.reply(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/"+teamID+"/statuses":
			_, _ = w.Write([]byte(listBody))
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + statusID + `","resource_type":"artifact","slug":"in-review","name":"In review","is_system":false,"is_live":true,"usage_count":0}`))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"id":"` + statusID + `","resource_type":"artifact","slug":"in-review","name":"Under review","is_system":false,"is_live":true,"usage_count":2}`))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"reassigned_count":2,"reassigned_to":"active"}`))
		default:
			problem(w, http.StatusNotFound, "not found")
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"title":"Error","status":` + strconv.Itoa(status) + `,"detail":"` + detail + `","request_id":"req-fab-1"}`))
}

// run executes `status <args>` under a stand-in root that declares the global
// --format/--jq flags. format is the runtime format ("" → table, non-TTY).
// The returned stderr carries the command error, as the real root prints it.
func run(t *testing.T, srv *fakeServer, format string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	getenv := func(k string) string {
		if k == cred.EnvAPIKey {
			return "vxk_fabricated"
		}
		return ""
	}
	creds := func() (*cred.Store, error) { return &cred.Store{Path: path}, nil }
	root := &cobra.Command{Use: "vibexp", SilenceUsage: true, SilenceErrors: true}
	var f, jq string
	root.PersistentFlags().StringVar(&f, "format", "", "")
	root.PersistentFlags().StringVar(&jq, "jq", "", "")
	root.AddCommand(New(creds, getenv))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"status"}, args...))
	ctx := context.Background()
	rt := &config.Runtime{BaseURL: srv.URL, Team: "acme", Format: format}
	rt.Slugs = api.NewSlugResolver(ctx, rt, creds, getenv) // as the root does
	err := root.ExecuteContext(clictx.WithRuntime(ctx, rt))
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return out.String(), errOut.String(), exitcode.FromError(err)
}

func TestListQueriesTypeAndKeepsServerOrder(t *testing.T) {
	srv := newServer(t)
	out, errOut, code := run(t, srv, "", "list", "--type", "artifact")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if len(srv.seen) != 1 || srv.seen[0].path != "/api/v1/"+teamID+"/statuses" || srv.seen[0].query != "resource_type=artifact" {
		t.Fatalf("requests = %+v", srv.seen)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{
		"active\tActive\ttrue\ttrue\t4\t",
		"draft\tDraft\ttrue\tfalse\t1\t",
		"in-review\tIn review\tfalse\ttrue\t2\t" + statusID,
	}
	if len(lines) != len(want) { // piped: headerless TSV in column order
		t.Fatalf("rows = %q", out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("row %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestListJSONIsTheRawBody(t *testing.T) {
	srv := newServer(t)
	out, _, code := run(t, srv, "json", "list", "--type", "artifact")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var got, want any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	_ = json.Unmarshal([]byte(listBody), &want)
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("json = %s, want %s", g, w)
	}
}

func TestInvalidTypeIsUsageErrorWithoutRequest(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"list", "--type", "artifacts"},
		{"create", "--type", "widget", "--slug", "x", "--name", "X"},
		{"rename", statusID, "--type", "widget", "--name", "X"},
		{"delete", statusID, "--type", "widget", "--yes"},
	} {
		srv := newServer(t)
		_, errOut, code := run(t, srv, "", args...)
		if code != exitcode.UsageErr {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
		if !strings.Contains(errOut, "prompt, artifact, blueprint, memory") {
			t.Errorf("%v: message should name the valid types: %q", args, errOut)
		}
		if len(srv.seen) != 0 {
			t.Errorf("%v: no request should be sent, got %+v", args, srv.seen)
		}
	}
}

func TestCreateSendsExactlyTheThreeFields(t *testing.T) {
	srv := newServer(t)
	out, errOut, code := run(t, srv, "", "create", "--type", "artifact", "--slug", "in-review", "--name", "In review")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if len(srv.seen) != 1 || srv.seen[0].method != http.MethodPost || srv.seen[0].path != "/api/v1/"+teamID+"/statuses" {
		t.Fatalf("requests = %+v", srv.seen)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(srv.seen[0].body), &body)
	if len(body) != 3 || body["resource_type"] != "artifact" || body["slug"] != "in-review" || body["name"] != "In review" {
		t.Errorf("body = %s", srv.seen[0].body)
	}
	if !strings.Contains(out, "in-review\tIn review\tfalse\ttrue\t0\t"+statusID) {
		t.Errorf("output = %q", out)
	}
}

func TestCreateRequiresSlugAndName(t *testing.T) {
	srv := newServer(t)
	if _, _, code := run(t, srv, "", "create", "--type", "artifact", "--slug", "x"); code != exitcode.UsageErr {
		t.Errorf("exit = %d, want 2", code)
	}
	if len(srv.seen) != 0 {
		t.Errorf("no request should be sent: %+v", srv.seen)
	}
}

func TestCreateConflictSurfacesDetail(t *testing.T) {
	srv := newServer(t)
	srv.reply = func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusConflict, "a status with slug in-review already exists")
	}
	_, errOut, code := run(t, srv, "", "create", "--type", "artifact", "--slug", "in-review", "--name", "In review")
	if code != exitcode.RuntimeErr {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "already exists") || !strings.Contains(errOut, "req-fab-1") {
		t.Errorf("stderr should carry the detail and request_id: %q", errOut)
	}
}

func TestRenameBySlugResolvesThenPatchesNameOnly(t *testing.T) {
	srv := newServer(t)
	out, errOut, code := run(t, srv, "", "rename", "in-review", "--type", "artifact", "--name", "Under review")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if len(srv.seen) != 2 || srv.seen[0].method != http.MethodGet || srv.seen[0].query != "resource_type=artifact" {
		t.Fatalf("requests = %+v, want the resolving GET then the PATCH", srv.seen)
	}
	p := srv.seen[1]
	if p.method != http.MethodPatch || p.path != "/api/v1/"+teamID+"/statuses/"+statusID || p.body != `{"name":"Under review"}` {
		t.Errorf("patch = %+v", p)
	}
	if !strings.Contains(out, "Under review") {
		t.Errorf("output = %q", out)
	}
}

func TestRenameByIDNeedsNoLookup(t *testing.T) {
	srv := newServer(t)
	if _, errOut, code := run(t, srv, "json", "rename", statusID, "--name", "Under review"); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if len(srv.seen) != 1 || srv.seen[0].method != http.MethodPatch {
		t.Errorf("requests = %+v, want only the PATCH", srv.seen)
	}
}

func TestRenameRejections(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		gets int
	}{
		{[]string{"rename", "active", "--type", "artifact", "--name", "X"}, "system statuses cannot be renamed or deleted", 1},
		{[]string{"rename", "gone", "--type", "artifact", "--name", "X"}, `no custom artifact status with slug "gone"`, 1},
		{[]string{"rename", "in-review", "--name", "X"}, "--type is needed", 0},
		{[]string{"rename", "in-review", "--type", "artifact"}, "--name is required", 0},
	} {
		srv := newServer(t)
		_, errOut, code := run(t, srv, "", tc.args...)
		if code != exitcode.UsageErr {
			t.Errorf("%v: exit = %d, want 2", tc.args, code)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: stderr = %q, want %q", tc.args, errOut, tc.want)
		}
		if len(srv.seen) != tc.gets {
			t.Errorf("%v: requests = %+v, want %d lookups and no write", tc.args, srv.seen, tc.gets)
		}
	}
}

func TestDeleteSummarizesReassignment(t *testing.T) {
	srv := newServer(t)
	out, errOut, code := run(t, srv, "", "delete", "in-review", "--type", "artifact", "--yes")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if out != "" {
		t.Errorf("stdout should stay empty, got %q", out)
	}
	if !strings.Contains(errOut, "Deleted status in-review; 2 resources moved to active.") {
		t.Errorf("stderr = %q", errOut)
	}
	if last := srv.seen[len(srv.seen)-1]; last.method != http.MethodDelete || last.path != "/api/v1/"+teamID+"/statuses/"+statusID {
		t.Errorf("delete = %+v", last)
	}
}

func TestDeleteWithFormatRendersRawBody(t *testing.T) {
	srv := newServer(t)
	out, _, code := run(t, srv, "json", "--format", "json", "delete", statusID, "--yes")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, `"reassigned_count":2`) {
		t.Errorf("stdout = %q", out)
	}
}

func TestDeleteRefusesNonInteractiveWithoutYes(t *testing.T) {
	srv := newServer(t)
	_, errOut, code := run(t, srv, "", "delete", "in-review", "--type", "prompt")
	if code != exitcode.UsageErr {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "--yes") || !strings.Contains(errOut, "move to draft") {
		t.Errorf("stderr = %q", errOut)
	}
	if len(srv.seen) != 0 {
		t.Errorf("no request should be sent: %+v", srv.seen)
	}
}

func TestDeleteLockedSurfacesDetail(t *testing.T) {
	srv := newServer(t)
	srv.reply = func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusLocked, "HAS_LOCKED_RESOURCES: 1 locked resource carries this status")
	}
	_, errOut, code := run(t, srv, "", "delete", statusID, "--yes")
	if code != exitcode.RuntimeErr {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "locked resource") || !strings.Contains(errOut, "req-fab-1") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestDefaultStatus(t *testing.T) {
	for typ, want := range map[string]string{"prompt": "draft", "memory": "active", "artifact": "active"} {
		if got := defaultStatus(typ); got != want {
			t.Errorf("defaultStatus(%q) = %q, want %q", typ, got, want)
		}
	}
	if got := defaultStatus(""); !strings.Contains(got, "draft for prompts") {
		t.Errorf("defaultStatus(\"\") = %q", got)
	}
}
