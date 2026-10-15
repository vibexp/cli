package resource_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/clictx"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// lockRequest is what the fake server saw.
type lockRequest struct{ method, path, body string }

// lockServer answers every request with status and body, recording each one.
func lockServer(t *testing.T, status int, body string, seen *[]lockRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = append(*seen, lockRequest{r.Method, r.URL.Path, string(b)})
		ct := "application/json"
		if status >= 400 {
			ct = "application/problem+json"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

// itemPathOK is a lockable noun's item path; itemPathNoProject mimics a
// project-scoped noun with no project resolved.
func itemPathOK(_ *config.Runtime, slug string) (string, error) {
	return "/api/v1/t/things/" + slug, nil
}

func itemPathNoProject(*config.Runtime, string) (string, error) {
	return "", exitcode.Usage("no project")
}

func runLockCmd(t *testing.T, srv *httptest.Server, cmd *cobra.Command, format string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Format: format})
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return out.String(), errOut.String(), exitcode.FromError(err)
}

var lockSpec = output.TableSpec{Rows: ".", Columns: append([]output.Column{{Header: "SLUG", Path: ".slug"}}, resource.LockDetailColumns()...)}

const lockedThing = `{"slug":"greet","lock":{"comment":"release agent","locked_at":"2026-10-11T10:15:30Z","locked_by":{"user_id":"u-1","display_name":"Ada"}}}`

func TestLockCommandPostsComment(t *testing.T) {
	var seen []lockRequest
	srv := lockServer(t, http.StatusOK, lockedThing, &seen)
	defer srv.Close()

	cmd := resource.NewLockCommand("thing", credResolver(t), fakeEnv, itemPathOK, lockSpec)
	out, errOut, code := runLockCmd(t, srv, cmd, "text", "greet", "--comment", "  release agent ")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	want := lockRequest{http.MethodPost, "/api/v1/t/things/greet/lock", `{"comment":"release agent"}`}
	if len(seen) != 1 || seen[0] != want {
		t.Fatalf("requests = %+v, want [%+v]", seen, want)
	}
	if got := strings.TrimRight(out, "\n"); got != "greet\tlocked\tAda\t2026-10-11T10:15:30Z\trelease agent" {
		t.Errorf("rendered = %q", got)
	}
}

func TestLockCommandRequiresComment(t *testing.T) {
	for _, args := range [][]string{{"greet"}, {"greet", "--comment", "   "}} {
		var seen []lockRequest
		srv := lockServer(t, http.StatusOK, lockedThing, &seen)
		cmd := resource.NewLockCommand("thing", credResolver(t), fakeEnv, itemPathOK, lockSpec)
		_, errOut, code := runLockCmd(t, srv, cmd, "json", args...)
		srv.Close()
		if code != exitcode.UsageErr || len(seen) != 0 {
			t.Errorf("%v: exit = %d, requests = %d; want exit 2 and no request", args, code, len(seen))
		}
		if !strings.Contains(errOut, "--comment is required") {
			t.Errorf("%v: stderr = %q", args, errOut)
		}
	}
}

func TestUnlockCommandDeletes(t *testing.T) {
	var seen []lockRequest
	srv := lockServer(t, http.StatusOK, `{"slug":"greet","lock":null}`, &seen)
	defer srv.Close()

	cmd := resource.NewUnlockCommand("thing", credResolver(t), fakeEnv, itemPathOK, lockSpec)
	out, errOut, code := runLockCmd(t, srv, cmd, "json", "greet")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if len(seen) != 1 || seen[0].method != http.MethodDelete || seen[0].path != "/api/v1/t/things/greet/lock" {
		t.Fatalf("requests = %+v", seen)
	}
	// JSON passes the raw body through, the null lock included.
	if !strings.Contains(out, `"lock":null`) {
		t.Errorf("json output = %q", out)
	}
}

func TestLockCommandsNeedTheItemPath(t *testing.T) {
	tests := []struct {
		cmd  *cobra.Command
		args []string
	}{
		{resource.NewLockCommand("thing", credResolver(t), fakeEnv, itemPathNoProject, lockSpec), []string{"greet", "--comment", "x"}},
		{resource.NewUnlockCommand("thing", credResolver(t), fakeEnv, itemPathNoProject, lockSpec), []string{"greet"}},
	}
	for _, tc := range tests {
		var seen []lockRequest
		srv := lockServer(t, http.StatusOK, lockedThing, &seen)
		_, _, code := runLockCmd(t, srv, tc.cmd, "json", tc.args...)
		srv.Close()
		if code != exitcode.UsageErr || len(seen) != 0 {
			t.Errorf("%s: exit = %d, requests = %d; want exit 2 and no request", tc.cmd.Name(), code, len(seen))
		}
	}
}

// 409s on lock/unlock carry a server detail that already reads well; the CLI
// prints it with the request id and exits 1.
func TestLockConflictsRenderServerDetail(t *testing.T) {
	tests := []struct {
		cmd    func() *cobra.Command
		args   []string
		code   string
		detail string
	}{
		{func() *cobra.Command {
			return resource.NewLockCommand("thing", credResolver(t), fakeEnv, itemPathOK, lockSpec)
		}, []string{"greet", "--comment", "x"}, "RESOURCE_LOCKED", "this prompt is already locked"},
		{func() *cobra.Command {
			return resource.NewUnlockCommand("thing", credResolver(t), fakeEnv, itemPathOK, lockSpec)
		}, []string{"greet"}, "RESOURCE_NOT_LOCKED", "this prompt is not locked"},
	}
	for _, tc := range tests {
		var seen []lockRequest
		srv := lockServer(t, http.StatusConflict,
			`{"status":409,"code":"`+tc.code+`","detail":"`+tc.detail+`","request_id":"req-409"}`, &seen)
		_, errOut, code := runLockCmd(t, srv, tc.cmd(), "json", tc.args...)
		srv.Close()
		if code != exitcode.RuntimeErr {
			t.Errorf("%s: exit = %d, want 1", tc.code, code)
		}
		for _, want := range []string{tc.detail, "request_id: req-409"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("%s: stderr %q missing %q", tc.code, errOut, want)
			}
		}
		if strings.Contains(errOut, "unlock with") {
			t.Errorf("%s: a 409 must not carry the write-lock hint: %q", tc.code, errOut)
		}
	}
}
