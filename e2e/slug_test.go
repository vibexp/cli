//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestTeamAndProjectSlugs proves --team/--project accept a slug: the CLI
// resolves it to the UUID REST paths require, and an unknown slug is a usage
// error pointing at the list command.
func TestTeamAndProjectSlugs(t *testing.T) {
	t.Parallel()
	if teamSlug == "" || projectSlug == "" {
		t.Skip("team/project list carried no slug")
	}

	args := []string{"memory", "list", "--limit", "1", "--team", teamSlug, "--project", projectSlug, "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var page map[string]any
	parseJSON(t, stdout, &page)

	args = []string{"memory", "list", "--team", ns() + "-no-such-team"}
	stdout, stderr, code = run(t, authEnv(), args...)
	requireCode(t, 2, code, stdout, stderr, args...)
	if !strings.Contains(stderr, "vibexp team list") {
		t.Errorf("unknown team slug should point at team list: %q", redact(stderr))
	}
}
