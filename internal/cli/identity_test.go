package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// identityServer emulates /auth/me, /teams, and /{team}/projects. It records
// the last teams-list query so pagination mapping can be asserted.
func identityServer(t *testing.T, gotQuery *url.Values) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u-1","email":"dev@example.com","name":"Dev User","created_at":"2026-01-01T00:00:00Z","is_instance_admin":false,"onboarding_completed":true}`))
	})
	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		if gotQuery != nil {
			*gotQuery = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"teams":[
			{"slug":"acme","name":"Acme","member_count":4,"role":"owner","permissions":["read","write"]},
			{"slug":"beta","name":"Beta","member_count":1,"role":"viewer","permissions":["read"]}
		],"page":1,"per_page":50,"total_count":2,"total_pages":1}`))
	})
	mux.HandleFunc("/api/v1/the-team/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[
			{"slug":"proj-a","name":"Project A","description":"first","updated_at":"2026-02-01T00:00:00Z"}
		],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
	})
	return httptest.NewServer(mux)
}

func TestWhoamiJSONByteIdentical(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "whoami")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, `"email":"dev@example.com"`) || !strings.Contains(out, `"id":"u-1"`) {
		t.Errorf("whoami json missing identity: %q", out)
	}
}

func TestWhoamiDefaultTSV(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	// Non-TTY default → TSV of the single identity row (no header, no color).
	out, _, code := runAuth(t, cfg, cs, nil, "", "whoami")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("piped output must have no color: %q", out)
	}
	if !strings.Contains(out, "u-1") || !strings.Contains(out, "Dev User") || !strings.Contains(out, "dev@example.com") {
		t.Errorf("whoami TSV missing fields: %q", out)
	}
}

func TestTeamListColumnsUsePermissions(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	// Force table to check headers + the permissions summary column.
	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "table", "team", "list")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	for _, want := range []string{"SLUG", "NAME", "MEMBERS", "PERMISSIONS", "acme", "read,write", "beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("team list missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ROLE") {
		t.Error("team list must summarize permissions, not role")
	}
}

func TestTeamListPaginationFlagsMapToQuery(t *testing.T) {
	var q url.Values
	srv := identityServer(t, &q)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "team", "list", "--limit", "5", "--page", "2", "--offset", "10"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if q.Get("limit") != "5" || q.Get("page") != "2" || q.Get("offset") != "10" {
		t.Errorf("pagination not mapped to query: %v", q)
	}
}

func TestProjectListResolvesTeamFromContext(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team") // default team on context

	out, _, code := runAuth(t, cfg, cs, nil, "", "project", "list")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	if !strings.Contains(out, "proj-a") {
		t.Errorf("project list missing project: %q", out)
	}
}

func TestProjectListTeamViaFlag(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "") // no context default

	out, _, code := runAuth(t, cfg, cs, nil, "", "--team", "the-team", "project", "list")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	if !strings.Contains(out, "proj-a") {
		t.Errorf("project list --team missing project: %q", out)
	}
}

func TestProjectListNoTeamIsUsageError(t *testing.T) {
	srv := identityServer(t, nil)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "") // no team anywhere

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "project", "list")
	if code != exitcode.UsageErr {
		t.Fatalf("no-team exit = %d, want 2", code)
	}
	for _, want := range []string{"--team", "VIBEXP_TEAM", "set-context"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("error should name %q: %q", want, errOut)
		}
	}
}
