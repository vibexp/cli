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
	cmd.AddCommand(resource.NewListCommand(resolve, getenv, "List projects in the resolved team", resource.ListConfig{
		PathFor: func(rt *config.Runtime) (string, error) {
			team, err := api.Team(rt) // flag > env > context; missing → exit 2
			if err != nil {
				return "", err
			}
			return "/api/v1/" + team + "/projects", nil
		},
		Spec: output.TableSpec{
			Rows: resource.ListRows("projects"),
			Columns: []output.Column{
				{Header: "SLUG", Path: ".slug"},
				{Header: "NAME", Path: ".name"},
				{Header: "DESCRIPTION", Path: ".description"},
				{Header: "UPDATED", Path: ".updated_at"},
			},
		},
	}))
	return cmd
}
