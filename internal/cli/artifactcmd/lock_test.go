package artifactcmd

import (
	"bytes"
	"context"
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

// These drive lock, unlock and list --locked through this package's command
// group (Sonar measures coverage per package); the shared behaviour is covered
// in internal/cli/resource. All data is fabricated.

const (
	testTeam    = "0f5e0a1c-7d2b-4c3e-9a41-5b6c7d8e9f01"
	testProject = "3a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c61"
)

const lockListBody = `{"artifacts":[{"slug":"greet","lock":{"comment":"c","locked_at":"2026-10-11T10:15:30Z","locked_by":{"display_name":"Ada"}}},{"slug":"other","lock":null}],"page":1,"per_page":20,"total_count":2,"total_pages":1}`

// runLock runs `artifact <args>` against a server answering every request with
// lockListBody, and returns stdout, the requests seen (method + URI) and the
// exit code.
func runLock(t *testing.T, project string, args ...string) (string, []string, int) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(lockListBody))
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
	cmd := New(resolve, getenv)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	ctx := clictx.WithRuntime(context.Background(), &config.Runtime{BaseURL: srv.URL, Team: testTeam, Project: project, Format: "text"})
	err := cmd.ExecuteContext(ctx)
	return out.String(), seen, exitcode.FromError(err)
}

func TestLockVerbs(t *testing.T) {
	item := "/api/v1/" + testTeam + "/artifacts/" + testProject + "/greet" + "/lock"
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"lock", "greet", "--comment", "in use"}, "POST " + item},
		{[]string{"unlock", "greet"}, "DELETE " + item},
	}
	for _, tc := range tests {
		_, seen, code := runLock(t, testProject, tc.args...)
		if code != 0 || len(seen) != 1 || seen[0] != tc.want {
			t.Errorf("%v: exit = %d, requests = %v; want [%s]", tc.args, code, seen, tc.want)
		}
	}
	if _, seen, code := runLock(t, testProject, "lock", "greet"); code != exitcode.UsageErr || len(seen) != 0 {
		t.Errorf("lock without --comment: exit = %d, requests = %v", code, seen)
	}
}

func TestListLockedShowsLockColumn(t *testing.T) {
	out, seen, code := runLock(t, "", "list", "--locked")
	if code != 0 || len(seen) != 1 || !strings.Contains(seen[0], "locked=true") {
		t.Fatalf("exit = %d, requests = %v", code, seen)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "\tlocked") || !strings.HasSuffix(lines[1], "\t") {
		t.Errorf("LOCKED must be the last column, empty when unlocked:\n%q", out)
	}
}
func TestLockVerbsNeedAProject(t *testing.T) {
	for _, args := range [][]string{{"lock", "greet", "--comment", "x"}, {"unlock", "greet"}} {
		if _, seen, code := runLock(t, "", args...); code != exitcode.UsageErr || len(seen) != 0 {
			t.Errorf("%v: exit = %d, requests = %v; want exit 2, no request", args, code, seen)
		}
	}
}
