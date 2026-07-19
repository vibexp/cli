package blueprintcmd

import (
	"net/http"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/cli/resource"
	"github.com/vibexp/cli/internal/config"
)

func newDelete(resolve resource.CredResolver, getenv config.Getenv) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete a blueprint",
		Long:  "Delete a blueprint by slug within the resolved project (--project, VIBEXP_PROJECT, or the active context).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			proceed, err := resource.ConfirmDeletion(cmd, "blueprint "+slug, yes)
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
			itemURL, err := itemPath(rt, slug)
			if err != nil {
				return err
			}
			raw, _, err := resource.Do(ctx, client, http.MethodDelete, itemURL, nil)
			if err != nil {
				return err
			}
			// Some deletes return the object, others 204 with no body.
			if len(raw) > 0 {
				return resource.Render(cmd, rt, getenv, raw, &itemSpec)
			}
			cmd.PrintErrf("Deleted blueprint %s.\n", slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt (required for non-interactive use)")
	return cmd
}
