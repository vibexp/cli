// Package searchcmd implements `vibexp search <query>` — team-scoped, semantic
// cross-resource search (prompts, artifacts, blueprints, memories). The API is a
// POST that takes the query, optional type filters, pagination, and an optional
// project scope in its body; the response envelope is preserved verbatim under
// --format=json. With --summary it posts the same body minus paging to the AI
// Summary endpoint instead (see summary.go).
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
	var summary bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search team resources (prompts, artifacts, blueprints, memories)",
		Long: "Semantic search across the team's resources. Narrow with repeatable\n" +
			"--type (prompts|artifacts|blueprints|memories) and scope to a project\n" +
			"with --project. --format=json returns the full API envelope.\n\n" +
			"--summary answers the query from the team's top results with the\n" +
			"team's model instead (AI Summary, platform v0.14.0+; needs AI summaries\n" +
			"enabled and a model provider configured for the team). It prints the\n" +
			"markdown answer raw, then its numbered sources; --format=json|yaml or\n" +
			"--jq return the full response. It reads the global top results, so\n" +
			"--limit and --page do not apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if summary {
				return runSummary(cmd, resolve, getenv, args[0], types)
			}
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			team, err := api.Team(rt)
			if err != nil {
				return err
			}
			payload, err := buildPayload(rt, args[0], types)
			if err != nil {
				return err
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
				return resource.UsageOnBadRequest(err)
			}
			resource.WarnIfPartial(cmd.ErrOrStderr(), raw, "--page or --limit (max "+resource.ServerMaxLimit+")")
			return resource.Render(cmd, rt, getenv, raw,
				&output.TableSpec{Rows: resource.ListRows("results"), Columns: columns})
		},
	}
	cmd.Flags().StringArrayVar(&types, "type", nil, "restrict to a resource type (repeatable): prompts|artifacts|blueprints|memories")
	cmd.Flags().IntVar(&limit, "limit", 0, resource.LimitHelp)
	cmd.Flags().IntVar(&page, "page", 0, "page number (1-based)")
	cmd.Flags().BoolVar(&summary, "summary", false, "answer the query from the top results with the team's model (AI Summary, platform v0.14.0+)")
	return cmd
}

// buildPayload is the body both search endpoints share: the query, the optional
// type filters and the optional project scope (resolved to its UUID).
func buildPayload(rt *config.Runtime, query string, types []string) (map[string]any, error) {
	payload := map[string]any{"query": query}
	if len(types) > 0 {
		payload["types"] = types
	}
	project, err := api.OptionalProject(rt)
	if err != nil {
		return nil, err
	}
	if project != "" {
		payload["project_id"] = project
	}
	return payload, nil
}
