// Package teamcmd implements `vibexp team` (currently `team list`).
package teamcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// New builds the `team` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Discover teams you belong to",
	}
	cmd.AddCommand(newList(resolve, getenv))
	return cmd
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List teams you are a member of",
		Args:  cobra.NoArgs,
	}
	page := resource.AddPaginationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return resource.RunList(cmd, resolve, getenv, resource.ListConfig{
			PathFor: func(_ *config.Runtime) (string, error) { return "/api/v1/teams", nil },
			// Membership is summarized via permissions (never role — team access
			// is gated on the permissions array).
			Spec: output.TableSpec{
				Rows: ".teams[]? // .items[]? // .data[]?",
				Columns: []output.Column{
					{Header: "SLUG", Path: ".slug"},
					{Header: "NAME", Path: ".name"},
					{Header: "MEMBERS", Path: ".member_count"},
					{Header: "PERMISSIONS", Path: ".permissions | join(\",\")"},
				},
			},
		}, page)
	}
	return cmd
}
