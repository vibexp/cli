package resource

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/output"
)

// NewListCommand builds a ready `list` subcommand from a ListConfig: it binds
// the pagination flags and wires RunE to RunList, so a noun package only
// supplies its path and columns.
func NewListCommand(resolve CredResolver, getenv config.Getenv, short string, cfg ListConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		Args:  cobra.NoArgs,
	}
	page := AddPaginationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return RunList(cmd, resolve, getenv, cfg, page)
	}
	return cmd
}

// ListConfig declares a list endpoint: how to build its path (resolving team
// scope when needed) and how to tabulate it.
type ListConfig struct {
	// PathFor returns the server-relative list path for the runtime. It returns
	// a usage error (exit 2) when a required scope (e.g. team) is unset.
	PathFor func(rt *config.Runtime) (string, error)
	// Spec is the table/TSV column spec applied on a terminal or with
	// --format=table|text.
	Spec output.TableSpec
}

// RunList is the shared runner for a list command: resolve runtime, build the
// path, apply pagination, fetch, and render. A list command's RunE is just a
// call to this with its ListConfig and Pagination.
func RunList(cmd *cobra.Command, resolve CredResolver, getenv config.Getenv, cfg ListConfig, p *Pagination) error {
	ctx := cmd.Context()
	rt, err := Runtime(ctx)
	if err != nil {
		return err
	}
	path, err := cfg.PathFor(rt)
	if err != nil {
		return err
	}
	path, err = p.ApplyToPath(path)
	if err != nil {
		return err
	}
	client, err := Client(ctx, rt, resolve, getenv)
	if err != nil {
		return err
	}
	body, err := FetchJSON(ctx, client, http.MethodGet, path)
	if err != nil {
		return err
	}
	return Render(cmd, rt, getenv, body, &cfg.Spec)
}
