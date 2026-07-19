package authcmd

import (
	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/exitcode"
)

func newLogout(resolve StoreResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the active context's stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := requireRuntime(cmd.Context())
			if err != nil {
				return err
			}
			contextName, err := requireContextName(rt)
			if err != nil {
				return err
			}
			store, err := resolve()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			removed, err := store.Delete(contextName)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			if !removed {
				cmd.PrintErrf("No stored credentials for context %q.\n", contextName)
				return nil
			}
			cmd.PrintErrf("Logged out of context %q.\n", contextName)
			return nil
		},
	}
}
