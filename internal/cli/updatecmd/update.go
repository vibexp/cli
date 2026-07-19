// Package updatecmd implements `vibexp update` — the explicit, checksum-verified
// self-update. It never runs implicitly; the background notice lives in
// internal/update and is fired from root's Execute.
package updatecmd

import (
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/update"
	"github.com/vibexp/cli/internal/version"
)

// New builds the `update` command.
func New() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update vibexp to the latest release",
		Long: "Download the latest GitHub release for this OS/arch, verify it against\n" +
			"the release checksums, and atomically replace this binary. Installs from\n" +
			"Homebrew or `go install` are not self-updated — the correct upgrade\n" +
			"command is printed instead. Use --check to report availability only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := &http.Client{Timeout: 30 * time.Second}
			if err := update.Apply(cmd.Context(), cmd.OutOrStdout(), os.Getenv, client, update.DefaultAPIBase, version.Version, checkOnly); err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report whether an update is available without installing it")
	return cmd
}
