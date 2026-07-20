//go:build e2e

// Package e2e drives the compiled vibexp binary against a live deployment.
// The deployment is addressed exclusively through VIBEXP_CLI_TEST_URL and
// VIBEXP_CLI_TEST_API_KEY, consumed by reference — their values are never
// logged, asserted on, or written anywhere.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Suite state, populated in TestMain.
var (
	binPath   string // compiled vibexp binary
	homeDir   string // isolated $HOME so the developer's ~/.vibexp never leaks in
	baseURL   string // VIBEXP_CLI_TEST_URL, by reference
	apiKey    string // VIBEXP_CLI_TEST_API_KEY, by reference
	teamID    string // first team visible to the key's user
	projectID string // first project in that team
	runID     string // short random hex namespacing this run's resources
)

// nsPrefix is the namespace shared by every resource any run of this suite
// creates; the final sweep only ever touches text carrying it.
const nsPrefix = "cli-e2e-"

// ns returns this run's resource namespace marker, e.g. "cli-e2e-3f9a2c1b".
func ns() string { return nsPrefix + runID }

// authEnv is the standard environment for an authenticated invocation.
func authEnv() []string {
	return []string{
		"VIBEXP_API_KEY=" + apiKey,
		"VIBEXP_BASE_URL=" + baseURL,
	}
}

// run executes the built binary with an isolated environment: only PATH and an
// empty HOME are inherited, so the developer's real config/credentials can
// never influence a test. extraEnv comes last and wins.
func run(t *testing.T, extraEnv []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runStdin(t, extraEnv, "", args...)
}

// runStdin is run with the given standard input (for --body-file -).
func runStdin(t *testing.T, extraEnv []string, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	out, errOut, code, err := execBinary(extraEnv, stdin, args...)
	if err != nil {
		t.Fatalf("vibexp %s: failed to execute: %v", strings.Join(args, " "), err)
	}
	return out, errOut, code
}

// execBinary is the raw runner shared by tests and the TestMain harness.
func execBinary(extraEnv []string, stdin string, args ...string) (stdout, stderr string, code int, err error) {
	cmd := exec.Command(binPath, args...)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"VIBEXP_NO_UPDATE_CHECK=1",
	}, extraEnv...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	code = 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			return "", "", -1, runErr
		}
	}
	return outBuf.String(), errBuf.String(), code, nil
}

// requireCode fails the test when the exit code differs, dumping redacted,
// truncated output for diagnosis.
func requireCode(t *testing.T, want, got int, stdout, stderr string, args ...string) {
	t.Helper()
	if got != want {
		t.Fatalf("vibexp %s: exit code = %d, want %d\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), got, want, redact(stdout), redact(stderr))
	}
}

// redact strips the credential from diagnostic dumps (belt and braces — the
// CLI never echoes it) and truncates so a failure can't flood logs.
func redact(s string) string {
	if apiKey != "" {
		s = strings.ReplaceAll(s, apiKey, "[redacted]")
	}
	const max = 2000
	if len(s) > max {
		s = s[:max] + "…[truncated]"
	}
	return strings.TrimSpace(s)
}

// parseJSON unmarshals CLI --format=json output, failing the test on garbage.
func parseJSON(t *testing.T, s string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), into); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, redact(s))
	}
}

// firstListItem returns the items of a list response body that may be a bare
// array or wrap the list in a named field ({"teams": […]}, {"items": […]}, …).
func listItems(raw []byte) ([]map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, err
	}
	for _, v := range obj {
		var arr []map[string]any
		if json.Unmarshal(v, &arr) == nil {
			return arr, nil
		}
	}
	return nil, fmt.Errorf("no list field found in response")
}

// --- created-resource registry -------------------------------------------

var (
	createdMu  sync.Mutex
	createdIDs []string // memory ids created by this run, deleted in the sweep
)

// trackMemory registers a created memory for deletion and attaches a
// per-test cleanup so even a mid-run failure deletes it promptly.
func trackMemory(t *testing.T, id string) {
	t.Helper()
	createdMu.Lock()
	createdIDs = append(createdIDs, id)
	createdMu.Unlock()
	t.Cleanup(func() { deleteMemory(id) })
}

// deleteMemory best-effort deletes one memory; idempotent (a 404 is fine).
func deleteMemory(id string) {
	_, _, _, _ = execBinary(authEnv(), "", "memory", "delete", id, "--yes", "--team", teamID)
}

// createMemory creates a namespaced memory and returns its id.
func createMemory(t *testing.T, text string) string {
	t.Helper()
	args := []string{"memory", "create", "--body-file", "-", "--team", teamID, "--project", projectID, "--format", "json"}
	stdout, stderr, code := runStdin(t, authEnv(), text, args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	var created struct {
		ID string `json:"id"`
	}
	parseJSON(t, stdout, &created)
	if created.ID == "" {
		t.Fatalf("memory create returned no id\noutput: %s", redact(stdout))
	}
	trackMemory(t, created.ID)
	return created.ID
}

// --- final sweep -----------------------------------------------------------

// sweep deletes every leftover suite resource: everything this run registered,
// plus any memory namespaced cli-e2e-* older than an hour — self-healing after
// a previous crashed run. Runs after m.Run() regardless of outcome.
func sweep() {
	createdMu.Lock()
	ids := append([]string(nil), createdIDs...)
	createdMu.Unlock()
	for _, id := range ids {
		deleteMemory(id)
	}

	stdout, _, code, err := execBinary(authEnv(), "",
		"api", "GET", "/api/v1/"+teamID+"/memories?limit=100", "--paginate")
	if err != nil || code != 0 {
		fmt.Fprintln(os.Stderr, "e2e sweep: could not list memories; skipping stale-resource sweep")
		return
	}
	items, err := listItems([]byte(stdout))
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e sweep: unrecognized list shape; skipping stale-resource sweep")
		return
	}
	swept := 0
	for _, it := range items {
		text, _ := it["text"].(string)
		id, _ := it["id"].(string)
		if id == "" || !strings.HasPrefix(text, nsPrefix) {
			continue
		}
		stale := false
		if created, ok := it["created_at"].(string); ok {
			if ts, perr := time.Parse(time.RFC3339, created); perr == nil {
				stale = time.Since(ts) > time.Hour
			}
		}
		if strings.HasPrefix(text, ns()) || stale {
			deleteMemory(id)
			swept++
		}
	}
	fmt.Fprintf(os.Stderr, "e2e sweep: removed %d leftover namespaced resource(s)\n", swept)
}
