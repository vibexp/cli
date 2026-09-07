// Package teamcmd implements `vibexp team` (`team list`, `team audit`).
package teamcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
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
	cmd.AddCommand(newAudit(resolve, getenv))
	return cmd
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewListCommand(resolve, getenv, "List teams you are a member of", resource.ListConfig{
		PathFor: func(_ *config.Runtime) (string, error) { return "/api/v1/teams", nil },
		// Membership is summarized via permissions (never role — team access is
		// gated on the permissions array).
		Spec: output.TableSpec{
			Rows: resource.ListRows("teams"),
			Columns: []output.Column{
				{Header: "SLUG", Path: ".slug"},
				{Header: "NAME", Path: ".name"},
				{Header: "MEMBERS", Path: ".member_count"},
				{Header: "PERMISSIONS", Path: ".permissions | join(\",\")"},
			},
		},
	})
}

// auditPath returns /api/v1/{team}/settings/audit, resolving the team
// (flag > env > context; missing → exit 2).
func auditPath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/settings/audit", nil
}

func newAudit(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewNamedListCommand("audit", resolve, getenv,
		"List cross-team settings copies recorded for the resolved team (requires platform v0.12.0+)",
		resource.ListConfig{
			PathFor: auditPath,
			// The endpoint takes only page/limit, which Pagination already
			// handles — no Filters.
			Spec: output.TableSpec{
				// This endpoint's array is "entries", not "<noun>s".
				Rows: resource.ListRows("entries"),
				// actor_name / source_team_name / created_resource_id are
				// nullable — the entry deliberately outlives the actor and the
				// source team, and a custom_types copy carries no single
				// resource id. They need no `// ""` guard: output.stringify
				// already maps a JSON null to an empty cell. Only an
				// aggregation over an optional container needs guarding, since
				// jq's `null | length` is 0, not null.
				Columns: []output.Column{
					{Header: "WHEN", Path: ".created_at"},
					{Header: "WHO", Path: ".actor_name"},
					{Header: "SURFACE", Path: ".surface"},
					{Header: "SOURCE_TEAM", Path: ".source_team_name"},
					{Header: "CREATED", Path: ".created_resource_id"},
				},
			},
		})
}
