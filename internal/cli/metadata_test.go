package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/exitcode"
)

// metadataCapture records what the fake metadata catalog API received.
type metadataCapture struct {
	keysQuery   url.Values
	valuesQuery url.Values
}

// metadataServer emulates the team-scoped metadata catalog endpoints
// (platform v0.9.0) with fabricated data.
func metadataServer(t *testing.T, cap *metadataCapture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/the-team/metadata/keys", func(w http.ResponseWriter, r *http.Request) {
		cap.keysQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":["env","owner","spec.type"],"truncated":false}`))
	})
	mux.HandleFunc("/api/v1/the-team/metadata/values", func(w http.ResponseWriter, r *http.Request) {
		cap.valuesQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":["prod","staging"],"truncated":false}`))
	})
	return httptest.NewServer(mux)
}

func TestMetadataKeysQuery(t *testing.T) {
	var cap metadataCapture
	srv := metadataServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json",
		"metadata", "keys", "--type", "memories", "--limit", "10")
	if code != 0 {
		t.Fatalf("keys exit = %d", code)
	}
	if cap.keysQuery.Get("resource_type") != "memories" || cap.keysQuery.Get("limit") != "10" {
		t.Errorf("keys query wrong: %v", cap.keysQuery)
	}
	if !strings.Contains(out, `"env"`) {
		t.Errorf("keys json missing key: %q", out)
	}
}

func TestMetadataValuesQuery(t *testing.T) {
	var cap metadataCapture
	srv := metadataServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	out, _, code := runAuth(t, cfg, cs, nil, "", "--format", "json", "--project", "p-9",
		"metadata", "values", "--type", "artifacts", "--key", "env", "--q", "pro")
	if code != 0 {
		t.Fatalf("values exit = %d", code)
	}
	q := cap.valuesQuery
	if q.Get("resource_type") != "artifacts" || q.Get("key") != "env" ||
		q.Get("q") != "pro" || q.Get("project_id") != "p-9" {
		t.Errorf("values query wrong: %v", q)
	}
	if !strings.Contains(out, `"prod"`) {
		t.Errorf("values json missing value: %q", out)
	}
}

func TestMetadataValuesRequiresKey(t *testing.T) {
	var cap metadataCapture
	srv := metadataServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "metadata", "values", "--type", "memories")
	if code != exitcode.UsageErr {
		t.Fatalf("values exit = %d, want %d", code, exitcode.UsageErr)
	}
}

func TestMetadataKeysRejectsBadType(t *testing.T) {
	var cap metadataCapture
	srv := metadataServer(t, &cap)
	defer srv.Close()
	cfg, cs := apiFixture(t, srv.URL, "the-team")

	_, _, code := runAuth(t, cfg, cs, nil, "", "metadata", "keys", "--type", "prompts")
	if code != exitcode.UsageErr {
		t.Fatalf("keys exit = %d, want %d", code, exitcode.UsageErr)
	}
}
