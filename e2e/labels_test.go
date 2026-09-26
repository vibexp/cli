//go:build e2e

package e2e

import (
	"testing"
	"time"
)

// TestMemoryLabelsRoundTrip creates a memory with --label and finds it again
// through list --labels (OR semantics: a second, unused label must not hide
// it), then proves an update without --label leaves the labels alone.
func TestMemoryLabelsRoundTrip(t *testing.T) {
	t.Parallel()

	label := ns() + "-lbl" // unique per run, well under the 50-char cap
	args := []string{"memory", "create", "--body-file", "-", "--label", label,
		"--team", teamID, "--project", projectID, "--format", "json"}
	stdout, stderr, code := runStdin(t, authEnv(), ns()+" labelled memory", args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var created struct {
		ID     string   `json:"id"`
		Labels []string `json:"labels"`
	}
	parseJSON(t, stdout, &created)
	if created.ID == "" {
		t.Fatalf("memory create returned no id\noutput: %s", redact(stdout))
	}
	trackMemory(t, created.ID)
	if len(created.Labels) != 1 || created.Labels[0] != label {
		t.Fatalf("created labels = %v, want [%s]", created.Labels, label)
	}

	// An update that does not pass --label must not clear the labels.
	args = []string{"memory", "update", created.ID, "--status", "active", "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var updated struct {
		Labels []string `json:"labels"`
	}
	parseJSON(t, stdout, &updated)
	if len(updated.Labels) != 1 || updated.Labels[0] != label {
		t.Fatalf("labels after update without --label = %v, want [%s]", updated.Labels, label)
	}

	args = []string{"memory", "list", "--labels", label, "--labels", ns() + "-unused",
		"--team", teamID, "--format", "json"}
	deadline := time.Now().Add(20 * time.Second)
	for {
		stdout, stderr, code := run(t, authEnv(), args...)
		requireCode(t, 0, code, stdout, stderr, args...)
		items, err := listItems([]byte(stdout))
		if err != nil {
			t.Fatalf("memory list shape: %v\n%s", err, redact(stdout))
		}
		// Only this run's memory carries the label, so the filtered list is
		// exactly it — anything else means the filter was not applied.
		if len(items) == 1 {
			if id, _ := items[0]["id"].(string); id != created.ID {
				t.Fatalf("list --labels returned %q, want %q", id, created.ID)
			}
			return
		}
		if len(items) > 1 || time.Now().After(deadline) {
			t.Fatalf("list --labels returned %d items, want exactly the labelled memory", len(items))
		}
		time.Sleep(500 * time.Millisecond)
	}
}
