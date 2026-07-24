package relationcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

// newConfirm promotes a suggested relation to confirmed. Confirming an
// already-confirmed relation returns 409, surfaced as the uniform CLI error.
func newConfirm(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "confirm <relation_id>",
		Short: "Confirm a suggested relation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}
			raw, _, err := resource.Do(ctx, client, http.MethodPost, base+"/"+args[0]+"/confirm", nil)
			if err != nil {
				return err
			}
			return resource.Render(cmd, rt, getenv, raw, &itemSpec)
		},
	}
}
