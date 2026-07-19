// Package versioncmd implements `vibexp version`.
package versioncmd

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/clictx"
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

			// Best-effort: append the server release sha when a context resolves.
			// Never fails the command — offline prints CLI info only.
			if rt := clictx.Runtime(cmd.Context()); rt != nil && rt.BaseURL != "" {
				timeout := rt.Timeout
				if timeout <= 0 {
					timeout = 5 * time.Second
				}
				if h, err := api.FetchHealth(cmd.Context(), &http.Client{Timeout: timeout}, rt.BaseURL); err == nil {
					cmd.Printf("server: %s (%s)\n", h.Sha, h.Status)
				}
			}
			return nil
		},
	}
}
