package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the CLI once for the process-level exit-code tests.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "vibexp")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// exitCode runs the binary and returns its process exit code.
func exitCode(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	// Isolate config/logs into a throwaway HOME so the test never touches the
	// real ~/.vibexp.
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("run %v: %v", args, err)
	return "", -1
}

func TestBinaryExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build test in -short mode")
	}
	bin := buildBinary(t)

	if out, code := exitCode(t, bin, "--help"); code != 0 || !strings.Contains(out, "vibexp") {
		t.Errorf("--help: code=%d out=%q, want 0 with usage", code, out)
	}
	if out, code := exitCode(t, bin, "version"); code != 0 || !strings.Contains(out, "vibexp") {
		t.Errorf("version: code=%d out=%q, want 0", code, out)
	}
	if _, code := exitCode(t, bin, "--not-a-flag"); code != 2 {
		t.Errorf("bad flag: code=%d, want 2", code)
	}
	if _, code := exitCode(t, bin, "config", "use-context", "nope"); code != 2 {
		t.Errorf("missing context: code=%d, want 2", code)
	}
}
