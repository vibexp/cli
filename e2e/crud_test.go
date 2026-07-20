//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestMemoryLifecycle drives one full CRUD lifecycle through the binary:
// create → get → update → list (contains) → delete → get (gone). Memories are
// the representative resource family per the issue; every artifact is
// namespaced under this run's id and cleaned up even on failure.
func TestMemoryLifecycle(t *testing.T) {
	t.Parallel()

	text := ns() + " lifecycle memory"
	id := createMemory(t, text)

	// Read it back.
	args := []string{"memory", "get", id, "--team", teamID, "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var got struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	parseJSON(t, stdout, &got)
	if got.ID != id || !strings.Contains(got.Text, text) {
		t.Fatalf("memory get mismatch: id=%q text=%q", got.ID, redact(got.Text))
	}

	// Update the content and confirm the new text round-trips.
	updated := text + " (updated)"
	args = []string{"memory", "update", id, "--body-file", "-", "--team", teamID, "--format", "json"}
	stdout, stderr, code = runStdin(t, authEnv(), updated, args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	args = []string{"memory", "get", id, "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	parseJSON(t, stdout, &got)
	if !strings.Contains(got.Text, "(updated)") {
		t.Fatalf("memory update did not stick: %q", redact(got.Text))
	}

	// The list must contain it.
	args = []string{"memory", "list", "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	items, err := listItems([]byte(stdout))
	if err != nil {
		t.Fatalf("memory list shape: %v\n%s", err, redact(stdout))
	}
	found := false
	for _, it := range items {
		if itID, _ := it["id"].(string); itID == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("memory %s not present in list of %d items", id, len(items))
	}

	// Delete, then prove it is gone (API error → exit 1).
	args = []string{"memory", "delete", id, "--yes", "--team", teamID}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	args = []string{"memory", "get", id, "--team", teamID}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 1, code, stdout, stderr, args...)
}
