package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatorPolicy(t *testing.T) {
	r := newRotator(t.TempDir())
	if r.MaxSize != 5 {
		t.Errorf("MaxSize = %d MB, want 5", r.MaxSize)
	}
	if r.MaxBackups != 3 {
		t.Errorf("MaxBackups = %d, want 3", r.MaxBackups)
	}
}

func TestRedactRegisteredSecret(t *testing.T) {
	// A fabricated, obviously-fake secret (never a real credential).
	const fakeSecret = "vxk_FAKE_super_secret_value_1234567890"
	RegisterSecret(fakeSecret)

	got := Redact("authorizing with " + fakeSecret + " now")
	if strings.Contains(got, fakeSecret) {
		t.Fatalf("Redact leaked the secret: %q", got)
	}
	if !strings.Contains(got, placeholder) {
		t.Errorf("Redact should insert placeholder, got %q", got)
	}
}

func TestRedactIgnoresShortValues(t *testing.T) {
	RegisterSecret("ab") // too short — must be ignored to avoid corrupting logs
	if Redact("a table of abacus data") != "a table of abacus data" {
		t.Error("short secret should not be registered/redacted")
	}
}

func TestLoggerRedactsInFile(t *testing.T) {
	const fakeSecret = "vxk_FAKE_logfile_secret_ABCDEFGHIJKLMNOP"
	RegisterSecret(fakeSecret)

	dir := t.TempDir()
	logger, err := Init(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	logger.Info("user authenticated",
		"api_key", fakeSecret, // sensitive KEY -> redacted by name
		"note", "token is "+fakeSecret, // sensitive VALUE -> redacted by registry
	)
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cli.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(data)
	if strings.Contains(content, fakeSecret) {
		t.Fatalf("log file leaked the secret:\n%s", content)
	}
	if !strings.Contains(content, placeholder) {
		t.Errorf("expected placeholder in log, got:\n%s", content)
	}

	// Each non-empty line must be valid JSON.
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("log line is not valid JSON: %q (%v)", line, err)
		}
	}
}
