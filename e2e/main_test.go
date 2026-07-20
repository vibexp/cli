//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gates, builds, and bootstraps the suite:
//   - both VIBEXP_CLI_TEST_* variables present, else skip the whole suite
//     cleanly (exit 0 — fork PRs and secretless checkouts stay green);
//   - compile vibexp once into a temp dir;
//   - discover the key's first team and that team's first project;
//   - run the tests, then sweep leftover namespaced resources.
func TestMain(m *testing.M) {
	baseURL = os.Getenv("VIBEXP_CLI_TEST_URL")
	apiKey = os.Getenv("VIBEXP_CLI_TEST_API_KEY")
	if baseURL == "" || apiKey == "" {
		// Presence check only — values are never printed.
		fmt.Fprintln(os.Stderr, "e2e: VIBEXP_CLI_TEST_URL / VIBEXP_CLI_TEST_API_KEY not set; skipping e2e suite")
		os.Exit(0)
	}

	code, err := setupAndRun(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setupAndRun(m *testing.M) (int, error) {
	tmp, err := os.MkdirTemp("", "vibexp-e2e-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	homeDir = filepath.Join(tmp, "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		return 0, err
	}

	var idBytes [4]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return 0, err
	}
	runID = hex.EncodeToString(idBytes[:])

	binPath = filepath.Join(tmp, "vibexp")
	if err := buildBinary(binPath); err != nil {
		return 0, fmt.Errorf("building vibexp: %w", err)
	}

	if err := discoverScope(); err != nil {
		return 0, fmt.Errorf("discovering team/project: %w", err)
	}

	code := m.Run()
	sweep()
	return code, nil
}

// buildBinary compiles ./cmd/vibexp from the module root (the parent of this
// e2e package) exactly as a user build would.
func buildBinary(out string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/vibexp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, b)
	}
	return nil
}

// moduleRoot resolves the repo root via `go env GOMOD` so the suite works from
// any invocation directory.
func moduleRoot() (string, error) {
	b, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(string(b))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("not inside a Go module")
	}
	return filepath.Dir(gomod), nil
}

// discoverScope resolves the team and project the suite operates in: the first
// team visible to the key's user, and the first project in it. Against the
// ephemeral CI stack these are the bootstrap user's auto-created defaults.
func discoverScope() error {
	stdout, stderr, code, err := execBinary(authEnv(), "", "team", "list", "--format", "json")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("team list exited %d: %s", code, redact(stderr))
	}
	teams, err := listItems([]byte(stdout))
	if err != nil || len(teams) == 0 {
		return fmt.Errorf("no teams visible to the test user")
	}
	teamID, _ = teams[0]["id"].(string)
	if teamID == "" {
		return fmt.Errorf("team list item has no id")
	}

	stdout, stderr, code, err = execBinary(authEnv(), "", "project", "list", "--team", teamID, "--format", "json")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("project list exited %d: %s", code, redact(stderr))
	}
	projects, err := listItems([]byte(stdout))
	if err != nil || len(projects) == 0 {
		return fmt.Errorf("no projects in team")
	}
	projectID, _ = projects[0]["id"].(string)
	if projectID == "" {
		return fmt.Errorf("project list item has no id")
	}
	return nil
}
