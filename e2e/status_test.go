//go:build e2e

package e2e

import (
	"testing"
)

// TestCustomStatusLifecycle creates a custom memory status, lists it, sets it
// on a memory, deletes it, and checks the memory moved to the default status.
// Memories stand in for every resource type: the catalog is the same endpoint.
func TestCustomStatusLifecycle(t *testing.T) {
	t.Parallel()

	slug := ns() + "-status"
	args := []string{"status", "create", "--type", "memory", "--slug", slug, "--name", "E2E review", "--team", teamID, "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var created struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	parseJSON(t, stdout, &created)
	if created.ID == "" || created.Slug != slug {
		t.Fatalf("status create returned %+v", created)
	}
	t.Cleanup(func() {
		_, _, _, _ = execBinary(authEnv(), "", "status", "delete", created.ID, "--yes", "--team", teamID)
	})

	// It is listed after the system statuses, by slug.
	args = []string{"status", "list", "--type", "memory", "--team", teamSlug, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var list struct {
		Statuses []struct {
			Slug     string `json:"slug"`
			IsSystem bool   `json:"is_system"`
		} `json:"statuses"`
	}
	parseJSON(t, stdout, &list)
	if len(list.Statuses) == 0 || !list.Statuses[0].IsSystem || list.Statuses[len(list.Statuses)-1].IsSystem {
		t.Fatalf("status list should put system statuses first: %+v", list.Statuses)
	}
	found := false
	for _, s := range list.Statuses {
		found = found || s.Slug == slug
	}
	if !found {
		t.Fatalf("status list does not contain %s", slug)
	}

	// A memory carrying it moves to active when it is deleted (by slug).
	id := createMemory(t, ns()+" status memory")
	args = []string{"memory", "update", id, "--status", slug, "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	args = []string{"status", "delete", slug, "--type", "memory", "--yes", "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var deleted struct {
		ReassignedCount int    `json:"reassigned_count"`
		ReassignedTo    string `json:"reassigned_to"`
	}
	parseJSON(t, stdout, &deleted)
	if deleted.ReassignedCount != 1 || deleted.ReassignedTo != "active" {
		t.Fatalf("status delete = %+v, want 1 resource moved to active", deleted)
	}

	args = []string{"memory", "get", id, "--team", teamID, "--format", "json"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var got struct {
		Status string `json:"status"`
	}
	parseJSON(t, stdout, &got)
	if got.Status != "active" {
		t.Fatalf("memory status after the delete = %q, want active", got.Status)
	}
}
