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
		case "/api/v1/" + acmeTeamID + "/scoped":
			_, _ = w.Write([]byte(`{"ok":true}`))
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

func TestAPITeamPlaceholderResolvesSlug(t *testing.T) {
	var cap slugCapture
	srv := slugServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	if _, errOut, code := runAuth(t, cfg, cs, nil, "", "--team", "acme", "api", "GET", "/api/v1/{team}/scoped"); code != 0 {
		t.Fatalf("api {team} with a slug exit = %d, stderr=%q", code, errOut)
	}
	want := []string{"/api/v1/teams", "/api/v1/" + acmeTeamID + "/scoped"}
	if strings.Join(cap.paths, " ") != strings.Join(want, " ") {
		t.Errorf("requests = %v, want %v", cap.paths, want)
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

// guardServer resolves the "acme"/"web-app" slugs and answers any other path
// under the team UUID with an empty, list-shaped body. A path that still
// carries a slug 404s.
func guardServer(t *testing.T, paths *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/teams":
			_, _ = w.Write([]byte(`{"teams":[{"id":"` + acmeTeamID + `","slug":"acme","name":"Acme"}],"total_count":1,"page":1,"page_size":100}`))
		case r.URL.Path == "/api/v1/"+acmeTeamID+"/projects/web-app":
			_, _ = w.Write([]byte(`{"id":"` + webAppProjID + `","slug":"web-app"}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/"+acmeTeamID+"/"):
			_, _ = w.Write([]byte(`{"page":1,"per_page":50,"total_count":0,"total_pages":1}`))
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"not found"}`))
		}
	}))
}

// TestTeamScopedCommandsResolveSlugs guards every team-scoped command against
// sending a slug where REST takes a UUID, whatever order the command builds
// its client and path in.
func TestTeamScopedCommandsResolveSlugs(t *testing.T) {
	const ownerID = "0f1e2d3c-4b5a-4968-8776-655443322110"
	for _, args := range [][]string{
		{"prompt", "get", "greet"},
		{"prompt", "list"},
		{"memory", "list"},
		{"memory", "list", "--project", "web-app"},
		{"memory", "get", ownerID},
		{"blueprint", "get", "bp", "--project", "web-app"},
		{"artifact", "get", "art", "--project", "web-app"},
		{"feed", "list"},
		{"search", "hello"},
		{"search", "hello", "--project", "web-app"},
		{"attachment", "list", "--owner-id", ownerID},
		{"relations", "list", "memory", ownerID},
		{"metadata", "keys", "--type", "memories"},
		{"metadata", "keys", "--type", "memories", "--project", "web-app"},
		{"project", "list"},
		{"api", "GET", "/api/v1/{team}/scoped"},
	} {
		name := strings.Join(args, " ")
		t.Run(name, func(t *testing.T) {
			var paths []string
			srv := guardServer(t, &paths)
			defer srv.Close()
			cfg, cs := apiFixture(t, srv.URL, "")

			argv := append([]string{"--format", "json", "--team", "acme"}, args...)
			if _, errOut, code := runAuth(t, cfg, cs, nil, "", argv...); code != 0 {
				t.Fatalf("exit = %d, stderr=%q, requests=%v", code, errOut, paths)
			}
			if len(paths) < 2 || paths[0] != "/api/v1/teams" {
				t.Fatalf("requests = %v, want one team lookup then the command's own", paths)
			}
			for _, p := range paths[1:] {
				if !strings.HasPrefix(p, "/api/v1/"+acmeTeamID+"/") {
					t.Errorf("request path %q is not under the team UUID (requests %v)", p, paths)
				}
			}
		})
	}
}
