package memorycmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func newDelete(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			proceed, err := resource.ConfirmDeletion(cmd, "memory "+id, yes)
			if err != nil {
				return err
			}
			if !proceed {
				cmd.PrintErrln("Aborted.")
				return nil
			}

			ctx, rt, client, err := resource.RuntimeAndClient(cmd, resolve, getenv)
			if err != nil {
				return err
			}
			base, err := basePath(rt)
			if err != nil {
				return err
			}
			raw, _, err := resource.Do(ctx, client, http.MethodDelete, base+"/"+id, nil)
			if err != nil {
				return err
			}
			// Some deletes return the object, others 204 with no body.
			if len(raw) > 0 {
				return resource.Render(cmd, rt, getenv, raw, &itemSpec)
			}
			cmd.PrintErrf("Deleted memory %s.\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (required for non-interactive use)")
	return cmd
}
