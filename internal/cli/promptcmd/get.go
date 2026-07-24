package promptcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func newGet(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Show a single prompt",
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
		return resource.RenderWithRelations(cmd, rt, getenv, body, &itemSpec, *showRelations)
	}
	return cmd
}
