//go:build e2e

package e2e

import (
	"testing"
)

// TestAPIRawGet proves the raw passthrough: `vibexp api GET` returns the raw
// API response body (the JSON contract) with exit 0.
func TestAPIRawGet(t *testing.T) {
	t.Parallel()
	args := []string{"api", "GET", "/api/v1/auth/me"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	var me struct {
		Email string `json:"email"`
	}
	parseJSON(t, stdout, &me)
	if me.Email == "" {
		t.Fatalf("raw GET /api/v1/auth/me has no email: %s", redact(stdout))
	}
}

// TestAPIPaginate proves --paginate walks a paged list endpoint and merges all
// pages into one JSON array: with limit=1, three namespaced memories must all
// come back in a single merged result.
func TestAPIPaginate(t *testing.T) {
	t.Parallel()

	want := map[string]bool{}
	for _, suffix := range []string{"page-a", "page-b", "page-c"} {
		id := createMemory(t, ns()+" paginate "+suffix)
		want[id] = false
	}

	args := []string{"api", "GET", "/api/v1/" + teamID + "/memories?limit=1", "--paginate"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	var merged []map[string]any
	parseJSON(t, stdout, &merged)
	if len(merged) < len(want) {
		t.Fatalf("--paginate merged %d items, want at least %d", len(merged), len(want))
	}
	for _, it := range merged {
		if id, _ := it["id"].(string); id != "" {
			if _, ok := want[id]; ok {
				want[id] = true
			}
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("memory %s missing from merged --paginate output (%d items)", id, len(merged))
		}
	}
}
