//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestListOverOnePage proves a list spanning several pages is never silently
// partial: a single page warns on stderr (stdout stays the raw page body), and
// --all returns every item as one merged JSON array.
func TestListOverOnePage(t *testing.T) {
	t.Parallel()

	want := map[string]bool{}
	for _, suffix := range []string{"list-a", "list-b", "list-c"} {
		want[createMemory(t, ns()+" "+suffix)] = false
	}

	// One page of one item: at least three exist, so the hint must appear.
	args := []string{"memory", "list", "--limit", "1", "--team", teamID, "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	if !strings.Contains(stderr, "showing page 1 of") {
		t.Errorf("no partial-page hint on stderr: %q", redact(stderr))
	}
	if strings.Contains(stdout, "showing page") {
		t.Error("the partial-page hint leaked onto stdout")
	}

	// The walk covers the whole shared team, so page at the max, not 1.
	args = []string{"memory", "list", "--all", "--limit", "100", "--team", teamID, "--format", "json"}
	deadline := time.Now().Add(20 * time.Second)
	for {
		stdout, stderr, code := run(t, authEnv(), args...)
		requireCode(t, 0, code, stdout, stderr, args...)

		var merged []map[string]any
		parseJSON(t, stdout, &merged)
		for id := range want {
			want[id] = false
		}
		for _, it := range merged {
			if id, _ := it["id"].(string); id != "" {
				if _, ok := want[id]; ok {
					want[id] = true
				}
			}
		}
		missing := ""
		for id, seen := range want {
			if !seen {
				missing = id
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("memory %s missing from merged --all output (%d items) after retries", missing, len(merged))
		}
		time.Sleep(500 * time.Millisecond)
	}
}
