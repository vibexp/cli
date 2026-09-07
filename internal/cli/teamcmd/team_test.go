package teamcmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

// The behaviour of the built commands is covered end to end in internal/cli;
// these tests live here because Go measures coverage per package, so a command
// exercised only from internal/cli reports zero for the package that owns it.

func sub(t *testing.T, cmd *cobra.Command, use string) *cobra.Command {
	t.Helper()
	for _, c := range cmd.Commands() {
		if c.Use == use {
			return c
		}
	}
	t.Fatalf("no %q subcommand; got %v", use, cmd.Commands())
	return nil
}

func TestNewExposesListAndAudit(t *testing.T) {
	cmd := New(nil, nil)
	if cmd.Use != "team" {
		t.Errorf("Use = %q, want team", cmd.Use)
	}
	for _, use := range []string{"list", "audit"} {
		c := sub(t, cmd, use)
		if c.Short == "" {
			t.Errorf("%q has no Short", use)
		}
		// Both are paginated list views.
		if c.Flags().Lookup("limit") == nil || c.Flags().Lookup("page") == nil {
			t.Errorf("%q does not bind the pagination flags", use)
		}
	}
	// The audit trail needs platform v0.12.0+; the help must say so, since an
	// older deployment answers with a 404 that reads like a CLI bug.
	if short := sub(t, cmd, "audit").Short; !strings.Contains(short, "v0.12.0") {
		t.Errorf("audit Short should name the platform requirement: %q", short)
	}
}

func TestAuditPathIsTeamScoped(t *testing.T) {
	got, err := auditPath(&config.Runtime{Team: "acme"})
	if err != nil {
		t.Fatalf("auditPath: %v", err)
	}
	if want := "/api/v1/acme/settings/audit"; got != want {
		t.Errorf("auditPath = %q, want %q", got, want)
	}
}

func TestAuditPathWithoutTeamIsUsageError(t *testing.T) {
	_, err := auditPath(&config.Runtime{})
	if err == nil {
		t.Fatal("a missing team must be an error")
	}
	// Propagated unchanged from api.Team, which is what makes it exit 2.
	var coded *exitcode.CodedError
	if !errors.As(err, &coded) || coded.Code != exitcode.UsageErr {
		t.Fatalf("want a usage-coded error, got %#v", err)
	}
	for _, want := range []string{"--team", "VIBEXP_TEAM"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q: %v", want, err)
		}
	}
}
