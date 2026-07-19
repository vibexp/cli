// Package versioncmd implements `vibexp version`.
package versioncmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/version"
)

// New builds the `version` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version, commit, and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Current()
			cmd.Printf("vibexp %s\ncommit: %s\nbuilt:  %s\nsource: %s\n",
				info.Version, info.Commit, info.Date, info.InstallSource)
			return nil
		},
	}
}
