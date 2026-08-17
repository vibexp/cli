// Package memorycmd implements `vibexp memory` CRUD — the first full
// create/get/update/delete resource, built on the #9 scaffold
// (docs/adding-commands.md).
package memorycmd

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
// reduced to a single STALE flag.
var columns = []output.Column{
	{Header: "ID", Path: ".id"},
	{Header: "PROJECT", Path: ".project_id"},
	{Header: "STATUS", Path: ".status"},
	resource.FreshnessColumn(),
	{Header: "UPDATED", Path: ".updated_at"},
	{Header: "TEXT", Path: ".text[0:60]"}, // preview of the content
}

// detailColumns drive the single-object views — get, and the create/update/
// delete confirmations — which carry the full v0.11.0 freshness state the list
// view reduces to one flag.
var detailColumns = resource.WithFreshnessDetail(
	[]output.Column{
		{Header: "ID", Path: ".id"},
		{Header: "PROJECT", Path: ".project_id"},
		{Header: "STATUS", Path: ".status"},
	},
	[]output.Column{
		{Header: "UPDATED", Path: ".updated_at"},
		{Header: "TEXT", Path: ".text[0:60]"}, // preview of the content
	},
)

// itemSpec renders a single memory object.
var itemSpec = output.TableSpec{Rows: ".", Columns: detailColumns}

// New builds the `memory` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage memories (create, read, update, delete)",
	}
	cmd.AddCommand(
		newList(resolve, getenv),
		newGet(resolve, getenv),
		newCreate(resolve, getenv),
		newUpdate(resolve, getenv),
		newDelete(resolve, getenv),
	)
	return cmd
}

// readBodyFile reads memory content from a file, or stdin when path is "-".
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

// basePath returns /api/v1/{team}/memories, resolving the team (flag > env >
// context; missing → exit 2).
func basePath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/memories", nil
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewListCommand(resolve, getenv, "List memories in the resolved team", resource.ListConfig{
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
		Spec:    output.TableSpec{Rows: ".memories[]? // .items[]? // .data[]?", Columns: columns},
		Filters: &resource.ListFilters{Metadata: true, Tags: true, Stale: true},
	})
}
