package resource

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

func newCmd(stdin string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

func TestConfirmDeletionAssumeYes(t *testing.T) {
	proceed, err := ConfirmDeletion(newCmd(""), "thing x", true)
	if err != nil || !proceed {
		t.Errorf("assumeYes should proceed: proceed=%v err=%v", proceed, err)
	}
}

func TestConfirmDeletionNonInteractiveRefuses(t *testing.T) {
	// stdin is a strings.Reader (not a TTY) → refuse with a usage error.
	proceed, err := ConfirmDeletion(newCmd("y\n"), "thing x", false)
	if proceed {
		t.Error("non-interactive without --yes must not proceed")
	}
	if got := exitcode.FromError(err); got != exitcode.UsageErr {
		t.Errorf("exit = %d, want 2", got)
	}
}
