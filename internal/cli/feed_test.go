package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

type feedCapture struct {
	postBody   map[string]any
	replyBody  map[string]any
	itemsQuery url.Values
	repliesHit bool
}

func feedServer(t *testing.T, cap *feedCapture) *httptest.Server {
	t.Helper()
	const base = "/api/v1/the-team"
	mux := http.NewServeMux()
	mux.HandleFunc(base+"/feeds", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"feeds":[{"id":"f-1","name":"General","description":"team feed","updated_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
	})
	mux.HandleFunc(base+"/feeds/f-1/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.itemsQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"items":[{"id":"i-1","ai_assistant_name":"bot","excerpt":"hello there","reply_count":1,"posted_at":"2026-02-01T00:00:00Z"}],"page":1,"per_page":50,"total_count":1,"total_pages":1}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.postBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"i-new","ai_assistant_name":"vibexp-cli","excerpt":"posted body","reply_count":0,"posted_at":"2026-02-02T00:00:00Z","title":"Deploy"}`))
		}
	})
	mux.HandleFunc(base+"/feed-items/i-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i-1","ai_assistant_name":"bot","excerpt":"hello there","content":"hello there","reply_count":1,"posted_at":"2026-02-01T00:00:00Z","title":"Note"}`))
	})
	// i-2 exists but its replies endpoint fails — get-item must still succeed.
	mux.HandleFunc(base+"/feed-items/i-2", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i-2","ai_assistant_name":"bot","excerpt":"solo","content":"solo","reply_count":0,"posted_at":"2026-02-01T00:00:00Z","title":"Solo"}`))
	})
	mux.HandleFunc(base+"/feed-items/i-2/replies", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"title":"Server Error","status":500,"detail":"boom","request_id":"req-500"}`))
	})
	mux.HandleFunc(base+"/feed-items/i-1/replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cap.repliesHit = true
			_, _ = w.Write([]byte(`{"replies":[{"id":"r-1","ai_assistant_name":"human","content":"nice work","posted_at":"2026-02-01T01:00:00Z"}]}`))
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&cap.replyBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"r-new","ai_assistant_name":"vibexp-cli","content":"my reply","posted_at":"2026-02-02T00:00:00Z"}`))
		}
	})
	return httptest.NewServer(mux)
}

func TestFeedList(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "feed", "list")
	if code != 0 {
		t.Fatalf("feed list exit = %d", code)
	}
	if !strings.Contains(out, `"id":"f-1"`) {
		t.Errorf("feed list missing feed: %q", out)
	}
}

func TestFeedItems(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	// Missing --feed → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "feed", "items"); code != exitcode.UsageErr {
		t.Errorf("items without --feed exit = %d, want 2", code)
	}
	out, _, code := runAuth(t, cfg, cs, nil, "", "feed", "items", "--feed", "f-1", "--limit", "5")
	if code != 0 {
		t.Fatalf("items exit = %d", code)
	}
	if cap.itemsQuery.Get("limit") != "5" {
		t.Errorf("pagination not applied: %v", cap.itemsQuery)
	}
	if !strings.Contains(out, "i-1") || !strings.Contains(out, "bot") {
		t.Errorf("items output wrong: %q", out)
	}
}

// TestFeedGetItemWithReplies: default (human) output shows the item AND fetches
// and lists its replies.
func TestFeedGetItemWithReplies(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "feed", "get-item", "i-1")
	if code != 0 {
		t.Fatalf("get-item exit = %d, out=%q", code, out)
	}
	if !cap.repliesHit {
		t.Error("human get-item should fetch replies")
	}
	if !strings.Contains(out, "i-1") || !strings.Contains(out, "nice work") {
		t.Errorf("get-item should show item + reply: %q", out)
	}
}

// TestFeedGetItemJSONOmitsReplies: json output is the raw item body only — no
// replies fetch, so the output stays a single pipe-safe document.
func TestFeedGetItemJSONOmitsReplies(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "feed", "get-item", "i-1")
	if code != 0 {
		t.Fatalf("get-item json exit = %d", code)
	}
	if cap.repliesHit {
		t.Error("json get-item must not fetch/append replies")
	}
	if !strings.Contains(out, `"id":"i-1"`) || strings.Contains(out, "nice work") {
		t.Errorf("json get-item should be the raw item only: %q", out)
	}
}

// TestFeedGetItemRepliesFailureNonFatal: the item is the primary result, so a
// failed replies fetch warns but does not fail the command.
func TestFeedGetItemRepliesFailureNonFatal(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, errOut, code := runAuth(t, cfg, cs, nil, "", "feed", "get-item", "i-2")
	if code != 0 {
		t.Fatalf("get-item with failing replies exit = %d, want 0", code)
	}
	if !strings.Contains(out, "i-2") {
		t.Errorf("item should still be shown: %q", out)
	}
	if !strings.Contains(errOut, "could not load replies") {
		t.Errorf("should warn about replies failure: %q", errOut)
	}
}

func TestFeedPost(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "feed", "post", "shipping it", "--feed", "f-1", "--title", "Deploy")
	if code != 0 {
		t.Fatalf("post exit = %d, out=%q", code, out)
	}
	if cap.postBody["title"] != "Deploy" || cap.postBody["content"] != "shipping it" || cap.postBody["ai_assistant_name"] != "vibexp-cli" {
		t.Errorf("post body wrong: %+v", cap.postBody)
	}
	if !strings.Contains(out, "i-new") {
		t.Errorf("post output missing created item: %q", out)
	}

	// Missing --feed → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "feed", "post", "x", "--title", "T"); code != exitcode.UsageErr {
		t.Errorf("post without --feed exit = %d, want 2", code)
	}
	// Missing --title → exit 2.
	if _, _, code := runAuth(t, cfg, cs, nil, "", "feed", "post", "x", "--feed", "f-1"); code != exitcode.UsageErr {
		t.Errorf("post without --title exit = %d, want 2", code)
	}
}

func TestFeedPostFromStdinAndAuthor(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "from stdin", "feed", "post",
		"--feed", "f-1", "--title", "T", "--body-file", "-", "--author", "ci-bot")
	if code != 0 {
		t.Fatalf("post stdin exit = %d", code)
	}
	if cap.postBody["content"] != "from stdin" || cap.postBody["ai_assistant_name"] != "ci-bot" {
		t.Errorf("stdin/author not sent: %+v", cap.postBody)
	}
}

func TestFeedPostArgAndBodyFileConflict(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	if _, _, code := runAuth(t, cfg, cs, nil, "", "feed", "post", "arg msg",
		"--feed", "f-1", "--title", "T", "--body-file", "-"); code != exitcode.UsageErr {
		t.Errorf("arg+body-file exit = %d, want 2", code)
	}
	if cap.postBody != nil {
		t.Error("must not post when input is ambiguous")
	}
}

func TestFeedReply(t *testing.T) {
	var cap feedCapture
	srv := feedServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "feed", "reply", "i-1", "my reply")
	if code != 0 {
		t.Fatalf("reply exit = %d, out=%q", code, out)
	}
	if cap.replyBody["content"] != "my reply" {
		t.Errorf("reply body wrong: %+v", cap.replyBody)
	}
	if !strings.Contains(out, "my reply") {
		t.Errorf("reply output missing created reply: %q", out)
	}

	// Stdin path.
	_, _, code = runAuth(t, cfg, cs, nil, "piped reply", "feed", "reply", "i-1", "--body-file", "-")
	if code != 0 {
		t.Fatalf("reply stdin exit = %d", code)
	}
	if cap.replyBody["content"] != "piped reply" {
		t.Errorf("reply stdin not sent: %+v", cap.replyBody)
	}
}
