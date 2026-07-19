// Package searchcmd implements `vibexp search <query>` — team-scoped, semantic
// cross-resource search (prompts, artifacts, blueprints, memories). The API is a
// POST that takes the query, optional type filters, pagination, and an optional
// project scope in its body; the response envelope is preserved verbatim under
// --format=json.
package searchcmd

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

var columns = []output.Column{
	{Header: "TYPE", Path: ".type"},
	{Header: "TITLE", Path: ".title"},
	{Header: "SLUG", Path: ".slug"},
	{Header: "SCORE", Path: ".score"},
	{Header: "PROJECT", Path: ".project_name"},
}

// New builds the `search` command.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var types []string
	var limit, page int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search team resources (prompts, artifacts, blueprints, memories)",
		Long: "Semantic search across the team's resources. Narrow with repeatable\n" +
			"--type (prompts|artifacts|blueprints|memories) and scope to a project\n" +
			"with --project. --format=json returns the full API envelope.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			team, err := api.Team(rt)
			if err != nil {
				return err
			}

			payload := map[string]any{"query": args[0]}
			if len(types) > 0 {
				payload["types"] = types
			}
			if rt.Project != "" {
				payload["project_id"] = rt.Project
			}
			if limit > 0 {
				payload["per_page"] = limit
			}
			if page > 0 {
				payload["page"] = page
			}

			body, err := json.Marshal(payload)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			raw, _, err := resource.Do(ctx, client, http.MethodPost, "/api/v1/"+team+"/search", body)
			if err != nil {
				return err
			}
			return resource.Render(cmd, rt, getenv, raw,
				&output.TableSpec{Rows: ".results[]? // .items[]? // .data[]?", Columns: columns})
		},
	}
	cmd.Flags().StringArrayVar(&types, "type", nil, "restrict to a resource type (repeatable): prompts|artifacts|blueprints|memories")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results per page")
	cmd.Flags().IntVar(&page, "page", 0, "page number (1-based)")
	return cmd
}
