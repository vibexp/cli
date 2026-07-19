// Package projectcmd implements `vibexp project` (currently `project list`).
package projectcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// New builds the `project` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Discover projects in a team",
	}
	cmd.AddCommand(newList(resolve, getenv))
	return cmd
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects in the resolved team",
		Args:  cobra.NoArgs,
	}
	page := resource.AddPaginationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return resource.RunList(cmd, resolve, getenv, resource.ListConfig{
			PathFor: func(rt *config.Runtime) (string, error) {
				team, err := api.Team(rt) // flag > env > context; missing → exit 2
				if err != nil {
					return "", err
				}
				return "/api/v1/" + team + "/projects", nil
			},
			Spec: output.TableSpec{
				Rows: ".projects[]? // .items[]? // .data[]?",
				Columns: []output.Column{
					{Header: "SLUG", Path: ".slug"},
					{Header: "NAME", Path: ".name"},
					{Header: "DESCRIPTION", Path: ".description"},
					{Header: "UPDATED", Path: ".updated_at"},
				},
			},
		}, page)
	}
	return cmd
}
