package api

import (
	"strings"
	"testing"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
)

func TestTeamPresentPassThrough(t *testing.T) {
	// UUID and slug are both passed through unchanged.
	for _, v := range []string{"dee2f88f-4372-4deb-af2c-021b21b4eb0e", "my-team-slug"} {
		got, err := Team(&config.Runtime{Team: v})
		if err != nil {
			t.Fatalf("Team(%q): %v", v, err)
		}
		if got != v {
			t.Errorf("Team = %q, want %q", got, v)
		}
	}
}

func TestTeamMissingIsUsageError(t *testing.T) {
	_, err := Team(&config.Runtime{ContextName: "dev"})
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Fatalf("missing team exit = %d, want 2", got)
	}
	if err == nil || !strings.Contains(err.Error(), "--team") || !strings.Contains(err.Error(), "VIBEXP_TEAM") {
		t.Errorf("error should name all three options: %v", err)
	}
}

func TestProjectMissingIsUsageError(t *testing.T) {
	_, err := Project(&config.Runtime{})
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("missing project exit = %d, want 2", got)
	}
}
