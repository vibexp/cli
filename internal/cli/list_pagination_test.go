package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// pagedArtifactServer serves a fabricated team with `total` artifacts, paged
// the way the platform pages them: per_page defaults to 2 when no limit is
// sent (standing in for the server's own default), and a limit above 100 is
// rejected with a 400. Every requested page number is recorded.
func pagedArtifactServer(t *testing.T, total int, pages *[]int) *httptest.Server {
	t.Helper()
	return clampedArtifactServer(t, total, 0, pages)
}

// clampedArtifactServer is pagedArtifactServer serving at most clamp items per
// page (0 = no clamp) whatever limit is asked for, reporting the clamped
// per_page the way a server enforcing its own maximum does.
func clampedArtifactServer(t *testing.T, total, clamp int, pages *[]int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/"+testTeamID+"/artifacts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		perPage := 2
		if l := q.Get("limit"); l != "" {
			perPage, _ = strconv.Atoi(l)
		}
		if perPage > 100 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"title":"Bad Request","status":400,"detail":"limit must be between 1 and 100","request_id":"req-400"}`))
			return
		}
		if clamp > 0 && perPage > clamp {
			perPage = clamp
		}
		page := 1
		if p := q.Get("page"); p != "" {
			page, _ = strconv.Atoi(p)
		}
		*pages = append(*pages, page)
		var items []string
		for i := (page - 1) * perPage; i < page*perPage && i < total; i++ {
			items = append(items, fmt.Sprintf(`{"id":"a%d","slug":"art-%d","title":"T%d","project_id":"`+testProjectID+`","updated_at":"2026-02-01T00:00:00Z"}`, i+1, i+1, i+1))
		}
		totalPages := (total + perPage - 1) / perPage
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"artifacts":[%s],"page":%d,"per_page":%d,"total_count":%d,"total_pages":%d}`,
			strings.Join(items, ","), page, perPage, total, totalPages)
	})
	return httptest.NewServer(mux)
}

func TestListLimitHelpNamesServerMax(t *testing.T) {
	cfg, cs := apiFixture(t, "http://127.0.0.1:0", testTeamID)
	out, _, code := runAuth(t, cfg, cs, nil, "", "artifact", "list", "--help")
	if code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	for _, want := range []string{"server max 100", "--all"} {
		if !strings.Contains(out, want) {
			t.Errorf("artifact list --help missing %q:\n%s", want, out)
		}
	}
}

func TestListWarnsOnStderrWhenMorePagesExist(t *testing.T) {
	var pages []int
	srv := pagedArtifactServer(t, 5, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "artifact", "list")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if want := "showing page 1 of 3 (2 of 5); use --page, --limit (max 100) or --all"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want hint %q", errOut, want)
	}
	// stdout stays the raw page body — the hint never leaks into piped data.
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("stdout is not the JSON page body: %v\n%s", err, out)
	}
	if strings.Contains(out, "showing page") {
		t.Error("the hint leaked onto stdout")
	}
}

func TestListDoesNotWarnWhenEverythingFit(t *testing.T) {
	var pages []int
	srv := pagedArtifactServer(t, 5, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "artifact", "list", "--limit", "10")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if strings.Contains(errOut, "showing page") {
		t.Errorf("no hint expected when everything fit, got %q", errOut)
	}
}

func TestListAllMergesEveryPage(t *testing.T) {
	var pages []int
	srv := pagedArtifactServer(t, 5, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "artifact", "list", "--all", "--limit", "2")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("--all output is not one JSON array of items: %v\n%s", err, out)
	}
	if len(items) != 5 || items[0]["id"] != "a1" || items[4]["id"] != "a5" {
		t.Errorf("merged items = %+v, want a1..a5 in order", items)
	}
	if fmt.Sprint(pages) != "[1 2 3]" {
		t.Errorf("pages fetched = %v, want [1 2 3]", pages)
	}
	if strings.Contains(errOut, "showing page") {
		t.Errorf("--all fetched everything; no hint expected, got %q", errOut)
	}
}

func TestListAllRendersTableRows(t *testing.T) {
	var pages []int
	srv := pagedArtifactServer(t, 3, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	// The merged array has no envelope key, so the table must still find rows.
	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "text", "artifact", "list", "--all", "--limit", "2")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, slug := range []string{"art-1", "art-2", "art-3"} {
		if !strings.Contains(out, slug) {
			t.Errorf("table output missing %s:\n%s", slug, out)
		}
	}
}

func TestListAllRejectsPageAndOffset(t *testing.T) {
	cfg, cs := apiFixture(t, "http://127.0.0.1:0", testTeamID)
	for _, extra := range [][]string{{"--page", "2"}, {"--offset", "10"}} {
		args := append([]string{"artifact", "list", "--all"}, extra...)
		if _, _, code := runAuth(t, cfg, cs, nil, "", args...); code != exitcode.UsageErr {
			t.Errorf("--all %v exit = %d, want 2", extra, code)
		}
	}
}

func TestListServerBadRequestIsUsageError(t *testing.T) {
	var pages []int
	srv := pagedArtifactServer(t, 5, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	for _, extra := range [][]string{nil, {"--all"}} {
		args := append([]string{"artifact", "list", "--limit", "200"}, extra...)
		_, errOut, code := runAuth(t, cfg, cs, nil, "", args...)
		if code != exitcode.UsageErr {
			t.Errorf("%v: 400 exit = %d, want 2", args, code)
		}
		for _, want := range []string{"limit must be between 1 and 100", "req-400"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("%v: stderr %q missing %q", args, errOut, want)
			}
		}
	}
}

func TestListAllWalksEnvelopedPrompts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/"+testTeamID+"/prompts", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 1:
			_, _ = w.Write([]byte(`{"data":{"prompts":[{"id":"p1","slug":"one"},{"id":"p2","slug":"two"}],"page":1,"per_page":2,"total_pages":2,"total_count":3},"status":"success"}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"prompts":[{"id":"p3","slug":"three"}],"page":2,"per_page":2,"total_pages":2,"total_count":3},"status":"success"}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "prompt", "list", "--all", "--limit", "2")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 3 {
		t.Fatalf("want 3 merged prompts, got %v (%v)\n%s", len(items), err, out)
	}
}

func TestSearchWarnsOnStderrWhenMorePagesExist(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/"+testTeamID+"/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"type":"memory","title":"x"}],"page":1,"per_page":1,"total_pages":4,"total_count":4}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "search", "anything")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if want := "showing page 1 of 4 (1 of 4); use --page or --limit (max 100)"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want hint %q", errOut, want)
	}
}

// The attachments endpoint takes only owner_type/owner_id and returns every item
// with no page metadata, so `attachment list` binds no pagination flags. A raw `api --paginate` walk of it must still stop after
// one request instead of looping forever.
func TestAttachmentListIsUnpaginated(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/"+testTeamID+"/attachments", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits > 5 {
			t.Error("--paginate kept walking an unpaginated endpoint")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attachments":[{"id":"x1"},{"id":"x2"}],"total_count":2,"total_size_bytes":10}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	for _, flag := range []string{"--limit=5", "--page=2", "--offset=1", "--all"} {
		if _, _, code := runAuth(t, cfg, cs, nil, "", "attachment", "list", "--owner-id", "o-1", flag); code != exitcode.UsageErr {
			t.Errorf("%s: exit = %d, want 2 (unknown flag)", flag, code)
		}
	}

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "api", "GET",
		"/api/v1/"+testTeamID+"/attachments?owner_type=artifact&owner_id=o-1&limit=1", "--paginate")
	if code != 0 {
		t.Fatalf("--paginate: exit = %d, stderr=%q", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 2 || hits != 1 {
		t.Errorf("--paginate: want the 2 items from 1 request, got %d requests: %s", hits, out)
	}
}

// A server that serves fewer items per page than asked (it clamps the limit)
// reports its real per_page; --all must walk to the end, not stop after the
// first "short" page.
func TestListAllFollowsServerPerPage(t *testing.T) {
	var pages []int
	srv := clampedArtifactServer(t, 5, 2, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, testTeamID)

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "artifact", "list", "--all", "--limit", "50")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 5 {
		t.Errorf("want all 5 artifacts, got %s", out)
	}
	if fmt.Sprint(pages) != "[1 2 3]" {
		t.Errorf("pages fetched = %v, want [1 2 3]", pages)
	}
}

// listTeams reads page_size (default 20) and ignores limit, and reports no
// total_pages. --limit must be sent as page_size on every page --all walks,
// --all must still reach every team, and a single page must hint.
func TestTeamListAllFollowsPageSize(t *testing.T) {
	const total, defaultSize = 25, 20
	var sizes []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("limit") {
			t.Errorf("team list sent limit: %v", q)
		}
		sizes = append(sizes, q.Get("page_size"))
		page, size := 1, defaultSize
		if p := q.Get("page"); p != "" {
			page, _ = strconv.Atoi(p)
		}
		if n, err := strconv.Atoi(q.Get("page_size")); err == nil && n > 0 {
			size = min(n, 100)
		}
		var teams []string
		for i := (page - 1) * size; i < page*size && i < total; i++ {
			teams = append(teams, fmt.Sprintf(`{"id":"t%d","slug":"team-%d","name":"Team %d"}`, i+1, i+1, i+1))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"teams":[%s],"page":%d,"page_size":%d,"total_count":%d}`, strings.Join(teams, ","), page, size, total)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "")

	for _, tt := range []struct {
		args      []string
		wantSizes string
	}{
		{[]string{"--all"}, "[50]"},
		{[]string{"--all", "--limit", "100"}, "[100]"},
		{[]string{"--all", "--limit", "10"}, "[10 10 10]"},
	} {
		sizes = nil
		out, errOut, code := runAuth(t, cfg, cs, nil, "", append([]string{"--format", "json", "team", "list"}, tt.args...)...)
		if code != 0 {
			t.Fatalf("%v: exit = %d, stderr=%q", tt.args, code, errOut)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != total {
			t.Errorf("%v: want all %d teams, got %d (%v)", tt.args, total, len(items), err)
		}
		if fmt.Sprint(sizes) != tt.wantSizes {
			t.Errorf("%v: page_size per request = %v, want %s", tt.args, sizes, tt.wantSizes)
		}
	}

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "team", "list")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if want := "showing page 1 of 2 (20 of 25)"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want hint %q", errOut, want)
	}
}
