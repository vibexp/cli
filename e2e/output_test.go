//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFormatJSONIsParseable proves the --format=json contract: stdout is the
// raw API response body, parseable JSON, nothing else on stdout.
func TestFormatJSONIsParseable(t *testing.T) {
	t.Parallel()
	args := []string{"team", "list", "--format", "json"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("--format=json stdout is not valid JSON: %s", redact(stdout))
	}
}

// TestPipedOutputIsTSV proves the TTY contract: without a terminal (tests run
// the binary with piped stdio), the default output is tab-separated plain
// text — no ANSI escapes, tabs between columns.
func TestPipedOutputIsTSV(t *testing.T) {
	t.Parallel()
	args := []string{"team", "list"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("piped team list produced no stdout")
	}
	if !strings.Contains(stdout, "\t") {
		t.Fatalf("piped output has no tab separators (not TSV):\n%s", redact(stdout))
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("piped output contains ANSI escapes:\n%s", redact(stdout))
	}
}

// TestJQExtraction proves the built-in --jq filter extracts a field from the
// raw response.
func TestJQExtraction(t *testing.T) {
	t.Parallel()
	args := []string{"whoami", "--jq", ".email"}
	stdout, stderr, code := run(t, authEnv(), args...)
	requireCode(t, 0, code, stdout, stderr, args...)
	if !strings.Contains(stdout, "@") {
		t.Fatalf("--jq .email did not yield an email: %s", redact(stdout))
	}
}
