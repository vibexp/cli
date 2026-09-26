package resource

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/api"
	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/output"
)

// NewListCommand builds a ready `list` subcommand from a ListConfig: it binds
// the pagination flags and wires RunE to RunList, so a noun package only
// supplies its path and columns.
func NewListCommand(resolve CredResolver, getenv config.Getenv, short string, cfg ListConfig) *cobra.Command {
	return NewNamedListCommand("list", resolve, getenv, short, cfg)
}

// NewNamedListCommand is NewListCommand under a verb other than `list`, for a
// noun that exposes more than one paginated collection (e.g. `team audit`
// alongside `team list`). Everything else — pagination, filters, output
// contract — is identical.
func NewNamedListCommand(use string, resolve CredResolver, getenv config.Getenv, short string, cfg ListConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
	}
	page := AddPaginationFlags(cmd)
	if cfg.Filters != nil {
		AddFilterFlags(cmd, cfg.Filters)
	}
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
	// Filters, when non-nil, binds the filter flags it opts into on the command
	// and merges them into the query. Opt in only to filters the endpoint
	// actually accepts — see ListFilters.
	Filters *ListFilters
}

// WithProjectFilter appends ?project_id=<uuid> to a list path when a project
// is set (--project / env / context), resolving a slug first.
func WithProjectFilter(path string, rt *config.Runtime) (string, error) {
	project, err := api.OptionalProject(rt)
	if err != nil || project == "" {
		return path, err
	}
	return path + "?project_id=" + url.QueryEscape(project), nil
}

// RunList is the shared runner for a list command: resolve runtime, build the
// path, apply pagination, fetch, and render. A list command's RunE is just a
// call to this with its ListConfig and Pagination.
func RunList(cmd *cobra.Command, resolve CredResolver, getenv config.Getenv, cfg ListConfig, p *Pagination) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx := cmd.Context()
	rt, err := Runtime(ctx)
	if err != nil {
		return err
	}
	// The client comes first: building it installs the slug resolver PathFor
	// needs to turn a team or project slug into the UUID the path takes.
	client, err := Client(ctx, rt, resolve, getenv)
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
	if cfg.Filters != nil {
		path, err = cfg.Filters.ApplyToPath(path)
		if err != nil {
			return err
		}
	}
	if p.All {
		return renderAll(cmd, rt, getenv, client, path, cfg.Spec)
	}
	body, err := FetchJSON(ctx, client, http.MethodGet, path)
	if err != nil {
		return UsageOnBadRequest(err)
	}
	WarnIfPartial(cmd.ErrOrStderr(), body, "--page, --limit (max "+ServerMaxLimit+") or --all")
	return Render(cmd, rt, getenv, body, &cfg.Spec)
}

// allRows is the row expression for --all output: FetchAllPages merges every
// page's items into one bare array, so the endpoint's envelope key is gone.
const allRows = ".[]"

// renderAll walks every page of the list and renders the merged items as one
// JSON array (same shape as a single page's items), tabulated with the list's
// own columns.
func renderAll(cmd *cobra.Command, rt *config.Runtime, getenv config.Getenv, client *api.RawClient, path string, spec output.TableSpec) error {
	items, notList, err := FetchAllPages(cmd.Context(), client, path, nil)
	if err != nil {
		return UsageOnBadRequest(err)
	}
	if notList != nil {
		return Render(cmd, rt, getenv, notList, &spec)
	}
	merged, err := json.Marshal(items)
	if err != nil {
		return exitcode.New(exitcode.RuntimeErr, err)
	}
	spec.Rows = allRows
	return Render(cmd, rt, getenv, merged, &spec)
}
