package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// run executes the command tree with an isolated store/logdir and returns
// stdout, stderr, and the resolved exit code.
func run(t *testing.T, store *config.Store, getenv config.Getenv, args ...string) (string, string, int) {
	t.Helper()
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	root := NewRootCommand(Options{Store: store, Getenv: getenv, LogDir: t.TempDir()})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errBuf.String(), exitcode.FromError(err)
}

func tempStore(t *testing.T) *config.Store {
	t.Helper()
	return &config.Store{Path: t.TempDir() + "/config.yaml"}
}

func TestVersionExitsZero(t *testing.T) {
	out, _, code := run(t, tempStore(t), nil, "version")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "vibexp") {
		t.Errorf("version output missing 'vibexp': %q", out)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	_, _, code := run(t, tempStore(t), nil, "--definitely-not-a-flag")
	if code != exitcode.UsageErr {
		t.Fatalf("exit code = %d, want 2 (usage)", code)
	}
}

func TestHelpExitsZero(t *testing.T) {
	out, _, code := run(t, tempStore(t), nil, "--help")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "vibexp") {
		t.Errorf("help output missing usage: %q", out)
	}
}

func TestContextLifecycle(t *testing.T) {
	store := tempStore(t)

	// set-context creates and (being first) activates it.
	if _, _, code := run(t, store, nil, "config", "set-context", "dev",
		"--base-url", "https://dev.example", "--team", "team-dev"); code != 0 {
		t.Fatalf("set-context exit = %d", code)
	}

	// get-contexts lists it, marked current.
	out, _, code := run(t, store, nil, "config", "get-contexts")
	if code != 0 {
		t.Fatalf("get-contexts exit = %d", code)
	}
	if !strings.Contains(out, "dev") || !strings.Contains(out, "https://dev.example") {
		t.Errorf("get-contexts missing dev row: %q", out)
	}

	// A second context, then switch to it.
	if _, _, code := run(t, store, nil, "config", "set-context", "prod",
		"--base-url", "https://prod.example"); code != 0 {
		t.Fatalf("set-context prod exit = %d", code)
	}
	if _, _, code := run(t, store, nil, "config", "use-context", "prod"); code != 0 {
		t.Fatalf("use-context exit = %d", code)
	}
	out, _, _ = run(t, store, nil, "config", "current-context")
	if strings.TrimSpace(out) != "prod" {
		t.Errorf("current-context = %q, want prod", strings.TrimSpace(out))
	}

	// use-context on a missing name is a usage error.
	if _, _, code := run(t, store, nil, "config", "use-context", "ghost"); code != exitcode.UsageErr {
		t.Errorf("use-context ghost exit = %d, want 2", code)
	}
}

func TestContextEnvOverride(t *testing.T) {
	store := tempStore(t)
	if _, _, code := run(t, store, nil, "config", "set-context", "dev", "--base-url", "https://dev"); code != 0 {
		t.Fatalf("setup exit = %d", code)
	}
	if _, _, code := run(t, store, nil, "config", "set-context", "prod", "--base-url", "https://prod"); code != 0 {
		t.Fatalf("setup exit = %d", code)
	}
	// current-context is "dev" (first created), but VIBEXP_CONTEXT=prod overrides.
	getenv := func(k string) string {
		if k == config.EnvContext {
			return "prod"
		}
		return ""
	}
	out, _, _ := run(t, store, getenv, "config", "current-context")
	if strings.TrimSpace(out) != "prod" {
		t.Errorf("current-context with env = %q, want prod", strings.TrimSpace(out))
	}
}
