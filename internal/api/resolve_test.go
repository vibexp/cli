package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
)

func TestTeamUUIDPassesThrough(t *testing.T) {
	const v = "dee2f88f-4372-4deb-af2c-021b21b4eb0e"
	got, err := Team(&config.Runtime{Team: v})
	if err != nil || got != v {
		t.Fatalf("Team = %q, %v; want %q", got, err, v)
	}
}

func TestSlugWithoutResolverFailsClosed(t *testing.T) {
	// A slug must never reach a REST path unresolved.
	for name, resolve := range map[string]func(*config.Runtime) (string, error){"Team": Team, "Project": Project} {
		_, err := resolve(&config.Runtime{Team: "my-team-slug", Project: "my-project-slug"})
		if got := exitcode.FromError(err); got != exitcode.RuntimeErr {
			t.Errorf("%s with a slug and no resolver: exit = %d, want 1 (err %v)", name, got, err)
		}
	}
}

func TestTeamMissingIsUsageError(t *testing.T) {
	_, err := Team(&config.Runtime{ContextName: "dev"})
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Fatalf("missing team exit = %d, want 2", got)
	}
	if err == nil || !strings.Contains(err.Error(), "--team") || !strings.Contains(err.Error(), "VIBEXP_TEAM") {
		t.Errorf("error should name all three options: %v", err)
	}
}

func TestProjectMissingIsUsageError(t *testing.T) {
	_, err := Project(&config.Runtime{})
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", got)
	}
}

// Fabricated fixtures: a team reachable only on the second teams page, and a
// project inside it.
const (
	slugTeamID    = "4b1d2c3e-5f60-4718-9a2b-3c4d5e6f7a80"
	slugProjectID = "6e7f8091-a2b3-4c4d-8e5f-60718293a4b5"
)

// slugServer fakes listTeams (two pages) and getProject, counting requests.
func slugServer(t *testing.T, requests *int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Errorf("page_size = %q, want 100", got)
		}
		teams := make([]string, 0, 100)
		if r.URL.Query().Get("page") == "1" {
			for i := 0; i < 100; i++ {
				teams = append(teams, fmt.Sprintf(`{"id":"00000000-0000-4000-8000-%012d","slug":"filler-%d"}`, i, i))
			}
		} else {
			teams = append(teams, `{"id":"`+slugTeamID+`","slug":"acme"}`)
		}
		_, _ = fmt.Fprintf(w, `{"teams":[%s],"total_count":101,"page":1,"page_size":100}`, strings.Join(teams, ","))
	})
	mux.HandleFunc("/api/v1/"+slugTeamID+"/projects/", func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if strings.TrimPrefix(r.URL.Path, "/api/v1/"+slugTeamID+"/projects/") != "web-app" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"project not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + slugProjectID + `","slug":"web-app"}`))
	})
	return httptest.NewServer(mux)
}

// fakeKeyEnv supplies a fabricated API key.
func fakeKeyEnv(k string) string {
	if k == "VIBEXP_API_KEY" {
		return "vxk_fabricated"
	}
	return ""
}

// slugRuntime returns a runtime with the slug resolver installed, as the root
// installs it. No client is built up front.
func slugRuntime(t *testing.T, srv *httptest.Server, team, project string) *config.Runtime {
	t.Helper()
	rt := &config.Runtime{BaseURL: srv.URL, Team: team, Project: project}
	store := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	creds := func() (*cred.Store, error) { return store, nil }
	rt.Slugs = NewSlugResolver(context.Background(), rt, creds, fakeKeyEnv)
	return rt
}

func TestTeamUUIDMakesNoLookup(t *testing.T) {
	var requests int
	srv := slugServer(t, &requests)
	defer srv.Close()
	rt := slugRuntime(t, srv, slugTeamID, slugProjectID)

	if got, err := Team(rt); err != nil || got != slugTeamID {
		t.Fatalf("Team = %q, %v", got, err)
	}
	if got, err := Project(rt); err != nil || got != slugProjectID {
		t.Fatalf("Project = %q, %v", got, err)
	}
	if requests != 0 {
		t.Errorf("a UUID made %d lookup requests, want 0", requests)
	}
}

func TestTeamSlugResolvesOnceAcrossPages(t *testing.T) {
	var requests int
	srv := slugServer(t, &requests)
	defer srv.Close()
	rt := slugRuntime(t, srv, "acme", "")

	for i := 0; i < 2; i++ {
		got, err := Team(rt)
		if err != nil || got != slugTeamID {
			t.Fatalf("Team (call %d) = %q, %v", i+1, got, err)
		}
	}
	if requests != 2 { // two teams pages, then memoized
		t.Errorf("requests = %d, want 2 (both pages, once)", requests)
	}
	if rt.Team != slugTeamID {
		t.Errorf("rt.Team = %q, want the resolved UUID", rt.Team)
	}
}

func TestTeamSlugNotFoundIsUsageError(t *testing.T) {
	var requests int
	srv := slugServer(t, &requests)
	defer srv.Close()

	_, err := Team(slugRuntime(t, srv, "nope", ""))
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Fatalf("unknown slug exit = %d, want 2 (err %v)", got, err)
	}
	if !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "vibexp team list") {
		t.Errorf("error should name the slug and suggest team list: %v", err)
	}
}

func TestProjectSlugResolvesWithinResolvedTeam(t *testing.T) {
	var requests int
	srv := slugServer(t, &requests)
	defer srv.Close()
	rt := slugRuntime(t, srv, "acme", "web-app")

	for i := 0; i < 2; i++ {
		got, err := Project(rt)
		if err != nil || got != slugProjectID {
			t.Fatalf("Project (call %d) = %q, %v", i+1, got, err)
		}
	}
	if requests != 3 { // two teams pages + one project GET, then memoized
		t.Errorf("requests = %d, want 3", requests)
	}
	if got, err := OptionalProject(rt); err != nil || got != slugProjectID {
		t.Errorf("OptionalProject = %q, %v", got, err)
	}
}

func TestProjectSlugNotFoundIsUsageError(t *testing.T) {
	var requests int
	srv := slugServer(t, &requests)
	defer srv.Close()

	_, err := Project(slugRuntime(t, srv, slugTeamID, "gone"))
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Fatalf("unknown project exit = %d, want 2 (err %v)", got, err)
	}
	if !strings.Contains(err.Error(), `"gone"`) || !strings.Contains(err.Error(), "vibexp project list") {
		t.Errorf("error should name the slug and suggest project list: %v", err)
	}
}

func TestProjectLookupServerErrorIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"Forbidden","status":403,"detail":"no access"}`))
	}))
	defer srv.Close()

	_, err := Project(slugRuntime(t, srv, slugTeamID, "web-app"))
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("a non-404 lookup failure should surface the API error, got %T %v", err, err)
	}
}

func TestProjectLookupWithoutIDIsRuntimeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"slug":"web-app"}`))
	}))
	defer srv.Close()

	_, err := Project(slugRuntime(t, srv, slugTeamID, "web-app"))
	if got := exitcode.FromError(err); got != exitcode.RuntimeErr {
		t.Errorf("id-less response exit = %d, want 1 (err %v)", got, err)
	}
}

func TestOptionalProjectUnsetMakesNoLookup(t *testing.T) {
	if got, err := OptionalProject(&config.Runtime{Team: "acme"}); err != nil || got != "" {
		t.Errorf("OptionalProject = %q, %v, want empty", got, err)
	}
}

func TestIsUUIDIsCanonicalOnly(t *testing.T) {
	for v, want := range map[string]bool{
		slugTeamID:               true,
		"{" + slugTeamID + "}":   false,
		"urn:uuid:" + slugTeamID: false,
		"acme":                   false,
		strings.Repeat("a", 36):  false,
	} {
		if got := isUUID(v); got != want {
			t.Errorf("isUUID(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestTeamLookupFailuresAreErrors(t *testing.T) {
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"teams":`))
	}))
	defer garbled.Close()
	if _, err := Team(slugRuntime(t, garbled, "acme", "")); exitcode.FromError(err) != exitcode.RuntimeErr {
		t.Errorf("an undecodable teams page should exit 1, got %v", err)
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	rt := slugRuntime(t, gone, "acme", "")
	gone.Close() // the lookup request itself fails
	if _, err := Team(rt); err == nil {
		t.Error("an unreachable server should fail the lookup")
	}
}

func TestNewRawLeavesRuntimeUntouched(t *testing.T) {
	rt := &config.Runtime{BaseURL: "http://127.0.0.1:1", Team: "acme"}
	store := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if _, err := NewRaw(context.Background(), rt, store, fakeKeyEnv); err != nil {
		t.Fatal(err)
	}
	if rt.Slugs != nil {
		t.Error("NewRaw must not install a slug resolver on the runtime")
	}
}

// countingCreds returns a credential func that counts its calls.
func countingCreds(t *testing.T, calls *int) func() (*cred.Store, error) {
	t.Helper()
	store := &cred.Store{Path: filepath.Join(t.TempDir(), "credentials.json")}
	return func() (*cred.Store, error) {
		*calls++
		return store, nil
	}
}

func TestSlugResolverBuildsItsClientOnceAndLazily(t *testing.T) {
	var requests, credCalls int
	srv := slugServer(t, &requests)
	defer srv.Close()
	rt := &config.Runtime{BaseURL: srv.URL, Team: "acme", Project: "web-app"}
	rt.Slugs = NewSlugResolver(context.Background(), rt, countingCreds(t, &credCalls), fakeKeyEnv)
	if credCalls != 0 {
		t.Fatalf("installing the resolver opened the credential store %d times, want 0", credCalls)
	}

	// Path before client: no RawClient exists, yet both slugs resolve.
	if got, err := Project(rt); err != nil || got != slugProjectID {
		t.Fatalf("Project = %q, %v", got, err)
	}
	if rt.Team != slugTeamID {
		t.Errorf("rt.Team = %q, want the resolved UUID", rt.Team)
	}
	if credCalls != 1 {
		t.Errorf("credential store opened %d times across a team and a project lookup, want 1", credCalls)
	}
}

func TestSlugResolverUnusedForUUIDs(t *testing.T) {
	var credCalls int
	rt := &config.Runtime{Team: slugTeamID, Project: slugProjectID} // no base URL either
	rt.Slugs = NewSlugResolver(context.Background(), rt, countingCreds(t, &credCalls), fakeKeyEnv)
	if _, err := Project(rt); err != nil {
		t.Fatal(err)
	}
	if credCalls != 0 {
		t.Errorf("a UUID opened the credential store %d times, want 0", credCalls)
	}
}

func TestSlugResolverClientErrorsPropagate(t *testing.T) {
	var credCalls int
	rt := &config.Runtime{Team: "acme"} // no base URL
	rt.Slugs = NewSlugResolver(context.Background(), rt, countingCreds(t, &credCalls), fakeKeyEnv)
	for i := 0; i < 2; i++ {
		_, err := Team(rt)
		if got := exitcode.FromError(err); got != exitcode.UsageErr {
			t.Fatalf("missing base URL (call %d): exit = %d, want 2 (err %v)", i+1, got, err)
		}
		if !strings.Contains(err.Error(), "base URL") {
			t.Errorf("error should name the missing base URL: %v", err)
		}
	}
	if credCalls != 1 {
		t.Errorf("a failed client build was retried: %d credential opens, want 1", credCalls)
	}

	rt = &config.Runtime{BaseURL: "http://127.0.0.1:1", Team: "acme"}
	rt.Slugs = NewSlugResolver(context.Background(), rt, func() (*cred.Store, error) {
		return nil, errors.New("credentials unreadable")
	}, fakeKeyEnv)
	if _, err := Team(rt); exitcode.FromError(err) != exitcode.RuntimeErr || !strings.Contains(err.Error(), "credentials unreadable") {
		t.Errorf("a credential store error should surface as exit 1, got %v", err)
	}
}
