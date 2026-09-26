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
	mux.HandleFunc("/api/v1/the-team/artifacts", func(w http.ResponseWriter, r *http.Request) {
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
			items = append(items, fmt.Sprintf(`{"id":"a%d","slug":"art-%d","title":"T%d","project_id":"p-1","updated_at":"2026-02-01T00:00:00Z"}`, i+1, i+1, i+1))
		}
		totalPages := (total + perPage - 1) / perPage
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"artifacts":[%s],"page":%d,"per_page":%d,"total_count":%d,"total_pages":%d}`,
			strings.Join(items, ","), page, perPage, total, totalPages)
	})
	return httptest.NewServer(mux)
}

func TestListLimitHelpNamesServerMax(t *testing.T) {
	cfg, cs := apiFixture(t, "http://127.0.0.1:0", "the-team")
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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	cfg, cs := apiFixture(t, "http://127.0.0.1:0", "the-team")
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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	mux.HandleFunc("/api/v1/the-team/prompts", func(w http.ResponseWriter, r *http.Request) {
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
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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
	mux.HandleFunc("/api/v1/the-team/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"type":"memory","title":"x"}],"page":1,"per_page":1,"total_pages":4,"total_count":4}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "search", "anything")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if want := "showing page 1 of 4 (1 of 4); use --page or --limit (max 100)"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want hint %q", errOut, want)
	}
}

// The attachments endpoint ignores page/limit and returns every item with no
// page metadata. --all must stop after one request instead of looping forever.
func TestListAllStopsOnUnpaginatedEndpoint(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/the-team/attachments", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits > 5 {
			t.Error("--all kept walking an unpaginated endpoint")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attachments":[{"id":"x1"},{"id":"x2"}],"total_count":2,"total_size_bytes":10}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	for _, limit := range []string{"1", "2"} {
		hits = 0
		out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json",
			"attachment", "list", "--owner-id", "o-1", "--all", "--limit", limit)
		if code != 0 {
			t.Fatalf("--limit %s: exit = %d, stderr=%q", limit, code, errOut)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 2 {
			t.Errorf("--limit %s: want the 2 items once, got %s", limit, out)
		}
		if hits != 1 {
			t.Errorf("--limit %s: requests = %d, want 1", limit, hits)
		}
	}
}

// A server that serves fewer items per page than asked (it clamps the limit)
// reports its real per_page; --all must walk to the end, not stop after the
// first "short" page.
func TestListAllFollowsServerPerPage(t *testing.T) {
	var pages []int
	srv := clampedArtifactServer(t, 5, 2, &pages)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

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

// listTeams ignores limit, serves page_size (20) items, and reports no
// total_pages — --all must still walk every page, and a single page must hint.
func TestTeamListAllFollowsPageSize(t *testing.T) {
	const total, size = 25, 20
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			page, _ = strconv.Atoi(p)
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

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "team", "list", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != total {
		t.Errorf("want all %d teams, got %d (%v)", total, len(items), err)
	}

	_, errOut, code = runAuth(t, cfg, cs, nil, "", "--format", "json", "team", "list")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errOut)
	}
	if want := "showing page 1 of 2 (20 of 25)"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want hint %q", errOut, want)
	}
}
