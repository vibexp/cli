// Package usercmd implements `vibexp whoami`.
package usercmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// NewWhoami builds the `whoami` command: the authenticated user's identity.
func NewWhoami(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec := output.TableSpec{
				Rows: ".",
				Columns: []output.Column{
					{Header: "ID", Path: ".id"},
					{Header: "NAME", Path: ".name"},
					{Header: "EMAIL", Path: ".email"},
				},
			}
			return resource.GetItem(cmd, resolve, getenv, "/api/v1/auth/me", spec)
		},
	}
}
