// Package relationcmd implements `vibexp relations` — curated commands for the
// typed resource-relations surface added in VibeXP platform v0.8.0. Relations
// are directed, typed edges (governed-by, built-from, explained-by, supersedes)
// between two of the four resource types (artifact | memory | prompt |
// blueprint) within one project. The surface is team-scoped under
// /api/v1/{team}/relations; built on the shared resource scaffold
// (docs/adding-commands.md).
package relationcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// listColumns render a page of RelatedResource rows (the list response): each
// relation touching a resource, seen from that resource, in both directions.
// slug is absent for memories and renders empty.
var listColumns = []output.Column{
	{Header: "RELATION_ID", Path: ".relation_id"},
	{Header: "DIRECTION", Path: ".direction"},
	{Header: "RELATION_TYPE", Path: ".relation_type"},
	{Header: "TYPE", Path: ".resource_type"},
	{Header: "TITLE", Path: ".title"},
	{Header: "SLUG", Path: ".slug"},
	{Header: "STATUS", Path: ".status"},
	{Header: "ORIGIN", Path: ".origin"},
}

// itemColumns render a single Relation edge (the create/confirm response),
// which carries both endpoints rather than one resolved neighbor.
var itemColumns = []output.Column{
	{Header: "ID", Path: ".id"},
	{Header: "RELATION_TYPE", Path: ".relation_type"},
	{Header: "FROM", Path: `(.from_type // "?") + " " + (.from_id // "?")`},
	{Header: "TO", Path: `(.to_type // "?") + " " + (.to_id // "?")`},
	{Header: "ORIGIN", Path: ".origin"},
	{Header: "STATUS", Path: ".status"},
}

// itemSpec renders a single relation edge object.
var itemSpec = output.TableSpec{Rows: ".", Columns: itemColumns}

// New builds the `relations` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relations",
		Short: "Manage typed resource relations (list, create, confirm, delete, seed)",
	}
	cmd.AddCommand(
		newList(resolve, getenv),
		newCreate(resolve, getenv),
		newConfirm(resolve, getenv),
		newDelete(resolve, getenv),
		newSeed(resolve, getenv),
	)
	return cmd
}

// basePath returns /api/v1/{team}/relations, resolving the team (flag > env >
// context; missing → exit 2). Relations are team-scoped only — the project is
// derived server-side from the two endpoints.
func basePath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/relations", nil
}
