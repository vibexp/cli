package blueprintcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func newGet(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Show a single blueprint",
		Long:  "Show a single blueprint by slug within the resolved project (--project, VIBEXP_PROJECT, or the active context).",
		Args:  cobra.ExactArgs(1),
	}
	showRelations := resource.AddRelationsFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
		if err != nil {
			return err
		}
		path, err := itemPath(rt, args[0])
		if err != nil {
			return err
		}
		body, _, err := resource.Do(ctx, client, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		if err := resource.Render(cmd, rt, getenv, body, &itemSpec); err != nil {
			return err
		}
		if *showRelations {
			resource.RenderRelationsSummary(cmd, body)
		}
		return nil
	}
	return cmd
}
