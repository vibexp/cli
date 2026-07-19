// Package artifactcmd implements `vibexp artifact` CRUD — the polished-output
// store, built on the shared resource scaffold (docs/adding-commands.md).
// Artifacts are project-scoped: the API path is
// /api/v1/{team}/artifacts/{project}/{slug}, so every single-item verb resolves
// a project (flag > env > context; missing → exit 2) in addition to the team.
package artifactcmd

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

// columns are shared by the list (multi-row) and single-item renderers.
var columns = []output.Column{
	{Header: "SLUG", Path: ".slug"},
	{Header: "TITLE", Path: ".title"},
	{Header: "PROJECT", Path: ".project_id"},
	{Header: "UPDATED", Path: ".updated_at"},
}

// itemSpec renders a single artifact object.
var itemSpec = output.TableSpec{Rows: ".", Columns: columns}

// New builds the `artifact` command group.
func New(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Manage artifacts (create, read, update, delete)",
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

// readBodyFile reads artifact content from a file, or stdin when path is "-".
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

// basePath returns /api/v1/{team}/artifacts, resolving the team (flag > env >
// context; missing → exit 2).
func basePath(rt *config.Runtime) (string, error) {
	team, err := api.Team(rt)
	if err != nil {
		return "", err
	}
	return "/api/v1/" + team + "/artifacts", nil
}

// itemPath returns /api/v1/{team}/artifacts/{project}/{slug} for the single-item
// verbs, resolving both team and project (missing project → exit 2).
func itemPath(rt *config.Runtime, slug string) (string, error) {
	base, err := basePath(rt)
	if err != nil {
		return "", err
	}
	project, err := api.Project(rt) // required; missing → exit 2
	if err != nil {
		return "", err
	}
	return base + "/" + url.PathEscape(project) + "/" + url.PathEscape(slug), nil
}

func newList(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return resource.NewListCommand(resolve, getenv, "List artifacts in the resolved team", resource.ListConfig{
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
		Spec: output.TableSpec{Rows: ".artifacts[]? // .items[]? // .data[]?", Columns: columns},
	})
}
