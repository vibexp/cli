//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestWhoamiWithEnvAuth proves env-var auth end to end: a valid key resolves
// the authenticated identity with exit 0.
func TestWhoamiWithEnvAuth(t *testing.T) {
	t.Parallel()
	args := []string{"whoami", "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	var me struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	parseJSON(t, stdout, &me)
	if me.ID == "" || me.Email == "" {
		t.Fatalf("whoami JSON missing id/email: %s", redact(stdout))
	}
}

// TestTeamList proves the discovery surface: at least one team, each with an id.
func TestTeamList(t *testing.T) {
	t.Parallel()
	args := []string{"team", "list", "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)

	teams, err := listItems([]byte(stdout))
	if err != nil || len(teams) == 0 {
		t.Fatalf("team list returned no teams (err=%v): %s", err, redact(stdout))
	}
	if id, _ := teams[0]["id"].(string); id == "" {
		t.Fatalf("team item has no id: %s", redact(stdout))
	}
}

// TestInvalidKeyExitsAuthCode proves the auth-failure contract: a rejected
// credential exits 4, with no panic and no secret echoed.
func TestInvalidKeyExitsAuthCode(t *testing.T) {
	t.Parallel()
	env := []string{
		"VIBEXP_API_KEY=vxk_e2e_deliberately_invalid",
		"VIBEXP_BASE_URL=" + baseURL,
	}
	args := []string{"whoami"}
	stdout, stderr, code := run(t, env, args...)
	requireCode(t, 4, code, stdout, stderr, args...)
	if strings.Contains(stdout+stderr, apiKey) {
		t.Fatal("real credential leaked into output of an unauthenticated call")
	}
}

// TestUsageErrorExitsUsageCode proves the usage contract: an invalid
// invocation exits 2 before any network traffic.
func TestUsageErrorExitsUsageCode(t *testing.T) {
	t.Parallel()
	// memory create without --body-file is a usage error (exit 2).
	args := []string{"memory", "create", "--team", teamID, "--project", projectID}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 2, code, stdout, stderr, args...)
}

// TestNonInteractiveDeleteRequiresYes proves the destructive-verb contract:
// without a TTY and without --yes, delete refuses with exit 2.
func TestNonInteractiveDeleteRequiresYes(t *testing.T) {
	t.Parallel()
	args := []string{"memory", "delete", "00000000-0000-0000-0000-000000000000", "--team", teamID}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 2, code, stdout, stderr, args...)
}
