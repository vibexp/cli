package authcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/cred"
	"github.com/vibexp/cli/internal/exitcode"
	"github.com/vibexp/cli/internal/logging"
)

func newStatus(resolve StoreResolver, getenv config.Getenv) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current authentication state",
		Long: "Report the authentication method and identity for the active context.\n" +
			"The secret itself is never shown — only a non-secret key fingerprint.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rt, err := requireRuntime(ctx)
			if err != nil {
				return err
			}
			store, err := resolve()
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}

			resolved, err := store.Resolve(rt.ContextName, getenv)
			if err != nil {
				return exitcode.New(exitcode.RuntimeErr, err)
			}
			out := cmd.OutOrStdout()
			if resolved.Source == cred.SourceNone {
				cmd.PrintErrf("Not authenticated for context %q.\n", rt.ContextName)
				cmd.PrintErrln("Log in with: vibexp auth login  (or --with-api-key)")
				return nil
			}

			baseURL, err := requireBaseURL(rt)
			if err != nil {
				return err
			}
			// For OAuth this transparently refreshes an expired access token.
			bearer, err := freshBearer(ctx, store, rt, resolved)
			if err != nil {
				return err
			}
			logging.RegisterSecret(bearer)

			user, err := fetchIdentity(ctx, baseURL, bearer, rt.Timeout)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Context:    %s\n", rt.ContextName)
			fmt.Fprintf(out, "Server:     %s\n", baseURL)
			fmt.Fprintf(out, "Method:     %s\n", methodLabel(resolved))
			fmt.Fprintf(out, "Credential: %s\n", cred.Fingerprint(bearer))
			fmt.Fprintf(out, "User:       %s <%s>\n", user.Name, string(user.Email))
			fmt.Fprintf(out, "User ID:    %s\n", user.Id)
			return nil
		},
	}
}
