package resource

import (
	"bufio"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/vibexp/cli/internal/exitcode"
)

// ConfirmDeletion asks the user to confirm a destructive action. It is the
// shared gate for every destructive verb (reused by memory/blueprint/etc.).
//
//   - assumeYes (from --yes): proceed without prompting.
//   - non-interactive (stdin not a TTY) without --yes: refuse with a usage
//     error (exit 2), so scripts must opt in explicitly.
//   - interactive: prompt [y/N]; proceed only on "y"/"yes".
//
// It returns (proceed, err). A false proceed with a nil err is a clean user
// abort — the caller should stop without treating it as a failure.
func ConfirmDeletion(cmd *cobra.Command, what string, assumeYes bool) (bool, error) {
	if assumeYes {
		return true, nil
	}
	in := cmd.InOrStdin()
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false, exitcode.Usage("refusing to delete %s without confirmation; pass --yes to proceed non-interactively", what)
	}
	cmd.PrintErrf("Delete %s? [y/N]: ", what)
	line, _ := bufio.NewReader(in).ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}
