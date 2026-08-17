// Package promptcmd implements `vibexp prompt` CRUD plus `prompt render` — the
// reusable-prompt library, built on the shared resource scaffold
// (docs/adding-commands.md). Prompts are team-scoped and addressed by slug:
// /api/v1/{team}/prompts/{slug}. `render` posts placeholder values to
// /api/v1/{team}/prompts/{slug}/render and prints the rendered body raw so it is
// safe to pipe.
package promptcmd

import (
	"io"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// columns drive the list (multi-row) view — kept compact, so freshness is
// reduced to a single STALE flag. Prompts expose `name` (not `title`) and carry
// no variables field on the object — placeholders live behind a separate
// endpoint that is out of scope here.
var columns = []output.Column{
	{Header: "SLUG", Path: ".slug"},
	{Header: "NAME", Path: ".name"},
	{Header: "STATUS", Path: ".status"},
	resource.FreshnessColumn(),
	{Header: "UPDATED", Path: ".updated_at"},
}

// detailColumns drive the single-object views — get, and the create/update/
// delete confirmations — which carry the full v0.11.0 freshness state the list
// view reduces to one flag.
var detailColumns = resource.WithFreshnessDetail(
	[]output.Column{
		{Header: "SLUG", Path: ".slug"},
		{Header: "NAME", Path: ".name"},
		{Header: "STATUS", Path: ".status"},
	},
	[]output.Column{
		{Header: "UPDATED", Path: ".updated_at"},
	},
)

// itemSpec renders a single prompt object.
var itemSpec = output.TableSpec{Rows: ".", Columns: detailColumns}

// New builds the `prompt` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Manage prompts (create, read, update, delete, render)",
	}
	cmd.AddCommand(
		newList(resolve, getenv),
		newGet(resolve, getenv),
		newCreate(resolve, getenv),
		newUpdate(resolve, getenv),
		newDelete(resolve, getenv),
		newRender(resolve, getenv),
	)
	return cmd
}

// readBodyFile reads prompt content from a file, or stdin when path is "-".
// An empty path means "not provided" and returns (nil, nil).
func readBodyFile(cmd *cobra.Command, path string) ([]byte, error) {
	switch path {
	case "":
		return nil, nil
	case "-":
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, exitcode.New(exitcode.RuntimeErr, err)
		}
		return body, nil
	default:
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, exitcode.Usage("read --body-file %q: %v", path, err)
		}
		return body, nil
	}
}

// basePath returns /api/v1/{team}/prompts, resolving the team (flag > env >
// context; missing → exit 2).
func basePath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/prompts", nil
}

// itemPath returns /api/v1/{team}/prompts/{slug} for the single-item verbs.
func itemPath(rt *config.Runtime, slug string) (string, error) {
	base, err := basePath(rt)
	if err != nil {
		return "", err
	}
	return base + "/" + url.PathEscape(slug), nil
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewListCommand(resolve, getenv, "List prompts in the resolved team", resource.ListConfig{
		PathFor: func(rt *config.Runtime) (string, error) {
			path, err := basePath(rt)
			if err != nil {
				return "", err
			}
			// Filter by project when one is resolved (--project / env / context).
			if rt.Project != "" {
				path += "?project_id=" + url.QueryEscape(rt.Project)
			}
			return path, nil
		},
		Spec: output.TableSpec{Rows: ".prompts[]? // .items[]? // .data[]?", Columns: columns},
		// Stale only: listPrompts takes freshness but has no metadata param, so
		// binding --metadata here would silently return the unfiltered list.
		Filters: &resource.ListFilters{Stale: true},
	})
}
