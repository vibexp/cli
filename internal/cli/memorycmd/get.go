package memorycmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func newGet(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show a single memory",
		Args:  cobra.ExactArgs(1),
	}
	showRelations := resource.AddRelationsFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
		if err != nil {
			return err
		}
		base, err := basePath(rt)
		if err != nil {
			return err
		}
		body, _, err := resource.Do(ctx, client, http.MethodGet, base+"/"+args[0], nil)
		if err != nil {
			return err
		}
		return resource.RenderWithRelations(cmd, rt, getenv, body, &itemSpec, *showRelations)
	}
	return cmd
}
