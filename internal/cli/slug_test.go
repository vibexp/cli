package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// Fabricated ids behind the "acme" team and "web-app" project slugs.
const (
	acmeTeamID   = "5d6e7f80-9a1b-4c2d-8e3f-405162738495"
	webAppProjID = "7a8b9c0d-1e2f-4a3b-9c4d-5e6f7a8b9c0d"
)

// slugCapture records the requests the slug server saw.
type slugCapture struct {
	paths         []string
	memoriesQuery string
}

// slugServer serves the team list, one project by slug, and the team's
// prompts/memories under the team UUID only — a slug in a path 404s.
func slugServer(t *testing.T, cap *slugCapture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cap.paths = append(cap.paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/teams":
			_, _ = w.Write([]byte(`{"teams":[{"id":"` + acmeTeamID + `","slug":"acme","name":"Acme"}],"total_count":1,"page":1,"page_size":100}`))
		case "/api/v1/" + acmeTeamID + "/projects/web-app":
			_, _ = w.Write([]byte(`{"id":"` + webAppProjID + `","slug":"web-app"}`))
		case "/api/v1/" + acmeTeamID + "/prompts/greet":
			_, _ = w.Write([]byte(`{"id":"pr-1","slug":"greet","title":"Greet","body":"hi"}`))
		case "/api/v1/" + acmeTeamID + "/memories":
			cap.memoriesQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"memories":[],"page":1,"per_page":50,"total_count":0,"total_pages":1}`))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"not found"}`))
		}
	})
	return httptest.NewServer(mux)
}

func TestTeamSlugResolvesToUUIDPath(t *testing.T) {
	var cap slugCapture
	srv := slugServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--team", "acme", "prompt", "get", "greet")
	if code != 0 {
		t.Fatalf("prompt get --team <slug> exit = %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(out, `"slug":"greet"`) {
		t.Errorf("prompt not rendered: %q", out)
	}
	want := []string{"/api/v1/teams", "/api/v1/" + acmeTeamID + "/prompts/greet"}
	if strings.Join(cap.paths, " ") != strings.Join(want, " ") {
		t.Errorf("requests = %v, want %v (one lookup, then the UUID path)", cap.paths, want)
	}
}

func TestProjectSlugResolvesForListFilter(t *testing.T) {
	var cap slugCapture
	srv := slugServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "acme") // slug as the context default

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "web-app", "memory", "list")
	if code != 0 {
		t.Fatalf("memory list --project <slug> exit = %d, stderr=%q", code, errOut)
	}
	if !strings.Contains(cap.memoriesQuery, "project_id="+webAppProjID) {
		t.Errorf("project filter should carry the UUID: %q", cap.memoriesQuery)
	}
}

func TestUnknownTeamSlugIsUsageError(t *testing.T) {
	var cap slugCapture
	srv := slugServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--team", "nope", "prompt", "get", "greet")
	if code != exitcode.UsageErr {
		t.Fatalf("unknown slug exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, `"nope"`) || !strings.Contains(errOut, "vibexp team list") {
		t.Errorf("error should name the slug and suggest team list: %q", errOut)
	}
}

func TestUnscopedCommandSkipsTeamLookup(t *testing.T) {
	var cap slugCapture
	srv := slugServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "stale-slug") // a default that matches no team

	if _, errOut, code := runAuth(t, cfg, cs, nil, "", "team", "list"); code != 0 {
		t.Fatalf("team list exit = %d, stderr=%q", code, errOut)
	}
	if len(cap.paths) != 1 || !strings.HasPrefix(cap.paths[0], "/api/v1/teams") {
		t.Errorf("team list should make only its own request, got %v", cap.paths)
	}
}
